package phoenix

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchTraceDecodesEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q, want Bearer test-key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [
				{
					"id": "root-id",
					"name": "http.v1.chat_completions",
					"context": {"trace_id": "trace-123", "span_id": "root-span"},
					"parent_id": "",
					"span_kind": "CHAIN",
					"start_time": "2026-03-29T08:00:00Z",
					"end_time": "2026-03-29T08:00:01Z",
					"status_code": "OK",
					"status_message": "",
					"attributes": {"http.route": "/v1/chat/completions"},
					"events": []
				},
				{
					"id": "child-id",
					"name": "service.chat.completion",
					"context": {"trace_id": "trace-123", "span_id": "child-span"},
					"parent_id": "root-id",
					"span_kind": "CHAIN",
					"start_time": "2026-03-29T08:00:00.100Z",
					"end_time": "2026-03-29T08:00:00.900Z",
					"status_code": "OK",
					"status_message": "",
					"attributes": {"rag.question": "what is rag"},
					"events": [{"name":"evt","timestamp":"2026-03-29T08:00:00.500Z","attributes":{"k":"v"}}]
				}
			],
			"next_cursor": ""
		}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:     server.URL,
		ProjectName: "go-rag",
		APIKey:      "test-key",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	envelope, err := client.FetchTrace(context.Background(), "trace-123")
	if err != nil {
		t.Fatalf("FetchTrace() error = %v", err)
	}
	if envelope.TraceID != "trace-123" {
		t.Fatalf("TraceID = %q, want trace-123", envelope.TraceID)
	}
	if envelope.RootSpan == nil || envelope.RootSpan.Name != "http.v1.chat_completions" {
		t.Fatalf("RootSpan = %+v, want http.v1.chat_completions", envelope.RootSpan)
	}
	if len(envelope.Spans) != 2 {
		t.Fatalf("span count = %d, want 2", len(envelope.Spans))
	}
	if len(envelope.Spans[1].Events) != 1 {
		t.Fatalf("event count = %d, want 1", len(envelope.Spans[1].Events))
	}
}
