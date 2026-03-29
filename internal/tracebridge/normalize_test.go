package tracebridge

import (
	"testing"
	"time"

	"github.com/dianwang-mac/go-rag/internal/observability"
	"github.com/dianwang-mac/go-rag/internal/phoenix"
)

func TestNormalizeChatTraceExportsPromptMessages(t *testing.T) {
	now := time.Date(2026, 3, 29, 10, 0, 0, 0, time.UTC)
	trace := phoenix.TraceEnvelope{
		ProjectName: "go-rag",
		TraceID:     "trace-456",
		RootSpan: &phoenix.TraceSpan{
			Name: "http.v1.chat_completions",
		},
		Spans: []phoenix.TraceSpan{
			{
				Name: "http.v1.chat_completions",
			},
			{
				Name: observability.SpanChatCompletion,
				Attributes: map[string]any{
					observability.AttrQuestion: "什么是 RAG",
				},
				StartTime: now,
				EndTime:   now.Add(time.Second),
			},
			{
				Name: observability.SpanChatRAGPrompt,
				Attributes: map[string]any{
					observability.AttrPrompt:   "system: use context\n\nuser: 什么是 RAG",
					"rag.prompt_messages_json": `[{"role":"system","content":"use context"},{"role":"user","content":"什么是 RAG"}]`,
				},
				StartTime: now,
				EndTime:   now.Add(500 * time.Millisecond),
			},
		},
	}

	sample, _, err := NormalizeChatTrace(trace)
	if err != nil {
		t.Fatalf("NormalizeChatTrace() error = %v", err)
	}
	if len(sample.PromptMessages) != 2 {
		t.Fatalf("PromptMessages count = %d, want 2", len(sample.PromptMessages))
	}
	if sample.PromptMessages[0] != (PromptMessage{Role: "system", Content: "use context"}) {
		t.Fatalf("PromptMessages[0] = %+v", sample.PromptMessages[0])
	}
	if sample.PromptMessages[1] != (PromptMessage{Role: "user", Content: "什么是 RAG"}) {
		t.Fatalf("PromptMessages[1] = %+v", sample.PromptMessages[1])
	}
}

func TestNormalizeChatTraceBuildsPromptAndChunks(t *testing.T) {
	now := time.Date(2026, 3, 29, 10, 0, 0, 0, time.UTC)
	trace := phoenix.TraceEnvelope{
		ProjectName: "go-rag",
		TraceID:     "trace-123",
		RootSpan: &phoenix.TraceSpan{
			Name: "http.v1.chat_completions",
		},
		Spans: []phoenix.TraceSpan{
			{
				Name: "http.v1.chat_completions",
			},
			{
				Name: observability.SpanChatCompletion,
				Attributes: map[string]any{
					observability.AttrQuestion:          "什么是 RAG",
					observability.AttrAnswer:            "RAG 是检索增强生成。",
					observability.AttrKnowledgeBaseID:   7.0,
					observability.AttrKnowledgeBaseName: "demo-kb",
					observability.AttrCollectionName:    "kb_7",
					observability.AttrEmbeddingModel:    "bge-m3",
					"rag.model":                         "gpt-4o-mini",
					"rag.temperature":                   0.2,
				},
				StartTime: now,
				EndTime:   now.Add(time.Second),
			},
			{
				Name: observability.SpanChatRAGPrompt,
				Attributes: map[string]any{
					observability.AttrPrompt:          "system: use context\n\nuser: 什么是 RAG",
					observability.AttrOriginalQuery:   "什么是 RAG",
					observability.AttrRewrittenQuery:  "RAG 的定义是什么",
					observability.AttrRetrievedChunks: "chunk-a\n---\nchunk-b",
				},
				StartTime: now,
				EndTime:   now.Add(500 * time.Millisecond),
			},
		},
	}

	sample, warnings, err := NormalizeChatTrace(trace)
	if err != nil {
		t.Fatalf("NormalizeChatTrace() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %+v, want none", warnings)
	}
	if sample.Question != "什么是 RAG" {
		t.Fatalf("Question = %q", sample.Question)
	}
	if sample.Answer != "RAG 是检索增强生成。" {
		t.Fatalf("Answer = %q", sample.Answer)
	}
	if sample.Model != "gpt-4o-mini" {
		t.Fatalf("Model = %q", sample.Model)
	}
	if sample.OriginalQuery != "什么是 RAG" {
		t.Fatalf("OriginalQuery = %q", sample.OriginalQuery)
	}
	if sample.RewrittenQuery != "RAG 的定义是什么" {
		t.Fatalf("RewrittenQuery = %q", sample.RewrittenQuery)
	}
	if len(sample.Chunks) != 2 {
		t.Fatalf("chunk count = %d, want 2", len(sample.Chunks))
	}
}

func TestNormalizeChatTraceFailsOnTruncatedPrompt(t *testing.T) {
	trace := phoenix.TraceEnvelope{
		ProjectName: "go-rag",
		TraceID:     "trace-123",
		RootSpan: &phoenix.TraceSpan{
			Name: "http.v1.chat_completions",
		},
		Spans: []phoenix.TraceSpan{
			{
				Name: observability.SpanChatCompletion,
			},
			{
				Name: observability.SpanChatRAGPrompt,
				Attributes: map[string]any{
					observability.AttrPrompt: "prompt...(truncated)",
				},
			},
		},
	}

	if _, _, err := NormalizeChatTrace(trace); err == nil {
		t.Fatal("NormalizeChatTrace() error = nil, want truncated prompt failure")
	}
}
