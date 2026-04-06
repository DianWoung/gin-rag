package store

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/dianwang-mac/go-rag/internal/observability"
	"github.com/qdrant/go-client/qdrant"
	"go.opentelemetry.io/otel/attribute"
)

type ChunkVector struct {
	PointID    string
	DocumentID uint
	ChunkIndex int
	ChunkType  string
	TableID    string
	PageNo     int
	Title      string
	SourceType string
	Content    string
	Vector     []float64
}

type SearchResult struct {
	PointID    string
	DocumentID uint
	ChunkIndex int
	ChunkType  string
	TableID    string
	PageNo     int
	Title      string
	SourceType string
	Content    string
	Score      float32
}

type SearchFilter struct {
	DocumentIDs []uint
	SourceTypes []string
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
				"chunk_type":  chunk.ChunkType,
				"table_id":    chunk.TableID,
				"page_no":     int64(chunk.PageNo),
				"title":       chunk.Title,
				"source_type": chunk.SourceType,
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

func (s *QdrantStore) Search(ctx context.Context, collectionName string, vector []float64, limit uint64, filter SearchFilter) ([]SearchResult, error) {
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
		Filter:         buildSearchFilter(filter),
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
		payload := point.GetPayload()
		content := payloadString(payload, "content")
		contents = append(contents, content)
		results = append(results, SearchResult{
			PointID:    point.GetId().GetUuid(),
			DocumentID: uint(payloadInt(payload, "document_id")),
			ChunkIndex: int(payloadInt(payload, "chunk_index")),
			ChunkType:  firstNonEmpty(payloadString(payload, "chunk_type"), "text"),
			TableID:    payloadString(payload, "table_id"),
			PageNo:     int(payloadInt(payload, "page_no")),
			Title:      payloadString(payload, "title"),
			SourceType: payloadString(payload, "source_type"),
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

func buildSearchFilter(filter SearchFilter) *qdrant.Filter {
	conditions := make([]*qdrant.Condition, 0, 2)

	if ids := normalizedDocumentIDs(filter.DocumentIDs); len(ids) > 0 {
		conditions = append(conditions, qdrant.NewMatchInts("document_id", ids...))
	}
	if sourceTypes := normalizedSourceTypes(filter.SourceTypes); len(sourceTypes) > 0 {
		conditions = append(conditions, qdrant.NewMatchKeywords("source_type", sourceTypes...))
	}
	if len(conditions) == 0 {
		return nil
	}

	return &qdrant.Filter{Must: conditions}
}

func normalizedDocumentIDs(ids []uint) []int64 {
	set := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		set[id] = struct{}{}
	}

	normalized := make([]int64, 0, len(set))
	for id := range set {
		normalized = append(normalized, int64(id))
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i] < normalized[j]
	})
	return normalized
}

func normalizedSourceTypes(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		set[value] = struct{}{}
	}

	normalized := make([]string, 0, len(set))
	for value := range set {
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	return normalized
}

func payloadString(payload map[string]*qdrant.Value, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	return value.GetStringValue()
}

func payloadInt(payload map[string]*qdrant.Value, key string) int64 {
	value, ok := payload[key]
	if !ok || value == nil {
		return 0
	}
	return value.GetIntegerValue()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
