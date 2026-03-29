package phoenix

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

type Config struct {
	BaseURL     string
	ProjectName string
	APIKey      string
	HTTPClient  *http.Client
}

type Client struct {
	baseURL     string
	projectName string
	apiKey      string
	httpClient  *http.Client
}

func NewClient(cfg Config) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	projectName := strings.TrimSpace(cfg.ProjectName)
	if baseURL == "" {
		return nil, fmt.Errorf("phoenix base url is required")
	}
	if projectName == "" {
		return nil, fmt.Errorf("phoenix project name is required")
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	return &Client{
		baseURL:     baseURL,
		projectName: projectName,
		apiKey:      strings.TrimSpace(cfg.APIKey),
		httpClient:  httpClient,
	}, nil
}

func ConfigFromEnv() (Config, error) {
	baseURL := strings.TrimSpace(os.Getenv("PHOENIX_BASE_URL"))
	if baseURL == "" {
		baseURL = deriveBaseURL(
			firstNonEmpty(
				os.Getenv("PHOENIX_OTLP_ENDPOINT"),
				os.Getenv("PHOENIX_COLLECTOR_ENDPOINT"),
			),
		)
	}

	cfg := Config{
		BaseURL:     baseURL,
		ProjectName: strings.TrimSpace(os.Getenv("PHOENIX_PROJECT_NAME")),
		APIKey:      strings.TrimSpace(os.Getenv("PHOENIX_API_KEY")),
	}
	if cfg.BaseURL == "" {
		return Config{}, fmt.Errorf("PHOENIX_BASE_URL or PHOENIX_OTLP_ENDPOINT is required")
	}
	if cfg.ProjectName == "" {
		return Config{}, fmt.Errorf("PHOENIX_PROJECT_NAME is required")
	}

	return cfg, nil
}

func (c *Client) FetchTrace(ctx context.Context, traceID string) (TraceEnvelope, error) {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return TraceEnvelope{}, fmt.Errorf("trace id is required")
	}

	var (
		cursor string
		spans  []TraceSpan
	)

	for {
		page, nextCursor, err := c.fetchSpanPage(ctx, cursor)
		if err != nil {
			return TraceEnvelope{}, err
		}
		for _, span := range page {
			if span.TraceID == traceID {
				spans = append(spans, span)
			}
		}
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	if len(spans) == 0 {
		return TraceEnvelope{}, fmt.Errorf("trace %q not found in project %q", traceID, c.projectName)
	}

	sort.Slice(spans, func(i, j int) bool {
		return spans[i].StartTime.Before(spans[j].StartTime)
	})

	envelope := TraceEnvelope{
		ProjectName: c.projectName,
		TraceID:     traceID,
		Spans:       spans,
		StartTime:   spans[0].StartTime,
		EndTime:     spans[len(spans)-1].EndTime,
	}
	for i := range spans {
		if spans[i].ParentID == "" {
			envelope.RootSpan = &spans[i]
			break
		}
	}
	if envelope.RootSpan == nil {
		envelope.RootSpan = &spans[0]
	}

	return envelope, nil
}

type spansResponseBody struct {
	Data       []spanDTO `json:"data"`
	NextCursor string    `json:"next_cursor"`
}

type spanDTO struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Context       spanContextDTO `json:"context"`
	ParentID      string         `json:"parent_id"`
	SpanKind      string         `json:"span_kind"`
	StartTime     time.Time      `json:"start_time"`
	EndTime       time.Time      `json:"end_time"`
	StatusCode    string         `json:"status_code"`
	StatusMessage string         `json:"status_message"`
	Attributes    map[string]any `json:"attributes"`
	Events        []eventDTO     `json:"events"`
}

type spanContextDTO struct {
	TraceID string `json:"trace_id"`
	SpanID  string `json:"span_id"`
}

type eventDTO struct {
	Name       string         `json:"name"`
	Timestamp  time.Time      `json:"timestamp"`
	Attributes map[string]any `json:"attributes"`
}

func (c *Client) fetchSpanPage(ctx context.Context, cursor string) ([]TraceSpan, string, error) {
	endpoint, err := url.Parse(c.baseURL + "/v1/projects/" + url.PathEscape(c.projectName) + "/spans")
	if err != nil {
		return nil, "", fmt.Errorf("build spans endpoint: %w", err)
	}

	query := endpoint.Query()
	query.Set("limit", "1000")
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("build spans request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("request phoenix spans: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return nil, "", fmt.Errorf("phoenix auth failed: %s", resp.Status)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, "", fmt.Errorf("phoenix project %q not found", c.projectName)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("phoenix spans request failed: %s", resp.Status)
	}

	var body spansResponseBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, "", fmt.Errorf("decode phoenix spans response: %w", err)
	}

	spans := make([]TraceSpan, 0, len(body.Data))
	for _, item := range body.Data {
		events := make([]TraceEvent, 0, len(item.Events))
		for _, event := range item.Events {
			events = append(events, TraceEvent{
				Name:       event.Name,
				Timestamp:  event.Timestamp,
				Attributes: event.Attributes,
			})
		}
		spans = append(spans, TraceSpan{
			ID:            item.ID,
			Name:          item.Name,
			TraceID:       item.Context.TraceID,
			SpanID:        item.Context.SpanID,
			ParentID:      item.ParentID,
			SpanKind:      item.SpanKind,
			StartTime:     item.StartTime,
			EndTime:       item.EndTime,
			StatusCode:    item.StatusCode,
			StatusMessage: item.StatusMessage,
			Attributes:    item.Attributes,
			Events:        events,
		})
	}

	return spans, body.NextCursor, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	return ""
}

func deriveBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, "/v1/traces")
	raw = strings.TrimSuffix(raw, "/v1")
	return strings.TrimRight(raw, "/")
}
