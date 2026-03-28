package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/qdrant/go-client/qdrant"
)

type ChunkVector struct {
	PointID   string
	DocumentID uint
	ChunkIndex int
	Content    string
	Vector     []float64
}

type SearchResult struct {
	PointID    string
	DocumentID uint
	ChunkIndex int
	Content    string
	Score      float32
}

type QdrantStore struct {
	client *qdrant.Client
}

func NewQdrantStore(host string, port int) (*QdrantStore, error) {
	client, err := qdrant.NewClient(&qdrant.Config{
		Host: host,
		Port: port,
	})
	if err != nil {
		return nil, fmt.Errorf("new qdrant client: %w", err)
	}

	return &QdrantStore{client: client}, nil
}

func (s *QdrantStore) EnsureCollection(ctx context.Context, collectionName string, dimension int) error {
	if dimension <= 0 {
		return fmt.Errorf("invalid vector dimension: %d", dimension)
	}

	err := s.client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     uint64(dimension),
			Distance: qdrant.Distance_Cosine,
		}),
	})
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "already exists") {
		return fmt.Errorf("create qdrant collection: %w", err)
	}

	return nil
}

func (s *QdrantStore) UpsertChunks(ctx context.Context, collectionName string, chunks []ChunkVector) error {
	points := make([]*qdrant.PointStruct, 0, len(chunks))
	for _, chunk := range chunks {
		points = append(points, &qdrant.PointStruct{
			Id:      qdrant.NewIDUUID(chunk.PointID),
			Vectors: qdrant.NewVectors(toFloat32s(chunk.Vector)...),
			Payload: qdrant.NewValueMap(map[string]any{
				"document_id": int64(chunk.DocumentID),
				"chunk_index": int64(chunk.ChunkIndex),
				"content":     chunk.Content,
			}),
		})
	}

	if len(points) == 0 {
		return nil
	}

	_, err := s.client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: collectionName,
		Points:         points,
	})
	if err != nil {
		return fmt.Errorf("upsert qdrant points: %w", err)
	}

	return nil
}

func (s *QdrantStore) Search(ctx context.Context, collectionName string, vector []float64, limit uint64) ([]SearchResult, error) {
	if limit == 0 {
		limit = 4
	}

	resp, err := s.client.Query(ctx, &qdrant.QueryPoints{
		CollectionName: collectionName,
		Query:          qdrant.NewQuery(toFloat32s(vector)...),
		WithPayload:    qdrant.NewWithPayload(true),
		Limit:          &limit,
	})
	if err != nil {
		return nil, fmt.Errorf("query qdrant: %w", err)
	}

	results := make([]SearchResult, 0, len(resp))
	for _, point := range resp {
		results = append(results, SearchResult{
			PointID:    point.GetId().GetUuid(),
			DocumentID: uint(point.GetPayload()["document_id"].GetIntegerValue()),
			ChunkIndex: int(point.GetPayload()["chunk_index"].GetIntegerValue()),
			Content:    point.GetPayload()["content"].GetStringValue(),
			Score:      point.GetScore(),
		})
	}

	return results, nil
}

func toFloat32s(values []float64) []float32 {
	result := make([]float32, 0, len(values))
	for _, value := range values {
		result = append(result, float32(value))
	}

	return result
}
