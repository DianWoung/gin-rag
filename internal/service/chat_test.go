package service

import (
	"context"
	"fmt"
	"testing"

	einoembedding "github.com/cloudwego/eino/components/embedding"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/dianwang-mac/go-rag/internal/appdto"
	"github.com/dianwang-mac/go-rag/internal/entity"
	"github.com/dianwang-mac/go-rag/internal/store"
)

type fakeChatVectorStore struct {
	results        []store.SearchResult
	err            error
	lastCollection string
	lastLimit      uint64
	lastFilter     store.SearchFilter
}

func (f *fakeChatVectorStore) Search(_ context.Context, collectionName string, _ []float64, limit uint64, filter store.SearchFilter) ([]store.SearchResult, error) {
	f.lastCollection = collectionName
	f.lastLimit = limit
	f.lastFilter = filter
	if f.err != nil {
		return nil, f.err
	}
	return append([]store.SearchResult(nil), f.results...), nil
}

type fakeChatModel struct {
	response string
}

func (f fakeChatModel) Generate(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	return &schema.Message{
		Role:    schema.Assistant,
		Content: f.response,
	}, nil
}

func (f fakeChatModel) Stream(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray([]*schema.Message{{
		Role:    schema.Assistant,
		Content: f.response,
	}}), nil
}

type fakeChatEmbedder struct {
	vectors [][]float64
}

func (f fakeChatEmbedder) EmbedStrings(_ context.Context, _ []string, _ ...einoembedding.Option) ([][]float64, error) {
	return f.vectors, nil
}

type fakeChatProvider struct {
	model    einomodel.BaseChatModel
	embedder einoembedding.Embedder
}

func (f fakeChatProvider) DefaultChatModel() string {
	return "fake-chat"
}

func (f fakeChatProvider) NewChatModel(context.Context, string, float32) (einomodel.BaseChatModel, error) {
	return f.model, nil
}

func (f fakeChatProvider) NewEmbedder(context.Context, string) (einoembedding.Embedder, error) {
	return f.embedder, nil
}

func openChatTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	if err := db.AutoMigrate(&entity.KnowledgeBase{}, &entity.Document{}, &entity.DocumentChunk{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	return db
}

func TestChatCompletionPassesMetadataFiltersToVectorSearch(t *testing.T) {
	db := openChatTestDB(t)

	kb := entity.KnowledgeBase{
		Name:            "kb",
		CollectionName:  "kb_1",
		EmbeddingModel:  "mini-lm",
		VectorDimension: 3,
	}
	if err := db.Create(&kb).Error; err != nil {
		t.Fatalf("Create(kb) error = %v", err)
	}
	doc := entity.Document{
		KnowledgeBaseID: kb.ID,
		Title:           "policy doc",
		SourceType:      "policy",
		Status:          "indexed",
		Content:         "rag policy body",
	}
	if err := db.Create(&doc).Error; err != nil {
		t.Fatalf("Create(doc) error = %v", err)
	}

	vectors := &fakeChatVectorStore{
		results: []store.SearchResult{{
			PointID:    "p1",
			DocumentID: doc.ID,
			ChunkIndex: 0,
			Content:    "rag policy body",
			Score:      0.9,
		}},
	}
	service := &ChatService{
		db:      db,
		vectors: vectors,
		provider: fakeChatProvider{
			model:    fakeChatModel{response: "policy answer [1]"},
			embedder: fakeChatEmbedder{vectors: [][]float64{{0.1, 0.2, 0.3}}},
		},
	}

	_, err := service.ChatCompletion(context.Background(), appdto.ChatRequest{
		KnowledgeBaseID: kb.ID,
		DocumentIDs:     []uint{doc.ID},
		SourceTypes:     []string{"policy"},
		Messages: []appdto.ChatMessage{
			{Role: "user", Content: "tell me the policy"},
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}

	if vectors.lastCollection != kb.CollectionName {
		t.Fatalf("lastCollection = %q, want %q", vectors.lastCollection, kb.CollectionName)
	}
	if vectors.lastLimit != retrievalTopK {
		t.Fatalf("lastLimit = %d, want %d", vectors.lastLimit, retrievalTopK)
	}
	if got := vectors.lastFilter.DocumentIDs; len(got) != 1 || got[0] != doc.ID {
		t.Fatalf("DocumentIDs = %#v, want [%d]", got, doc.ID)
	}
	if got := vectors.lastFilter.SourceTypes; len(got) != 1 || got[0] != "policy" {
		t.Fatalf("SourceTypes = %#v, want [policy]", got)
	}
}

func TestSortMatchesForPromptIsDeterministic(t *testing.T) {
	input := []store.SearchResult{
		{PointID: "b", DocumentID: 2, ChunkIndex: 1, Score: 0.8},
		{PointID: "a", DocumentID: 1, ChunkIndex: 2, Score: 0.8},
		{PointID: "c", DocumentID: 1, ChunkIndex: 1, Score: 0.8},
		{PointID: "d", DocumentID: 3, ChunkIndex: 0, Score: 0.9},
	}

	got := sortMatchesForPrompt(input)

	wantOrder := []string{"d", "c", "a", "b"}
	for i, want := range wantOrder {
		if got[i].PointID != want {
			t.Fatalf("PointID at %d = %q, want %q", i, got[i].PointID, want)
		}
	}
}
