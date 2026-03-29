package service

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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

type DocumentService struct {
	db       *gorm.DB
	splitter *ingest.Splitter
	pdf      ingest.PDFTextExtractor
	vectors  *store.QdrantStore
	provider *llm.Provider
}

func NewDocumentService(db *gorm.DB, splitter *ingest.Splitter, vectors *store.QdrantStore, provider *llm.Provider) *DocumentService {
	return &DocumentService{
		db:       db,
		splitter: splitter,
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
	if fileName == "" && strings.TrimSpace(req.FilePath) != "" {
		fileName = filepath.Base(strings.TrimSpace(req.FilePath))
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = fileName
	}
	if title == "" {
		err = apperr.New(http.StatusBadRequest, fmt.Errorf("title is required"))
		return nil, err
	}

	content := req.Content
	if len(content) == 0 && strings.TrimSpace(req.FilePath) != "" {
		raw, err := os.ReadFile(strings.TrimSpace(req.FilePath))
		if err != nil {
			err = fmt.Errorf("read pdf file: %w", err)
			return nil, err
		}
		content = raw
	}
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

	var chunkCount int64
	if err = s.db.WithContext(ctx).Model(&entity.DocumentChunk{}).Where("document_id = ?", doc.ID).Count(&chunkCount).Error; err != nil {
		err = fmt.Errorf("count document chunks: %w", err)
		return nil, err
	}
	if chunkCount > 0 {
		err = apperr.New(http.StatusBadRequest, fmt.Errorf("document already indexed"))
		return nil, err
	}

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
	chunks := s.splitter.Split(doc.Content)
	splitSpan.SetAttributes(
		attribute.Int(observability.AttrChunkCount, len(chunks)),
		observability.TextListAttribute(observability.AttrChunkBodies, chunks),
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

	vectors, err := embedder.EmbedStrings(embedCtx, chunks)
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
	for idx, content := range chunks {
		pointID := uuid.NewString()
		dbChunks = append(dbChunks, entity.DocumentChunk{
			KnowledgeBaseID: kb.ID,
			DocumentID:      doc.ID,
			ChunkIndex:      idx,
			Content:         content,
			TokenCount:      estimateTokens(content),
			VectorPointID:   pointID,
		})
		vectorChunks = append(vectorChunks, store.ChunkVector{
			PointID:    pointID,
			DocumentID: doc.ID,
			ChunkIndex: idx,
			Content:    content,
			Vector:     vectors[idx],
		})
	}

	if err = s.db.WithContext(ctx).Create(&dbChunks).Error; err != nil {
		err = fmt.Errorf("create document chunks: %w", err)
		return nil, err
	}

	if err = s.vectors.UpsertChunks(ctx, kb.CollectionName, vectorChunks); err != nil {
		return nil, err
	}

	doc.Status = "indexed"
	doc.ErrorMessage = ""
	if err = s.db.WithContext(ctx).Save(doc).Error; err != nil {
		err = fmt.Errorf("update document status: %w", err)
		return nil, err
	}

	return doc, nil
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

func estimateTokens(content string) int {
	if content == "" {
		return 0
	}

	return len([]rune(content)) / 4
}

func (s *DocumentService) createDocument(ctx context.Context, knowledgeBaseID uint, title, sourceType, content string) (doc *entity.Document, err error) {
	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanDocumentCreate,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceDocument),
		attribute.Int(observability.AttrKnowledgeBaseID, int(knowledgeBaseID)),
		observability.TextAttribute(observability.AttrTitle, title),
		observability.TextAttribute(observability.AttrSourceType, sourceType),
		observability.TextAttribute(observability.AttrChunkBodies, content),
	)
	defer func() {
		if doc != nil {
			span.SetAttributes(attribute.Int(observability.AttrDocumentID, int(doc.ID)))
		}
		observability.RecordError(span, err)
		span.End()
	}()

	doc = &entity.Document{
		KnowledgeBaseID: knowledgeBaseID,
		Title:           title,
		SourceType:      sourceType,
		Status:          "imported",
		Content:         content,
	}
	if err = s.db.WithContext(ctx).Create(doc).Error; err != nil {
		err = fmt.Errorf("create document: %w", err)
		return nil, err
	}

	return doc, nil
}
