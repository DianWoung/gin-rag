package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/dianwang-mac/go-rag/internal/appdto"
	"github.com/dianwang-mac/go-rag/internal/entity"
)

type fakeKnowledgeBaseService struct{}

func (f *fakeKnowledgeBaseService) CreateKnowledgeBase(_ context.Context, _ appdto.CreateKnowledgeBaseRequest) (*entity.KnowledgeBase, error) {
	return nil, nil
}

func (f *fakeKnowledgeBaseService) ListKnowledgeBases(_ context.Context) ([]entity.KnowledgeBase, error) {
	return nil, nil
}

func (f *fakeKnowledgeBaseService) DeleteKnowledgeBase(_ context.Context, _ uint) error {
	return nil
}

type fakeDocumentService struct {
	result         *entity.Document
	err            error
	textReq        appdto.ImportTextDocumentRequest
	pdfReq         appdto.ImportPDFDocumentRequest
	importPDFCalls int
	indexedDocID   uint
	chunks         []entity.DocumentChunk
}

func (f *fakeDocumentService) ImportTextDocument(_ context.Context, req appdto.ImportTextDocumentRequest) (*entity.Document, error) {
	f.textReq = req
	return f.result, f.err
}

func (f *fakeDocumentService) ImportPDFDocument(_ context.Context, req appdto.ImportPDFDocumentRequest) (*entity.Document, error) {
	f.importPDFCalls++
	f.pdfReq = req
	return f.result, f.err
}

func (f *fakeDocumentService) IndexDocument(_ context.Context, documentID uint) (*entity.Document, error) {
	f.indexedDocID = documentID
	return f.result, f.err
}

func (f *fakeDocumentService) ListDocuments(_ context.Context, _ uint) ([]entity.Document, error) {
	return nil, nil
}

func (f *fakeDocumentService) ListDocumentChunks(_ context.Context, _ uint) ([]entity.DocumentChunk, error) {
	return f.chunks, f.err
}

func (f *fakeDocumentService) DeleteDocument(_ context.Context, _ uint) error {
	return nil
}

func TestImportPDFDocumentMultipart(t *testing.T) {
	gin.SetMode(gin.TestMode)

	service := &fakeDocumentService{
		result: &entity.Document{
			ID:              7,
			KnowledgeBaseID: 3,
			Title:           "report.pdf",
			SourceType:      "pdf",
			Status:          "imported",
		},
	}
	handler := NewInternalAPIHandler(&fakeKnowledgeBaseService{}, service)
	router := gin.New()
	router.POST("/api/documents/import-pdf", handler.ImportPDFDocument)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("knowledge_base_id", "3"); err != nil {
		t.Fatalf("WriteField() error = %v", err)
	}
	if err := writer.WriteField("title", "report.pdf"); err != nil {
		t.Fatalf("WriteField() error = %v", err)
	}

	fileWriter, err := writer.CreateFormFile("file", "report.pdf")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	pdfBytes := []byte("%PDF-1.4 fake pdf bytes")
	if _, err := fileWriter.Write(pdfBytes); err != nil {
		t.Fatalf("fileWriter.Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/documents/import-pdf", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", resp.Code, http.StatusCreated, resp.Body.String())
	}
	if service.pdfReq.KnowledgeBaseID != 3 {
		t.Fatalf("KnowledgeBaseID = %d, want 3", service.pdfReq.KnowledgeBaseID)
	}
	if service.pdfReq.Title != "report.pdf" {
		t.Fatalf("Title = %q, want report.pdf", service.pdfReq.Title)
	}
	if service.pdfReq.FileName != "report.pdf" {
		t.Fatalf("FileName = %q, want report.pdf", service.pdfReq.FileName)
	}
	if !bytes.Equal(service.pdfReq.Content, pdfBytes) {
		t.Fatalf("Content = %q, want %q", service.pdfReq.Content, pdfBytes)
	}

	var payload entity.Document
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.SourceType != "pdf" {
		t.Fatalf("SourceType = %q, want pdf", payload.SourceType)
	}
}

func TestImportPDFDocumentRejectsJSONFilePath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	service := &fakeDocumentService{
		result: &entity.Document{ID: 7},
	}
	handler := NewInternalAPIHandler(&fakeKnowledgeBaseService{}, service)
	router := gin.New()
	router.POST("/api/documents/import-pdf", handler.ImportPDFDocument)

	body := []byte(`{"knowledge_base_id":3,"title":"report.pdf","file_path":"/tmp/report.pdf"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/documents/import-pdf", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
	if service.importPDFCalls != 0 {
		t.Fatalf("ImportPDFDocument() calls = %d, want 0", service.importPDFCalls)
	}
}

func TestListDocumentChunksReturnsMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	service := &fakeDocumentService{
		chunks: []entity.DocumentChunk{
			{DocumentID: 9, ChunkIndex: 0, ChunkType: "table", TableID: "table_1", PageNo: 2, Content: "A|B"},
		},
	}
	handler := NewInternalAPIHandler(&fakeKnowledgeBaseService{}, service)
	router := gin.New()
	router.GET("/api/documents/:id/chunks", handler.ListDocumentChunks)

	req := httptest.NewRequest(http.MethodGet, "/api/documents/9/chunks", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}
	var payload struct {
		Data []entity.DocumentChunk `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(payload.Data) != 1 {
		t.Fatalf("len(data)=%d, want 1", len(payload.Data))
	}
	if payload.Data[0].ChunkType != "table" || payload.Data[0].TableID != "table_1" {
		t.Fatalf("chunk metadata = %+v", payload.Data[0])
	}
}
