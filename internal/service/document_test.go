package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	einoembedding "github.com/cloudwego/eino/components/embedding"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/dianwang-mac/go-rag/internal/entity"
	"github.com/dianwang-mac/go-rag/internal/ingest"
	"github.com/dianwang-mac/go-rag/internal/store"
)

type fakeEmbedder struct {
	vectors [][]float64
	err     error
}

func (f fakeEmbedder) EmbedStrings(context.Context, []string, ...einoembedding.Option) ([][]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.vectors, nil
}

type fakeEmbeddingProvider struct {
	embedder einoembedding.Embedder
	err      error
}

func (f fakeEmbeddingProvider) NewEmbedder(context.Context, string) (einoembedding.Embedder, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.embedder, nil
}

type fakeVectorStore struct {
	upsertErr         error
	deleteErr         error
	ensureCalls       int
	upsertCalls       int
	deleteCalls       int
	lastCollection    string
	lastUpsertChunks  []store.ChunkVector
	lastDeletedPoints []string
}

func (f *fakeVectorStore) EnsureCollection(context.Context, string, int) error {
	f.ensureCalls++
	return nil
}

func (f *fakeVectorStore) UpsertChunks(_ context.Context, collectionName string, chunks []store.ChunkVector) error {
	f.upsertCalls++
	f.lastCollection = collectionName
	f.lastUpsertChunks = append([]store.ChunkVector(nil), chunks...)
	if f.upsertErr != nil {
		return f.upsertErr
	}
	return nil
}

func (f *fakeVectorStore) DeletePoints(_ context.Context, collectionName string, pointIDs []string) error {
	f.deleteCalls++
	f.lastCollection = collectionName
	f.lastDeletedPoints = append([]string(nil), pointIDs...)
	return f.deleteErr
}

func openDocumentTestDB(t *testing.T) *gorm.DB {
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

func seedIndexedDocumentFixture(t *testing.T, db *gorm.DB, status string) (entity.KnowledgeBase, entity.Document) {
	t.Helper()

	kb := entity.KnowledgeBase{
		Name:            "kb",
		CollectionName:  "kb_1",
		EmbeddingModel:  "bge-m3",
		VectorDimension: 2,
	}
	if err := db.Create(&kb).Error; err != nil {
		t.Fatalf("Create(kb) error = %v", err)
	}

	doc := entity.Document{
		KnowledgeBaseID: kb.ID,
		Title:           "doc",
		SourceType:      "text",
		Status:          status,
		Content:         "alpha beta gamma delta",
	}
	if err := db.Create(&doc).Error; err != nil {
		t.Fatalf("Create(doc) error = %v", err)
	}

	return kb, doc
}

func newDocumentServiceForTest(db *gorm.DB, vectors *fakeVectorStore, provider fakeEmbeddingProvider) *DocumentService {
	return &DocumentService{
		db:       db,
		splitter: ingest.NewSplitter(100, 0),
		vectors:  vectors,
		provider: provider,
	}
}

func TestIndexDocumentMarksIndexedOnSuccess(t *testing.T) {
	db := openDocumentTestDB(t)
	_, doc := seedIndexedDocumentFixture(t, db, "imported")
	vectors := &fakeVectorStore{}
	service := newDocumentServiceForTest(db, vectors, fakeEmbeddingProvider{
		embedder: fakeEmbedder{vectors: [][]float64{{1, 2}}},
	})

	got, err := service.IndexDocument(context.Background(), doc.ID)
	if err != nil {
		t.Fatalf("IndexDocument() error = %v", err)
	}
	if got.Status != "indexed" {
		t.Fatalf("Status = %q, want indexed", got.Status)
	}
	if len(vectors.lastUpsertChunks) != 1 {
		t.Fatalf("upsert chunk count = %d, want 1", len(vectors.lastUpsertChunks))
	}
	if vectors.lastUpsertChunks[0].SourceType != doc.SourceType {
		t.Fatalf("SourceType = %q, want %q", vectors.lastUpsertChunks[0].SourceType, doc.SourceType)
	}
	if vectors.lastUpsertChunks[0].Title != doc.Title {
		t.Fatalf("Title = %q, want %q", vectors.lastUpsertChunks[0].Title, doc.Title)
	}
}

func TestIndexDocumentAllowsRetryFromFailed(t *testing.T) {
	db := openDocumentTestDB(t)
	_, doc := seedIndexedDocumentFixture(t, db, "failed")
	service := newDocumentServiceForTest(db, &fakeVectorStore{}, fakeEmbeddingProvider{
		embedder: fakeEmbedder{vectors: [][]float64{{1, 2}}},
	})

	got, err := service.IndexDocument(context.Background(), doc.ID)
	if err != nil {
		t.Fatalf("IndexDocument() error = %v", err)
	}
	if got.Status != "indexed" {
		t.Fatalf("Status = %q, want indexed", got.Status)
	}
}

func TestIndexDocumentRejectsIndexedDocument(t *testing.T) {
	db := openDocumentTestDB(t)
	_, doc := seedIndexedDocumentFixture(t, db, "indexed")
	service := newDocumentServiceForTest(db, &fakeVectorStore{}, fakeEmbeddingProvider{
		embedder: fakeEmbedder{vectors: [][]float64{{1, 2}}},
	})

	if _, err := service.IndexDocument(context.Background(), doc.ID); err == nil {
		t.Fatal("IndexDocument() error = nil, want already indexed error")
	}
}

func TestIndexDocumentRejectsConcurrentIndexingState(t *testing.T) {
	db := openDocumentTestDB(t)
	_, doc := seedIndexedDocumentFixture(t, db, "indexing")
	service := newDocumentServiceForTest(db, &fakeVectorStore{}, fakeEmbeddingProvider{
		embedder: fakeEmbedder{vectors: [][]float64{{1, 2}}},
	})

	if _, err := service.IndexDocument(context.Background(), doc.ID); err == nil {
		t.Fatal("IndexDocument() error = nil, want indexing-state rejection")
	}
}

func TestIndexDocumentMarksFailedWhenQdrantUpsertFails(t *testing.T) {
	db := openDocumentTestDB(t)
	_, doc := seedIndexedDocumentFixture(t, db, "imported")
	service := newDocumentServiceForTest(db, &fakeVectorStore{upsertErr: errors.New("upsert failed")}, fakeEmbeddingProvider{
		embedder: fakeEmbedder{vectors: [][]float64{{1, 2}}},
	})

	if _, err := service.IndexDocument(context.Background(), doc.ID); err == nil {
		t.Fatal("IndexDocument() error = nil, want qdrant upsert failure")
	}

	var stored entity.Document
	if err := db.First(&stored, doc.ID).Error; err != nil {
		t.Fatalf("First(document) error = %v", err)
	}
	if stored.Status != "failed" {
		t.Fatalf("Status = %q, want failed", stored.Status)
	}
}

func TestIndexDocumentDeletesQdrantPointsWhenChunkInsertFails(t *testing.T) {
	db := openDocumentTestDB(t)
	kb, doc := seedIndexedDocumentFixture(t, db, "imported")
	if err := db.Exec("CREATE TRIGGER fail_document_chunks_insert BEFORE INSERT ON document_chunks BEGIN SELECT RAISE(FAIL, 'chunk insert failed'); END;").Error; err != nil {
		t.Fatalf("create trigger error = %v", err)
	}

	vectors := &fakeVectorStore{}
	service := newDocumentServiceForTest(db, vectors, fakeEmbeddingProvider{
		embedder: fakeEmbedder{vectors: [][]float64{{1, 2}}},
	})

	if _, err := service.IndexDocument(context.Background(), doc.ID); err == nil {
		t.Fatal("IndexDocument() error = nil, want chunk insert failure")
	}
	if vectors.deleteCalls == 0 {
		t.Fatal("DeletePoints() calls = 0, want compensation delete")
	}
	if vectors.lastCollection != kb.CollectionName {
		t.Fatalf("lastCollection = %q, want %q", vectors.lastCollection, kb.CollectionName)
	}
	if len(vectors.lastDeletedPoints) == 0 {
		t.Fatal("lastDeletedPoints = empty, want generated point ids")
	}
}
