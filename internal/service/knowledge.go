package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/dianwang-mac/go-rag/internal/appdto"
	"github.com/dianwang-mac/go-rag/internal/apperr"
	"github.com/dianwang-mac/go-rag/internal/entity"
	"gorm.io/gorm"
)

type KnowledgeBaseService struct {
	db                *gorm.DB
	defaultEmbeddingModel string
}

func NewKnowledgeBaseService(db *gorm.DB, defaultEmbeddingModel string) *KnowledgeBaseService {
	return &KnowledgeBaseService{db: db, defaultEmbeddingModel: defaultEmbeddingModel}
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

func (s *KnowledgeBaseService) ListKnowledgeBases(ctx context.Context) ([]entity.KnowledgeBase, error) {
	var list []entity.KnowledgeBase
	if err := s.db.WithContext(ctx).Order("id desc").Find(&list).Error; err != nil {
		return nil, fmt.Errorf("list knowledge bases: %w", err)
	}

	return list, nil
}
