package handler

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/dianwang-mac/go-rag/internal/appdto"
	"github.com/dianwang-mac/go-rag/internal/entity"
)

type KnowledgeBaseService interface {
	CreateKnowledgeBase(ctx context.Context, req appdto.CreateKnowledgeBaseRequest) (*entity.KnowledgeBase, error)
	ListKnowledgeBases(ctx context.Context) ([]entity.KnowledgeBase, error)
	DeleteKnowledgeBase(ctx context.Context, id uint) error
}

type DocumentService interface {
	ImportTextDocument(ctx context.Context, req appdto.ImportTextDocumentRequest) (*entity.Document, error)
	ImportPDFDocument(ctx context.Context, req appdto.ImportPDFDocumentRequest) (*entity.Document, error)
	IndexDocument(ctx context.Context, documentID uint) (*entity.Document, error)
	ListDocuments(ctx context.Context, knowledgeBaseID uint) ([]entity.Document, error)
	ListDocumentChunks(ctx context.Context, documentID uint) ([]entity.DocumentChunk, error)
	DeleteDocument(ctx context.Context, documentID uint) error
}

type InternalAPIHandler struct {
	knowledgeBases KnowledgeBaseService
	documents      DocumentService
}

func NewInternalAPIHandler(knowledgeBases KnowledgeBaseService, documents DocumentService) *InternalAPIHandler {
	return &InternalAPIHandler{
		knowledgeBases: knowledgeBases,
		documents:      documents,
	}
}

func (h *InternalAPIHandler) CreateKnowledgeBase(c *gin.Context) {
	var req appdto.CreateKnowledgeBaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, badRequest("invalid knowledge base payload"))
		return
	}

	kb, err := h.knowledgeBases.CreateKnowledgeBase(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusCreated, kb)
}

func (h *InternalAPIHandler) ListKnowledgeBases(c *gin.Context) {
	list, err := h.knowledgeBases.ListKnowledgeBases(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *InternalAPIHandler) ImportTextDocument(c *gin.Context) {
	req, err := parseImportTextRequest(c)
	if err != nil {
		writeError(c, err)
		return
	}

	doc, err := h.documents.ImportTextDocument(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusCreated, doc)
}

func (h *InternalAPIHandler) ImportPDFDocument(c *gin.Context) {
	req, err := parseImportPDFRequest(c)
	if err != nil {
		writeError(c, err)
		return
	}

	doc, err := h.documents.ImportPDFDocument(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusCreated, doc)
}

func (h *InternalAPIHandler) IndexDocument(c *gin.Context) {
	documentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		writeError(c, badRequest("invalid document id"))
		return
	}

	doc, err := h.documents.IndexDocument(c.Request.Context(), uint(documentID))
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, doc)
}

func (h *InternalAPIHandler) ListDocuments(c *gin.Context) {
	var knowledgeBaseID uint64
	var err error
	if raw := strings.TrimSpace(c.Query("knowledge_base_id")); raw != "" {
		knowledgeBaseID, err = strconv.ParseUint(raw, 10, 64)
		if err != nil {
			writeError(c, badRequest("invalid knowledge_base_id"))
			return
		}
	}

	list, err := h.documents.ListDocuments(c.Request.Context(), uint(knowledgeBaseID))
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *InternalAPIHandler) ListDocumentChunks(c *gin.Context) {
	documentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		writeError(c, badRequest("invalid document id"))
		return
	}

	list, err := h.documents.ListDocumentChunks(c.Request.Context(), uint(documentID))
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *InternalAPIHandler) DeleteKnowledgeBase(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		writeError(c, badRequest("invalid knowledge base id"))
		return
	}

	if err := h.knowledgeBases.DeleteKnowledgeBase(c.Request.Context(), uint(id)); err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

func (h *InternalAPIHandler) DeleteDocument(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		writeError(c, badRequest("invalid document id"))
		return
	}

	if err := h.documents.DeleteDocument(c.Request.Context(), uint(id)); err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

func parseImportTextRequest(c *gin.Context) (appdto.ImportTextDocumentRequest, error) {
	var req appdto.ImportTextDocumentRequest
	contentType := c.GetHeader("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := c.ShouldBind(&req); err != nil {
			return req, badRequest("invalid multipart payload")
		}

		if req.Content == "" {
			file, err := c.FormFile("file")
			if err != nil {
				return req, badRequest("content or file is required")
			}

			src, err := file.Open()
			if err != nil {
				return req, err
			}
			defer src.Close()

			raw, err := io.ReadAll(src)
			if err != nil {
				return req, err
			}
			req.Content = string(raw)
		}

		return req, nil
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		return req, badRequest("invalid document payload")
	}

	return req, nil
}

func parseImportPDFRequest(c *gin.Context) (appdto.ImportPDFDocumentRequest, error) {
	var req appdto.ImportPDFDocumentRequest
	contentType := c.GetHeader("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := c.ShouldBind(&req); err != nil {
			return req, badRequest("invalid multipart payload")
		}

		file, err := c.FormFile("file")
		if err != nil {
			return req, badRequest("pdf file is required")
		}

		src, err := file.Open()
		if err != nil {
			return req, err
		}
		defer src.Close()

		raw, err := io.ReadAll(src)
		if err != nil {
			return req, err
		}

		req.FileName = file.Filename
		req.Content = raw
		if strings.TrimSpace(req.Title) == "" {
			req.Title = file.Filename
		}

		return req, nil
	}

	return req, badRequest("pdf multipart upload is required")
}
