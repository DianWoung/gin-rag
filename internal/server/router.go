package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/dianwang-mac/go-rag/internal/handler"
	"github.com/dianwang-mac/go-rag/internal/observability"
	webui "github.com/dianwang-mac/go-rag/web"
)

func NewRouter(adminAPIKey string, internalAPI *handler.InternalAPIHandler, openAI *handler.OpenAIHandler) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery(), observability.Middleware())

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/web/")
	})
	router.StaticFS("/web", webui.FileSystem())

	api := router.Group("/api")
	api.Use(handler.AdminAuthMiddleware(adminAPIKey))
	api.POST("/knowledge-bases", internalAPI.CreateKnowledgeBase)
	api.GET("/knowledge-bases", internalAPI.ListKnowledgeBases)
	api.DELETE("/knowledge-bases/:id", internalAPI.DeleteKnowledgeBase)
	api.POST("/documents/import-text", internalAPI.ImportTextDocument)
	api.POST("/documents/import-pdf", internalAPI.ImportPDFDocument)
	api.POST("/documents/:id/index", internalAPI.IndexDocument)
	api.DELETE("/documents/:id", internalAPI.DeleteDocument)
	api.GET("/documents", internalAPI.ListDocuments)
	api.GET("/debug/phoenix/spans", phoenixSpansProxyHandler())

	router.POST("/v1/chat/completions", openAI.ChatCompletions)

	return router
}
