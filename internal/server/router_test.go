package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/dianwang-mac/go-rag/internal/appdto"
	"github.com/dianwang-mac/go-rag/internal/entity"
	"github.com/dianwang-mac/go-rag/internal/handler"
)

type routerTestKnowledgeBaseService struct{}

func (routerTestKnowledgeBaseService) CreateKnowledgeBase(context.Context, appdto.CreateKnowledgeBaseRequest) (*entity.KnowledgeBase, error) {
	return &entity.KnowledgeBase{ID: 1, Name: "kb"}, nil
}

func (routerTestKnowledgeBaseService) ListKnowledgeBases(context.Context) ([]entity.KnowledgeBase, error) {
	return []entity.KnowledgeBase{{ID: 1, Name: "kb"}}, nil
}

func (routerTestKnowledgeBaseService) DeleteKnowledgeBase(context.Context, uint) error {
	return nil
}

type routerTestDocumentService struct{}

func (routerTestDocumentService) ImportTextDocument(context.Context, appdto.ImportTextDocumentRequest) (*entity.Document, error) {
	return &entity.Document{ID: 1}, nil
}

func (routerTestDocumentService) ImportPDFDocument(context.Context, appdto.ImportPDFDocumentRequest) (*entity.Document, error) {
	return &entity.Document{ID: 1}, nil
}

func (routerTestDocumentService) IndexDocument(context.Context, uint) (*entity.Document, error) {
	return &entity.Document{ID: 1}, nil
}

func (routerTestDocumentService) ListDocuments(context.Context, uint) ([]entity.Document, error) {
	return []entity.Document{{ID: 1}}, nil
}

func (routerTestDocumentService) DeleteDocument(context.Context, uint) error {
	return nil
}

type routerTestChatService struct{}

func (routerTestChatService) ChatCompletion(context.Context, handler.ChatRequest) (handler.ChatResult, error) {
	return handler.ChatResult{
		Model:   "gpt-4o-mini",
		Content: "ok",
		Usage:   handler.Usage{TotalTokens: 1},
	}, nil
}

func (routerTestChatService) ChatCompletionStream(context.Context, handler.ChatRequest) (*schema.StreamReader[handler.ChatStreamChunk], error) {
	return schema.StreamReaderFromArray([]handler.ChatStreamChunk{
		{Model: "gpt-4o-mini", Delta: "ok", FinishReason: "stop"},
	}), nil
}

func TestNewRouterProtectsOnlyAdminRoutes(t *testing.T) {
	internalAPI := handler.NewInternalAPIHandler(routerTestKnowledgeBaseService{}, routerTestDocumentService{})
	openAI := handler.NewOpenAIHandler(routerTestChatService{})
	router := NewRouter("test-admin-key", internalAPI, openAI)

	adminReq := httptest.NewRequest(http.MethodGet, "/api/knowledge-bases", nil)
	adminResp := httptest.NewRecorder()
	router.ServeHTTP(adminResp, adminReq)

	if adminResp.Code != http.StatusUnauthorized {
		t.Fatalf("admin status = %d, want %d", adminResp.Code, http.StatusUnauthorized)
	}

	body := map[string]any{
		"knowledge_base_id": 1,
		"messages": []map[string]string{
			{"role": "user", "content": "hello"},
		},
	}
	raw, _ := json.Marshal(body)
	chatReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(raw))
	chatReq.Header.Set("Content-Type", "application/json")
	chatResp := httptest.NewRecorder()
	router.ServeHTTP(chatResp, chatReq)

	if chatResp.Code != http.StatusOK {
		t.Fatalf("chat status = %d, want %d, body = %s", chatResp.Code, http.StatusOK, chatResp.Body.String())
	}
}

func TestNewRouterServesWebConsoleWithoutAdminAuth(t *testing.T) {
	internalAPI := handler.NewInternalAPIHandler(routerTestKnowledgeBaseService{}, routerTestDocumentService{})
	openAI := handler.NewOpenAIHandler(routerTestChatService{})
	router := NewRouter("test-admin-key", internalAPI, openAI)

	req := httptest.NewRequest(http.MethodGet, "/web/", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte("RAG Manual Debug Console")) {
		t.Fatalf("body missing web console marker, got: %s", resp.Body.String())
	}
}
