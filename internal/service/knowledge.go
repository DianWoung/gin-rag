package service

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/dianwang-mac/go-rag/internal/appdto"
	"github.com/dianwang-mac/go-rag/internal/apperr"
	"github.com/dianwang-mac/go-rag/internal/entity"
	"github.com/dianwang-mac/go-rag/internal/store"
	"gorm.io/gorm"
)

type KnowledgeBaseService struct {
	db                    *gorm.DB
	vectors               *store.QdrantStore
	defaultEmbeddingModel string
}

func NewKnowledgeBaseService(db *gorm.DB, vectors *store.QdrantStore, defaultEmbeddingModel string) *KnowledgeBaseService {
	return &KnowledgeBaseService{db: db, vectors: vectors, defaultEmbeddingModel: defaultEmbeddingModel}
}

func (s *KnowledgeBaseService) CreateKnowledgeBase(ctx context.Context, req appdto.CreateKnowledgeBaseRequest) (*entity.KnowledgeBase, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, apperr.New(http.StatusBadRequest, fmt.Errorf("name is required"))
	}

	embeddingModel := strings.TrimSpace(req.EmbeddingModel)
	if embeddingModel == "" {
		embeddingModel = s.defaultEmbeddingModel
	}

	kb := &entity.KnowledgeBase{
		Name:           name,
		Description:    strings.TrimSpace(req.Description),
		EmbeddingModel: embeddingModel,
		CollectionName: "pending",
	}
	if err := s.db.WithContext(ctx).Create(kb).Error; err != nil {
		return nil, fmt.Errorf("create knowledge base: %w", err)
	}

	kb.CollectionName = fmt.Sprintf("kb_%d", kb.ID)
	if err := s.db.WithContext(ctx).Save(kb).Error; err != nil {
		return nil, fmt.Errorf("update knowledge base collection name: %w", err)
	}

	return kb, nil
}

func (s *KnowledgeBaseService) DeleteKnowledgeBase(ctx context.Context, id uint) error {
	var kb entity.KnowledgeBase
	if err := s.db.WithContext(ctx).First(&kb, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return apperr.New(http.StatusNotFound, fmt.Errorf("knowledge base not found"))
		}
		return fmt.Errorf("find knowledge base: %w", err)
	}

	// Delete Qdrant collection (best-effort: log and continue if it fails).
	if kb.CollectionName != "" && kb.CollectionName != "pending" {
		if err := s.vectors.DeleteCollection(ctx, kb.CollectionName); err != nil {
			log.Printf("WARNING: failed to delete qdrant collection %q: %v", kb.CollectionName, err)
		}
	}

	// Cascade delete: chunks → documents → knowledge base.
	if err := s.db.WithContext(ctx).Where("knowledge_base_id = ?", kb.ID).Delete(&entity.DocumentChunk{}).Error; err != nil {
		return fmt.Errorf("delete chunks: %w", err)
	}
	if err := s.db.WithContext(ctx).Where("knowledge_base_id = ?", kb.ID).Delete(&entity.Document{}).Error; err != nil {
		return fmt.Errorf("delete documents: %w", err)
	}
	if err := s.db.WithContext(ctx).Delete(&kb).Error; err != nil {
		return fmt.Errorf("delete knowledge base: %w", err)
	}

	return nil
}

func (s *KnowledgeBaseService) ListKnowledgeBases(ctx context.Context) ([]entity.KnowledgeBase, error) {
	var list []entity.KnowledgeBase
	if err := s.db.WithContext(ctx).Order("id desc").Find(&list).Error; err != nil {
		return nil, fmt.Errorf("list knowledge bases: %w", err)
	}

	return list, nil
}
