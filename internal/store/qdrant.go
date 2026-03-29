package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/dianwang-mac/go-rag/internal/observability"
	"github.com/qdrant/go-client/qdrant"
	"go.opentelemetry.io/otel/attribute"
)

type ChunkVector struct {
	PointID    string
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
	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanQdrantEnsureCollection,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleVectorStore),
		attribute.String(observability.AttrCollectionName, collectionName),
		attribute.Int("rag.vector_dimension", dimension),
	)
	defer span.End()

	if dimension <= 0 {
		err := fmt.Errorf("invalid vector dimension: %d", dimension)
		observability.RecordError(span, err)
		return err
	}

	err := s.client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     uint64(dimension),
			Distance: qdrant.Distance_Cosine,
		}),
	})
	if err == nil {
		return nil
	}

	if !strings.Contains(strings.ToLower(err.Error()), "already exists") {
		err = fmt.Errorf("create qdrant collection: %w", err)
		observability.RecordError(span, err)
		return err
	}

	// Collection already exists — verify its vector dimension matches.
	info, infoErr := s.client.GetCollectionInfo(ctx, collectionName)
	if infoErr != nil {
		err = fmt.Errorf("get qdrant collection info: %w", infoErr)
		observability.RecordError(span, err)
		return err
	}

	existing := int(info.GetConfig().GetParams().GetVectorsConfig().GetParams().GetSize())
	if existing != dimension {
		err = fmt.Errorf(
			"vector dimension mismatch: collection %q has dimension %d, but embedding model produces dimension %d (did you change the embedding model?)",
			collectionName, existing, dimension,
		)
		observability.RecordError(span, err)
		return err
	}

	return nil
}

func (s *QdrantStore) UpsertChunks(ctx context.Context, collectionName string, chunks []ChunkVector) error {
	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanQdrantUpsertChunks,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleVectorStore),
		attribute.String(observability.AttrCollectionName, collectionName),
		attribute.Int(observability.AttrChunkCount, len(chunks)),
	)
	defer span.End()

	points := make([]*qdrant.PointStruct, 0, len(chunks))
	contents := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		contents = append(contents, chunk.Content)
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
	span.SetAttributes(observability.TextListAttribute(observability.AttrChunkBodies, contents))

	_, err := s.client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: collectionName,
		Points:         points,
	})
	if err != nil {
		err = fmt.Errorf("upsert qdrant points: %w", err)
		observability.RecordError(span, err)
		return err
	}

	return nil
}

func (s *QdrantStore) Search(ctx context.Context, collectionName string, vector []float64, limit uint64) ([]SearchResult, error) {
	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanQdrantSearch,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleVectorStore),
		attribute.String(observability.AttrCollectionName, collectionName),
		attribute.Int("rag.search_limit", int(limit)),
		attribute.Int("rag.query_vector_dim", len(vector)),
	)
	defer span.End()

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
		err = fmt.Errorf("query qdrant: %w", err)
		observability.RecordError(span, err)
		return nil, err
	}

	results := make([]SearchResult, 0, len(resp))
	contents := make([]string, 0, len(resp))
	for _, point := range resp {
		content := point.GetPayload()["content"].GetStringValue()
		contents = append(contents, content)
		results = append(results, SearchResult{
			PointID:    point.GetId().GetUuid(),
			DocumentID: uint(point.GetPayload()["document_id"].GetIntegerValue()),
			ChunkIndex: int(point.GetPayload()["chunk_index"].GetIntegerValue()),
			Content:    content,
			Score:      point.GetScore(),
		})
	}
	span.SetAttributes(
		attribute.Int(observability.AttrMatchCount, len(results)),
		observability.TextListAttribute(observability.AttrRetrievedChunks, contents),
	)

	return results, nil
}

func (s *QdrantStore) DeletePoints(ctx context.Context, collectionName string, pointIDs []string) error {
	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanQdrantDeletePoints,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleVectorStore),
		attribute.String(observability.AttrCollectionName, collectionName),
		attribute.Int("rag.point_count", len(pointIDs)),
	)
	defer span.End()

	if len(pointIDs) == 0 {
		return nil
	}

	ids := make([]*qdrant.PointId, 0, len(pointIDs))
	for _, id := range pointIDs {
		ids = append(ids, qdrant.NewIDUUID(id))
	}

	_, err := s.client.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: collectionName,
		Points: &qdrant.PointsSelector{
			PointsSelectorOneOf: &qdrant.PointsSelector_Points{
				Points: &qdrant.PointsIdsList{Ids: ids},
			},
		},
	})
	if err != nil {
		err = fmt.Errorf("delete qdrant points: %w", err)
		observability.RecordError(span, err)
		return err
	}
	return nil
}

func (s *QdrantStore) DeleteCollection(ctx context.Context, collectionName string) error {
	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanQdrantDeleteCollection,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleVectorStore),
		attribute.String(observability.AttrCollectionName, collectionName),
	)
	defer span.End()

	err := s.client.DeleteCollection(ctx, collectionName)
	if err != nil {
		err = fmt.Errorf("delete qdrant collection: %w", err)
		observability.RecordError(span, err)
		return err
	}
	return nil
}

func toFloat32s(values []float64) []float32 {
	result := make([]float32, 0, len(values))
	for _, value := range values {
		result = append(result, float32(value))
	}

	return result
}
