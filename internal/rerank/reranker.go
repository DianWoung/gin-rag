package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

// Result holds a reranked item with its original index and relevance score.
type Result struct {
	Index int
	Score float64
}

// Reranker scores query–document pairs using a cross-encoder model exposed via
// a HuggingFace Text Embeddings Inference (TEI) compatible /rerank endpoint.
type Reranker struct {
	endpoint   string // e.g. "http://127.0.0.1:8081/rerank"
	httpClient *http.Client
}

// New creates a Reranker pointing at the given TEI base URL.
// baseURL should be the root (e.g. "http://127.0.0.1:8081"), without trailing slash.
func New(baseURL string) *Reranker {
	return &Reranker{
		endpoint: baseURL + "/rerank",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type rerankRequest struct {
	Query      string   `json:"query"`
	Texts      []string `json:"texts"`
	RawScores  bool     `json:"raw_scores"`
	ReturnText bool     `json:"return_text"`
}

type rerankResponseItem struct {
	Index int     `json:"index"`
	Score float64 `json:"score"`
}

// Rank sends query and texts to the reranker and returns results sorted by
// descending score. At most topK results are returned; pass 0 for all.
func (r *Reranker) Rank(ctx context.Context, query string, texts []string, topK int) ([]Result, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	body, err := json.Marshal(rerankRequest{
		Query:      query,
		Texts:      texts,
		RawScores:  false,
		ReturnText: false,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal rerank request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new rerank request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rerank request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("rerank returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var items []rerankResponseItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("decode rerank response: %w", err)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Score > items[j].Score
	})

	if topK > 0 && topK < len(items) {
		items = items[:topK]
	}

	results := make([]Result, len(items))
	for i, item := range items {
		results[i] = Result{Index: item.Index, Score: item.Score}
	}
	return results, nil
}
