package eval

import (
	"testing"

	"github.com/dianwang-mac/go-rag/internal/tracebridge"
)

func TestSampleRecordRoundTripsPromptMessages(t *testing.T) {
	record, err := NewSampleRecord(tracebridge.ChatSample{
		TraceID:        "trace-123",
		ProjectName:    "go-rag",
		RootSpanName:   "http.v1.chat_completions",
		Question:       "什么是 RAG",
		OriginalQuery:  "什么是 RAG",
		RewrittenQuery: "什么是检索增强生成",
		Prompt:         "system: use context\n\nuser: 什么是 RAG",
		PromptMessages: []tracebridge.PromptMessage{
			{Role: "system", Content: "use context"},
			{Role: "user", Content: "什么是 RAG"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewSampleRecord() error = %v", err)
	}

	stored, err := record.ToStoredSample()
	if err != nil {
		t.Fatalf("ToStoredSample() error = %v", err)
	}

	if len(stored.Sample.PromptMessages) != 2 {
		t.Fatalf("PromptMessages count = %d, want 2", len(stored.Sample.PromptMessages))
	}
	if stored.Sample.PromptMessages[0] != (tracebridge.PromptMessage{Role: "system", Content: "use context"}) {
		t.Fatalf("PromptMessages[0] = %+v", stored.Sample.PromptMessages[0])
	}
	if stored.Sample.PromptMessages[1] != (tracebridge.PromptMessage{Role: "user", Content: "什么是 RAG"}) {
		t.Fatalf("PromptMessages[1] = %+v", stored.Sample.PromptMessages[1])
	}
	if stored.Sample.OriginalQuery != "什么是 RAG" {
		t.Fatalf("OriginalQuery = %q", stored.Sample.OriginalQuery)
	}
	if stored.Sample.RewrittenQuery != "什么是检索增强生成" {
		t.Fatalf("RewrittenQuery = %q", stored.Sample.RewrittenQuery)
	}
}
