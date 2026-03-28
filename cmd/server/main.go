package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/dianwang-mac/go-rag/internal/config"
	"github.com/dianwang-mac/go-rag/internal/handler"
	"github.com/dianwang-mac/go-rag/internal/ingest"
	"github.com/dianwang-mac/go-rag/internal/llm"
	"github.com/dianwang-mac/go-rag/internal/server"
	"github.com/dianwang-mac/go-rag/internal/service"
	"github.com/dianwang-mac/go-rag/internal/store"
)

func main() {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := store.OpenMySQL(cfg.MySQLDSN)
	if err != nil {
		log.Fatalf("open mysql: %v", err)
	}

	vectorStore, err := store.NewQdrantStore(cfg.Qdrant.Host, cfg.Qdrant.GRPCPort)
	if err != nil {
		log.Fatalf("open qdrant: %v", err)
	}

	provider := llm.NewProvider(cfg.Chat, cfg.Embedding)

	if err := provider.PingEmbedding(ctx); err != nil {
		log.Fatalf("ping embedding service: %v", err)
	}
	log.Println("embedding service is reachable")

	splitter := ingest.NewSplitter(cfg.Chunking.ChunkSize, cfg.Chunking.ChunkOverlap)

	kbService := service.NewKnowledgeBaseService(db, cfg.Embedding.Model)
	documentService := service.NewDocumentService(db, splitter, vectorStore, provider)
	chatService := service.NewChatService(db, vectorStore, provider)

	internalAPI := handler.NewInternalAPIHandler(kbService, documentService)
	openAI := handler.NewOpenAIHandler(chatService)
	router := server.NewRouter(internalAPI, openAI)

	srv := &http.Server{
		Addr:              ":" + cfg.AppPort,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("starting server on :%s", cfg.AppPort)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen and serve: %v", err)
	}

	_ = ctx
}
