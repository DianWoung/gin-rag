package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	einoembedding "github.com/cloudwego/eino/components/embedding"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dianwang-mac/go-rag/internal/appdto"
	"github.com/dianwang-mac/go-rag/internal/apperr"
	"github.com/dianwang-mac/go-rag/internal/entity"
	"github.com/dianwang-mac/go-rag/internal/ingest"
	"github.com/dianwang-mac/go-rag/internal/llm"
	"github.com/dianwang-mac/go-rag/internal/observability"
	"github.com/dianwang-mac/go-rag/internal/store"
	"go.opentelemetry.io/otel/attribute"
)

type documentVectorStore interface {
	EnsureCollection(ctx context.Context, collectionName string, dimension int) error
	UpsertChunks(ctx context.Context, collectionName string, chunks []store.ChunkVector) error
	DeletePoints(ctx context.Context, collectionName string, pointIDs []string) error
}

type embeddingProvider interface {
	NewEmbedder(ctx context.Context, modelName string) (einoembedding.Embedder, error)
}

type DocumentService struct {
	db       *gorm.DB
	splitter *ingest.Splitter
	cleaner  *ingest.Cleaner
	pdf      ingest.PDFTextExtractor
	vectors  documentVectorStore
	provider embeddingProvider
}

const (
	documentStatusImported = "imported"
	documentStatusIndexing = "indexing"
	documentStatusIndexed  = "indexed"
	documentStatusFailed   = "failed"
)

func NewDocumentService(db *gorm.DB, splitter *ingest.Splitter, vectors *store.QdrantStore, provider *llm.Provider) *DocumentService {
	return &DocumentService{
		db:       db,
		splitter: splitter,
		cleaner:  ingest.NewCleaner(),
		pdf:      ingest.NewPDFExtractor(),
		vectors:  vectors,
		provider: provider,
	}
}

func (s *DocumentService) ImportTextDocument(ctx context.Context, req appdto.ImportTextDocumentRequest) (doc *entity.Document, err error) {
	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanDocumentImportText,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceDocument),
		attribute.Int(observability.AttrKnowledgeBaseID, int(req.KnowledgeBaseID)),
		observability.TextAttribute(observability.AttrTitle, req.Title),
		observability.TextAttribute(observability.AttrSourceType, req.SourceType),
		observability.TextAttribute(observability.AttrChunkBodies, req.Content),
	)
	defer func() {
		if doc != nil {
			span.SetAttributes(attribute.Int(observability.AttrDocumentID, int(doc.ID)))
		}
		observability.RecordError(span, err)
		span.End()
	}()

	if req.KnowledgeBaseID == 0 {
		err = apperr.New(http.StatusBadRequest, fmt.Errorf("knowledge_base_id is required"))
		return nil, err
	}
	if strings.TrimSpace(req.Title) == "" {
		err = apperr.New(http.StatusBadRequest, fmt.Errorf("title is required"))
		return nil, err
	}
	if strings.TrimSpace(req.Content) == "" {
		err = apperr.New(http.StatusBadRequest, fmt.Errorf("content is required"))
		return nil, err
	}

	var kb entity.KnowledgeBase
	if err = s.db.WithContext(ctx).First(&kb, req.KnowledgeBaseID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			err = apperr.New(http.StatusNotFound, fmt.Errorf("knowledge base not found"))
			return nil, err
		}
		err = fmt.Errorf("find knowledge base: %w", err)
		return nil, err
	}

	sourceType := strings.TrimSpace(req.SourceType)
	if sourceType == "" {
		sourceType = "text"
	}

	return s.createDocument(ctx, kb.ID, strings.TrimSpace(req.Title), sourceType, req.Content)
}

func (s *DocumentService) ImportPDFDocument(ctx context.Context, req appdto.ImportPDFDocumentRequest) (doc *entity.Document, err error) {
	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanDocumentImportPDF,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceDocument),
		attribute.Int(observability.AttrKnowledgeBaseID, int(req.KnowledgeBaseID)),
		observability.TextAttribute(observability.AttrTitle, req.Title),
		observability.TextAttribute("rag.file_name", req.FileName),
	)
	defer func() {
		if doc != nil {
			span.SetAttributes(attribute.Int(observability.AttrDocumentID, int(doc.ID)))
		}
		observability.RecordError(span, err)
		span.End()
	}()

	if req.KnowledgeBaseID == 0 {
		err = apperr.New(http.StatusBadRequest, fmt.Errorf("knowledge_base_id is required"))
		return nil, err
	}

	var kb entity.KnowledgeBase
	if err = s.db.WithContext(ctx).First(&kb, req.KnowledgeBaseID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			err = apperr.New(http.StatusNotFound, fmt.Errorf("knowledge base not found"))
			return nil, err
		}
		err = fmt.Errorf("find knowledge base: %w", err)
		return nil, err
	}

	fileName := strings.TrimSpace(req.FileName)

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = fileName
	}
	if title == "" {
		err = apperr.New(http.StatusBadRequest, fmt.Errorf("title is required"))
		return nil, err
	}

	content := req.Content
	if len(content) == 0 {
		err = apperr.New(http.StatusBadRequest, fmt.Errorf("pdf file content is required"))
		return nil, err
	}

	text, err := s.pdf.Extract(ctx, content)
	if err != nil {
		err = apperr.New(http.StatusBadRequest, fmt.Errorf("extract pdf text: %w", err))
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		err = apperr.New(http.StatusBadRequest, fmt.Errorf("pdf produced no extractable text"))
		return nil, err
	}
	span.SetAttributes(observability.TextAttribute(observability.AttrChunkBodies, text))

	return s.createDocument(ctx, kb.ID, title, "pdf", text)
}

func (s *DocumentService) IndexDocument(ctx context.Context, documentID uint) (doc *entity.Document, err error) {
	var claimedForIndexing bool

	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanDocumentIndex,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceDocument),
		attribute.Int(observability.AttrDocumentID, int(documentID)),
	)
	defer func() {
		if doc != nil {
			span.SetAttributes(attribute.Int(observability.AttrKnowledgeBaseID, int(doc.KnowledgeBaseID)))
		}
		if err != nil && claimedForIndexing {
			if markErr := s.markDocumentFailed(ctx, documentID, err.Error()); markErr != nil {
				err = errors.Join(err, fmt.Errorf("mark document failed: %w", markErr))
			}
		}
		observability.RecordError(span, err)
		span.End()
	}()

	var model entity.Document
	if err = s.db.WithContext(ctx).First(&model, documentID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			err = apperr.New(http.StatusNotFound, fmt.Errorf("document not found"))
			return nil, err
		}
		err = fmt.Errorf("find document: %w", err)
		return nil, err
	}
	doc = &model
	span.SetAttributes(
		attribute.Int(observability.AttrKnowledgeBaseID, int(doc.KnowledgeBaseID)),
		observability.TextAttribute(observability.AttrTitle, doc.Title),
		observability.TextAttribute(observability.AttrSourceType, doc.SourceType),
	)

	switch doc.Status {
	case documentStatusIndexed:
		err = apperr.New(http.StatusBadRequest, fmt.Errorf("document already indexed"))
		return nil, err
	case documentStatusIndexing:
		err = apperr.New(http.StatusConflict, fmt.Errorf("document is currently indexing"))
		return nil, err
	case documentStatusImported, documentStatusFailed:
	default:
		err = apperr.New(http.StatusBadRequest, fmt.Errorf("document is not indexable in status %q", doc.Status))
		return nil, err
	}

	if err = s.claimDocumentForIndexing(ctx, doc.ID); err != nil {
		return nil, err
	}
	claimedForIndexing = true
	doc.Status = documentStatusIndexing
	doc.ErrorMessage = ""

	var kb entity.KnowledgeBase
	if err = s.db.WithContext(ctx).First(&kb, doc.KnowledgeBaseID).Error; err != nil {
		err = fmt.Errorf("find knowledge base: %w", err)
		return nil, err
	}
	span.SetAttributes(
		attribute.String(observability.AttrCollectionName, kb.CollectionName),
		attribute.String(observability.AttrEmbeddingModel, kb.EmbeddingModel),
	)

	splitCtx, splitSpan := observability.StartSpan(ctx, observability.SpanDocumentSplit, attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceDocument))
	blocks := ingest.BuildBlocks(doc.Content)
	chunks := s.splitter.SplitBlocks(blocks)
	chunkBodies := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		chunkBodies = append(chunkBodies, chunk.Content)
	}
	splitSpan.SetAttributes(
		attribute.Int(observability.AttrChunkCount, len(chunks)),
		observability.TextListAttribute(observability.AttrChunkBodies, chunkBodies),
	)
	splitSpan.End()
	if len(chunks) == 0 {
		err = apperr.New(http.StatusBadRequest, fmt.Errorf("document content produced no chunks"))
		return nil, err
	}

	embedCtx, embedSpan := observability.StartSpan(splitCtx, observability.SpanDocumentEmbedChunks, attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceDocument))
	embedder, err := s.provider.NewEmbedder(embedCtx, kb.EmbeddingModel)
	if err != nil {
		observability.RecordError(embedSpan, err)
		embedSpan.End()
		return nil, err
	}

	vectors, err := embedder.EmbedStrings(embedCtx, chunkBodies)
	if err != nil {
		err = fmt.Errorf("embed document chunks: %w", err)
		observability.RecordError(embedSpan, err)
		embedSpan.End()
		return nil, err
	}
	if len(vectors) == 0 {
		err = fmt.Errorf("embedding result is empty")
		observability.RecordError(embedSpan, err)
		embedSpan.End()
		return nil, err
	}
	embedSpan.SetAttributes(attribute.Int("rag.vector_count", len(vectors)))
	embedSpan.End()

	actualDim := len(vectors[0])
	if kb.VectorDimension == 0 {
		kb.VectorDimension = actualDim
		if err = s.db.WithContext(ctx).Save(&kb).Error; err != nil {
			err = fmt.Errorf("update knowledge base dimension: %w", err)
			return nil, err
		}
	} else if kb.VectorDimension != actualDim {
		err = apperr.New(http.StatusBadRequest, fmt.Errorf(
			"vector dimension mismatch: knowledge base expects %d, but embedding model %q produces %d (did you change the embedding model?)",
			kb.VectorDimension, kb.EmbeddingModel, actualDim,
		))
		return nil, err
	}

	if err = s.vectors.EnsureCollection(ctx, kb.CollectionName, kb.VectorDimension); err != nil {
		return nil, err
	}

	dbChunks := make([]entity.DocumentChunk, 0, len(chunks))
	vectorChunks := make([]store.ChunkVector, 0, len(chunks))
	pointIDs := make([]string, 0, len(chunks))
	for idx, chunk := range chunks {
		pointID := uuid.NewString()
		pointIDs = append(pointIDs, pointID)
		dbChunks = append(dbChunks, entity.DocumentChunk{
			KnowledgeBaseID: kb.ID,
			DocumentID:      doc.ID,
			ChunkIndex:      idx,
			ChunkType:       chunk.Type,
			TableID:         chunk.TableID,
			PageNo:          chunk.PageNo,
			Content:         chunk.Content,
			TokenCount:      estimateTokens(chunk.Content),
			VectorPointID:   pointID,
		})
		vectorChunks = append(vectorChunks, store.ChunkVector{
			PointID:    pointID,
			DocumentID: doc.ID,
			ChunkIndex: idx,
			ChunkType:  chunk.Type,
			TableID:    chunk.TableID,
			PageNo:     chunk.PageNo,
			Title:      doc.Title,
			SourceType: doc.SourceType,
			Content:    chunk.Content,
			Vector:     vectors[idx],
		})
	}

	if err = s.vectors.UpsertChunks(ctx, kb.CollectionName, vectorChunks); err != nil {
		err = fmt.Errorf("upsert qdrant points: %w", err)
		return nil, err
	}

	if err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("document_id = ?", doc.ID).Delete(&entity.DocumentChunk{}).Error; err != nil {
			return fmt.Errorf("delete stale document chunks: %w", err)
		}
		if err := tx.Create(&dbChunks).Error; err != nil {
			return fmt.Errorf("create document chunks: %w", err)
		}
		if err := s.markDocumentIndexed(tx, doc.ID); err != nil {
			return err
		}
		return nil
	}); err != nil {
		cleanupErr := s.vectors.DeletePoints(ctx, kb.CollectionName, pointIDs)
		if cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("delete qdrant points: %w", cleanupErr))
		}
		return nil, err
	}

	doc.Status = documentStatusIndexed
	doc.ErrorMessage = ""

	return doc, nil
}

func (s *DocumentService) claimDocumentForIndexing(ctx context.Context, documentID uint) error {
	result := s.db.WithContext(ctx).
		Model(&entity.Document{}).
		Where("id = ? AND status IN ?", documentID, []string{documentStatusImported, documentStatusFailed}).
		Updates(map[string]any{
			"status":        documentStatusIndexing,
			"error_message": "",
		})
	if result.Error != nil {
		return fmt.Errorf("claim document for indexing: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return apperr.New(http.StatusConflict, fmt.Errorf("document is no longer indexable"))
	}
	return nil
}

func (s *DocumentService) markDocumentFailed(ctx context.Context, documentID uint, message string) error {
	return s.db.WithContext(ctx).
		Model(&entity.Document{}).
		Where("id = ?", documentID).
		Updates(map[string]any{
			"status":        documentStatusFailed,
			"error_message": message,
		}).Error
}

func (s *DocumentService) markDocumentIndexed(tx *gorm.DB, documentID uint) error {
	if err := tx.Model(&entity.Document{}).
		Where("id = ?", documentID).
		Updates(map[string]any{
			"status":        documentStatusIndexed,
			"error_message": "",
		}).Error; err != nil {
		return fmt.Errorf("update document status: %w", err)
	}
	return nil
}

func (s *DocumentService) DeleteDocument(ctx context.Context, documentID uint) (err error) {
	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanDocumentDelete,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceDocument),
		attribute.Int(observability.AttrDocumentID, int(documentID)),
	)
	defer func() {
		observability.RecordError(span, err)
		span.End()
	}()

	var doc entity.Document
	if err = s.db.WithContext(ctx).First(&doc, documentID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			err = apperr.New(http.StatusNotFound, fmt.Errorf("document not found"))
			return err
		}
		err = fmt.Errorf("find document: %w", err)
		return err
	}
	span.SetAttributes(
		attribute.Int(observability.AttrKnowledgeBaseID, int(doc.KnowledgeBaseID)),
		observability.TextAttribute(observability.AttrTitle, doc.Title),
	)

	var kb entity.KnowledgeBase
	if err = s.db.WithContext(ctx).First(&kb, doc.KnowledgeBaseID).Error; err != nil {
		err = fmt.Errorf("find knowledge base: %w", err)
		return err
	}

	// Collect Qdrant point IDs before deleting from MySQL.
	var chunks []entity.DocumentChunk
	if err = s.db.WithContext(ctx).Where("document_id = ?", doc.ID).Find(&chunks).Error; err != nil {
		err = fmt.Errorf("find document chunks: %w", err)
		return err
	}

	pointIDs := make([]string, 0, len(chunks))
	for _, c := range chunks {
		if c.VectorPointID != "" {
			pointIDs = append(pointIDs, c.VectorPointID)
		}
	}

	// Delete vectors from Qdrant first (external resource, harder to undo).
	if len(pointIDs) > 0 {
		if err = s.vectors.DeletePoints(ctx, kb.CollectionName, pointIDs); err != nil {
			err = fmt.Errorf("delete vectors: %w", err)
			return err
		}
	}

	// Delete chunks and document from MySQL.
	if err = s.db.WithContext(ctx).Where("document_id = ?", doc.ID).Delete(&entity.DocumentChunk{}).Error; err != nil {
		err = fmt.Errorf("delete document chunks: %w", err)
		return err
	}
	if err = s.db.WithContext(ctx).Delete(&doc).Error; err != nil {
		err = fmt.Errorf("delete document: %w", err)
		return err
	}

	return nil
}

func (s *DocumentService) ListDocuments(ctx context.Context, knowledgeBaseID uint) (list []entity.Document, err error) {
	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanDocumentList,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceDocument),
		attribute.Int(observability.AttrKnowledgeBaseID, int(knowledgeBaseID)),
	)
	defer func() {
		span.SetAttributes(attribute.Int("rag.list_count", len(list)))
		observability.RecordError(span, err)
		span.End()
	}()

	query := s.db.WithContext(ctx).Model(&entity.Document{}).Order("id desc")
	if knowledgeBaseID > 0 {
		query = query.Where("knowledge_base_id = ?", knowledgeBaseID)
	}

	if err = query.Find(&list).Error; err != nil {
		err = fmt.Errorf("list documents: %w", err)
		return nil, err
	}

	return list, nil
}

func (s *DocumentService) ListDocumentChunks(ctx context.Context, documentID uint) (list []entity.DocumentChunk, err error) {
	ctx, span := observability.StartSpan(
		ctx,
		"service.document.list_chunks",
		attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceDocument),
		attribute.Int(observability.AttrDocumentID, int(documentID)),
	)
	defer func() {
		span.SetAttributes(attribute.Int("rag.list_count", len(list)))
		observability.RecordError(span, err)
		span.End()
	}()

	if documentID == 0 {
		err = apperr.New(http.StatusBadRequest, fmt.Errorf("document id is required"))
		return nil, err
	}

	var doc entity.Document
	if err = s.db.WithContext(ctx).First(&doc, documentID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			err = apperr.New(http.StatusNotFound, fmt.Errorf("document not found"))
			return nil, err
		}
		err = fmt.Errorf("find document: %w", err)
		return nil, err
	}

	if err = s.db.WithContext(ctx).
		Where("document_id = ?", documentID).
		Order("chunk_index asc").
		Find(&list).Error; err != nil {
		err = fmt.Errorf("list document chunks: %w", err)
		return nil, err
	}

	return list, nil
}

func estimateTokens(content string) int {
	if content == "" {
		return 0
	}

	return len([]rune(content)) / 4
}

func (s *DocumentService) createDocument(ctx context.Context, knowledgeBaseID uint, title, sourceType, content string) (doc *entity.Document, err error) {
	cleanedContent := content
	cleanReport := ingest.CleanReport{}
	if s.cleaner != nil {
		cleanedContent, cleanReport = s.cleaner.Clean(content)
	}

	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanDocumentCreate,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceDocument),
		attribute.Int(observability.AttrKnowledgeBaseID, int(knowledgeBaseID)),
		observability.TextAttribute(observability.AttrTitle, title),
		observability.TextAttribute(observability.AttrSourceType, sourceType),
		observability.TextAttribute(observability.AttrChunkBodies, cleanedContent),
	)
	defer func() {
		if doc != nil {
			span.SetAttributes(attribute.Int(observability.AttrDocumentID, int(doc.ID)))
		}
		observability.RecordError(span, err)
		span.End()
	}()
	span.SetAttributes(
		attribute.Bool("rag.clean.changed", cleanReport.Changed),
		attribute.Int("rag.clean.removed_blank_lines", cleanReport.RemovedBlankLines),
		attribute.Int("rag.clean.removed_duplicate_lines", cleanReport.RemovedDuplicateLines),
		attribute.Int("rag.clean.removed_page_number_lines", cleanReport.RemovedPageNumberLines),
		attribute.Int("rag.clean.removed_repeated_header_footer_lines", cleanReport.RemovedRepeatedHeaderFooterLines),
	)

	doc = &entity.Document{
		KnowledgeBaseID: knowledgeBaseID,
		Title:           title,
		SourceType:      sourceType,
		Status:          "imported",
		Content:         cleanedContent,
	}
	if err = s.db.WithContext(ctx).Create(doc).Error; err != nil {
		err = fmt.Errorf("create document: %w", err)
		return nil, err
	}

	return doc, nil
}
