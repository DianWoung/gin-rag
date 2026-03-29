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
	"github.com/dianwang-mac/go-rag/internal/observability"
	"github.com/dianwang-mac/go-rag/internal/store"
	"go.opentelemetry.io/otel/attribute"
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

func (s *KnowledgeBaseService) CreateKnowledgeBase(ctx context.Context, req appdto.CreateKnowledgeBaseRequest) (kb *entity.KnowledgeBase, err error) {
	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanKnowledgeBaseCreate,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceKnowledgeBase),
		observability.TextAttribute(observability.AttrKnowledgeBaseName, req.Name),
		observability.TextAttribute("rag.description", req.Description),
	)
	defer func() {
		if kb != nil {
			span.SetAttributes(
				attribute.Int(observability.AttrKnowledgeBaseID, int(kb.ID)),
				attribute.String(observability.AttrCollectionName, kb.CollectionName),
				attribute.String(observability.AttrEmbeddingModel, kb.EmbeddingModel),
			)
		}
		observability.RecordError(span, err)
		span.End()
	}()

	name := strings.TrimSpace(req.Name)
	if name == "" {
		err = apperr.New(http.StatusBadRequest, fmt.Errorf("name is required"))
		return nil, err
	}

	embeddingModel := strings.TrimSpace(req.EmbeddingModel)
	if embeddingModel == "" {
		embeddingModel = s.defaultEmbeddingModel
	}

	kb = &entity.KnowledgeBase{
		Name:           name,
		Description:    strings.TrimSpace(req.Description),
		EmbeddingModel: embeddingModel,
		CollectionName: "pending",
	}
	if err = s.db.WithContext(ctx).Create(kb).Error; err != nil {
		err = fmt.Errorf("create knowledge base: %w", err)
		return nil, err
	}

	kb.CollectionName = fmt.Sprintf("kb_%d", kb.ID)
	if err = s.db.WithContext(ctx).Save(kb).Error; err != nil {
		err = fmt.Errorf("update knowledge base collection name: %w", err)
		return nil, err
	}

	return kb, nil
}

func (s *KnowledgeBaseService) DeleteKnowledgeBase(ctx context.Context, id uint) (err error) {
	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanKnowledgeBaseDelete,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceKnowledgeBase),
		attribute.Int(observability.AttrKnowledgeBaseID, int(id)),
	)
	defer func() {
		observability.RecordError(span, err)
		span.End()
	}()

	var kb entity.KnowledgeBase
	if err = s.db.WithContext(ctx).First(&kb, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			err = apperr.New(http.StatusNotFound, fmt.Errorf("knowledge base not found"))
			return err
		}
		err = fmt.Errorf("find knowledge base: %w", err)
		return err
	}
	span.SetAttributes(
		observability.TextAttribute(observability.AttrKnowledgeBaseName, kb.Name),
		attribute.String(observability.AttrCollectionName, kb.CollectionName),
	)

	// Delete Qdrant collection (best-effort: log and continue if it fails).
	if kb.CollectionName != "" && kb.CollectionName != "pending" {
		if err := s.vectors.DeleteCollection(ctx, kb.CollectionName); err != nil {
			log.Printf("WARNING: failed to delete qdrant collection %q: %v", kb.CollectionName, err)
		}
	}

	// Cascade delete: chunks → documents → knowledge base.
	if err = s.db.WithContext(ctx).Where("knowledge_base_id = ?", kb.ID).Delete(&entity.DocumentChunk{}).Error; err != nil {
		err = fmt.Errorf("delete chunks: %w", err)
		return err
	}
	if err = s.db.WithContext(ctx).Where("knowledge_base_id = ?", kb.ID).Delete(&entity.Document{}).Error; err != nil {
		err = fmt.Errorf("delete documents: %w", err)
		return err
	}
	if err = s.db.WithContext(ctx).Delete(&kb).Error; err != nil {
		err = fmt.Errorf("delete knowledge base: %w", err)
		return err
	}

	return nil
}

func (s *KnowledgeBaseService) ListKnowledgeBases(ctx context.Context) (list []entity.KnowledgeBase, err error) {
	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanKnowledgeBaseList,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceKnowledgeBase),
	)
	defer func() {
		span.SetAttributes(attribute.Int("rag.list_count", len(list)))
		observability.RecordError(span, err)
		span.End()
	}()

	if err = s.db.WithContext(ctx).Order("id desc").Find(&list).Error; err != nil {
		err = fmt.Errorf("list knowledge bases: %w", err)
		return nil, err
	}

	return list, nil
}
