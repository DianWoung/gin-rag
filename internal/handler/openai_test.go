package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
)

type fakeChatService struct {
	result       ChatResult
	stream       *schema.StreamReader[ChatStreamChunk]
	err          error
	req          ChatRequest
	streamedReq  ChatRequest
}

func (f *fakeChatService) ChatCompletion(_ context.Context, req ChatRequest) (ChatResult, error) {
	f.req = req
	return f.result, f.err
}

func (f *fakeChatService) ChatCompletionStream(_ context.Context, req ChatRequest) (*schema.StreamReader[ChatStreamChunk], error) {
	f.streamedReq = req
	return f.stream, f.err
}

func TestChatCompletionsStreamsSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)

	service := &fakeChatService{
		stream: schema.StreamReaderFromArray([]ChatStreamChunk{
			{Delta: "grounded "},
			{Delta: "answer"},
			{FinishReason: "stop"},
		}),
	}
	handler := NewOpenAIHandler(service)
	router := gin.New()
	router.POST("/v1/chat/completions", handler.ChatCompletions)

	body := map[string]any{
		"model":             "gpt-4o-mini",
		"knowledge_base_id": 1,
		"stream":            true,
		"messages": []map[string]string{
			{"role": "user", "content": "hello"},
		},
	}

	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", resp.Code, http.StatusOK, resp.Body.String())
	}
	if contentType := resp.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", contentType)
	}

	rawLines := strings.Split(strings.TrimSpace(resp.Body.String()), "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) != 4 {
		t.Fatalf("line count = %d, want 4, body = %q", len(lines), resp.Body.String())
	}

	var chunk1 struct {
		Object  string `json:"object"`
		Choices []struct {
			Delta struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[0], "data: ")), &chunk1); err != nil {
		t.Fatalf("json.Unmarshal(chunk1) error = %v", err)
	}
	if chunk1.Object != "chat.completion.chunk" {
		t.Fatalf("Object = %q, want chat.completion.chunk", chunk1.Object)
	}
	if got := chunk1.Choices[0].Delta.Role; got != "assistant" {
		t.Fatalf("delta.role = %q, want assistant", got)
	}
	if got := chunk1.Choices[0].Delta.Content; got != "grounded " {
		t.Fatalf("delta.content = %q, want grounded ", got)
	}

	var chunk2 struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[1], "data: ")), &chunk2); err != nil {
		t.Fatalf("json.Unmarshal(chunk2) error = %v", err)
	}
	if got := chunk2.Choices[0].Delta.Content; got != "answer" {
		t.Fatalf("delta.content = %q, want answer", got)
	}

	var chunk3 struct {
		Choices []struct {
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[2], "data: ")), &chunk3); err != nil {
		t.Fatalf("json.Unmarshal(chunk3) error = %v", err)
	}
	if chunk3.Choices[0].FinishReason == nil || *chunk3.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish_reason = %+v, want stop", chunk3.Choices[0].FinishReason)
	}
	if lines[3] != "data: [DONE]" {
		t.Fatalf("last line = %q, want data: [DONE]", lines[3])
	}

	if !service.streamedReq.Stream {
		t.Fatalf("Stream = %v, want true", service.streamedReq.Stream)
	}
}

func TestChatCompletionsMapsRequestAndResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	service := &fakeChatService{
		result: ChatResult{
			Model:   "gpt-4o-mini",
			Content: "grounded answer",
			Usage: Usage{
				PromptTokens:     11,
				CompletionTokens: 7,
				TotalTokens:      18,
			},
		},
	}
	handler := NewOpenAIHandler(service)
	router := gin.New()
	router.POST("/v1/chat/completions", handler.ChatCompletions)

	body := map[string]any{
		"model":             "gpt-4o-mini",
		"knowledge_base_id": 99,
		"document_ids":      []uint{7, 8},
		"source_types":      []string{"policy", "faq"},
		"temperature":       0.2,
		"messages": []map[string]string{
			{"role": "system", "content": "be precise"},
			{"role": "user", "content": "what is rag"},
		},
	}

	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}

	if service.req.KnowledgeBaseID != 99 {
		t.Fatalf("KnowledgeBaseID = %d, want 99", service.req.KnowledgeBaseID)
	}
	if got := service.req.DocumentIDs; len(got) != 2 || got[0] != 7 || got[1] != 8 {
		t.Fatalf("DocumentIDs = %#v, want [7 8]", got)
	}
	if got := service.req.SourceTypes; len(got) != 2 || got[0] != "policy" || got[1] != "faq" {
		t.Fatalf("SourceTypes = %#v, want [policy faq]", got)
	}
	if service.req.Model != "gpt-4o-mini" {
		t.Fatalf("Model = %q, want gpt-4o-mini", service.req.Model)
	}

	var payload struct {
		Model   string `json:"model"`
		Object  string `json:"object"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage Usage `json:"usage"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if payload.Object != "chat.completion" {
		t.Fatalf("Object = %q, want chat.completion", payload.Object)
	}
	if payload.Model != "gpt-4o-mini" {
		t.Fatalf("Model = %q, want gpt-4o-mini", payload.Model)
	}
	if len(payload.Choices) != 1 || payload.Choices[0].Message.Content != "grounded answer" {
		t.Fatalf("choices = %+v, want grounded answer", payload.Choices)
	}
	if payload.Usage.TotalTokens != 18 {
		t.Fatalf("TotalTokens = %d, want 18", payload.Usage.TotalTokens)
	}
}
