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
	"github.com/dianwang-mac/go-rag/internal/store"
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

func (s *DocumentService) ImportTextDocument(ctx context.Context, req appdto.ImportTextDocumentRequest) (*entity.Document, error) {
	if req.KnowledgeBaseID == 0 {
		return nil, apperr.New(http.StatusBadRequest, fmt.Errorf("knowledge_base_id is required"))
	}
	if strings.TrimSpace(req.Title) == "" {
		return nil, apperr.New(http.StatusBadRequest, fmt.Errorf("title is required"))
	}
	if strings.TrimSpace(req.Content) == "" {
		return nil, apperr.New(http.StatusBadRequest, fmt.Errorf("content is required"))
	}

	var kb entity.KnowledgeBase
	if err := s.db.WithContext(ctx).First(&kb, req.KnowledgeBaseID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, apperr.New(http.StatusNotFound, fmt.Errorf("knowledge base not found"))
		}
		return nil, fmt.Errorf("find knowledge base: %w", err)
	}

	sourceType := strings.TrimSpace(req.SourceType)
	if sourceType == "" {
		sourceType = "text"
	}

	return s.createDocument(ctx, kb.ID, strings.TrimSpace(req.Title), sourceType, req.Content)
}

func (s *DocumentService) ImportPDFDocument(ctx context.Context, req appdto.ImportPDFDocumentRequest) (*entity.Document, error) {
	if req.KnowledgeBaseID == 0 {
		return nil, apperr.New(http.StatusBadRequest, fmt.Errorf("knowledge_base_id is required"))
	}

	var kb entity.KnowledgeBase
	if err := s.db.WithContext(ctx).First(&kb, req.KnowledgeBaseID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, apperr.New(http.StatusNotFound, fmt.Errorf("knowledge base not found"))
		}
		return nil, fmt.Errorf("find knowledge base: %w", err)
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
		return nil, apperr.New(http.StatusBadRequest, fmt.Errorf("title is required"))
	}

	content := req.Content
	if len(content) == 0 && strings.TrimSpace(req.FilePath) != "" {
		raw, err := os.ReadFile(strings.TrimSpace(req.FilePath))
		if err != nil {
			return nil, fmt.Errorf("read pdf file: %w", err)
		}
		content = raw
	}
	if len(content) == 0 {
		return nil, apperr.New(http.StatusBadRequest, fmt.Errorf("pdf file content is required"))
	}

	text, err := s.pdf.Extract(ctx, content)
	if err != nil {
		return nil, apperr.New(http.StatusBadRequest, fmt.Errorf("extract pdf text: %w", err))
	}
	if strings.TrimSpace(text) == "" {
		return nil, apperr.New(http.StatusBadRequest, fmt.Errorf("pdf produced no extractable text"))
	}

	return s.createDocument(ctx, kb.ID, title, "pdf", text)
}

func (s *DocumentService) IndexDocument(ctx context.Context, documentID uint) (*entity.Document, error) {
	var doc entity.Document
	if err := s.db.WithContext(ctx).First(&doc, documentID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, apperr.New(http.StatusNotFound, fmt.Errorf("document not found"))
		}
		return nil, fmt.Errorf("find document: %w", err)
	}

	var chunkCount int64
	if err := s.db.WithContext(ctx).Model(&entity.DocumentChunk{}).Where("document_id = ?", doc.ID).Count(&chunkCount).Error; err != nil {
		return nil, fmt.Errorf("count document chunks: %w", err)
	}
	if chunkCount > 0 {
		return nil, apperr.New(http.StatusBadRequest, fmt.Errorf("document already indexed"))
	}

	var kb entity.KnowledgeBase
	if err := s.db.WithContext(ctx).First(&kb, doc.KnowledgeBaseID).Error; err != nil {
		return nil, fmt.Errorf("find knowledge base: %w", err)
	}

	chunks := s.splitter.Split(doc.Content)
	if len(chunks) == 0 {
		return nil, apperr.New(http.StatusBadRequest, fmt.Errorf("document content produced no chunks"))
	}

	embedder, err := s.provider.NewEmbedder(ctx, kb.EmbeddingModel)
	if err != nil {
		return nil, err
	}

	vectors, err := embedder.EmbedStrings(ctx, chunks)
	if err != nil {
		return nil, fmt.Errorf("embed document chunks: %w", err)
	}
	if len(vectors) == 0 {
		return nil, fmt.Errorf("embedding result is empty")
	}

	actualDim := len(vectors[0])
	if kb.VectorDimension == 0 {
		kb.VectorDimension = actualDim
		if err := s.db.WithContext(ctx).Save(&kb).Error; err != nil {
			return nil, fmt.Errorf("update knowledge base dimension: %w", err)
		}
	} else if kb.VectorDimension != actualDim {
		return nil, apperr.New(http.StatusBadRequest, fmt.Errorf(
			"vector dimension mismatch: knowledge base expects %d, but embedding model %q produces %d (did you change the embedding model?)",
			kb.VectorDimension, kb.EmbeddingModel, actualDim,
		))
	}

	if err := s.vectors.EnsureCollection(ctx, kb.CollectionName, kb.VectorDimension); err != nil {
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

	if err := s.db.WithContext(ctx).Create(&dbChunks).Error; err != nil {
		return nil, fmt.Errorf("create document chunks: %w", err)
	}

	if err := s.vectors.UpsertChunks(ctx, kb.CollectionName, vectorChunks); err != nil {
		return nil, err
	}

	doc.Status = "indexed"
	doc.ErrorMessage = ""
	if err := s.db.WithContext(ctx).Save(&doc).Error; err != nil {
		return nil, fmt.Errorf("update document status: %w", err)
	}

	return &doc, nil
}

func (s *DocumentService) ListDocuments(ctx context.Context, knowledgeBaseID uint) ([]entity.Document, error) {
	query := s.db.WithContext(ctx).Model(&entity.Document{}).Order("id desc")
	if knowledgeBaseID > 0 {
		query = query.Where("knowledge_base_id = ?", knowledgeBaseID)
	}

	var list []entity.Document
	if err := query.Find(&list).Error; err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}

	return list, nil
}

func estimateTokens(content string) int {
	if content == "" {
		return 0
	}

	return len([]rune(content)) / 4
}

func (s *DocumentService) createDocument(ctx context.Context, knowledgeBaseID uint, title, sourceType, content string) (*entity.Document, error) {
	doc := &entity.Document{
		KnowledgeBaseID: knowledgeBaseID,
		Title:           title,
		SourceType:      sourceType,
		Status:          "imported",
		Content:         content,
	}
	if err := s.db.WithContext(ctx).Create(doc).Error; err != nil {
		return nil, fmt.Errorf("create document: %w", err)
	}

	return doc, nil
}
