package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dianwang-mac/go-rag/internal/config"
	"github.com/dianwang-mac/go-rag/internal/handler"
	"github.com/dianwang-mac/go-rag/internal/ingest"
	"github.com/dianwang-mac/go-rag/internal/llm"
	"github.com/dianwang-mac/go-rag/internal/observability"
	"github.com/dianwang-mac/go-rag/internal/rerank"
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

	obsCfg, err := observability.FromTracingConfig(cfg.Tracing)
	if err != nil {
		log.Fatalf("build tracing config: %v", err)
	}
	_, shutdownTracing, err := observability.NewProvider(ctx, obsCfg)
	if err != nil {
		log.Fatalf("init tracing: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(shutdownCtx); err != nil {
			log.Printf("shutdown tracing: %v", err)
		}
	}()

	db, err := store.OpenMySQL(cfg.MySQLDSN)
	if err != nil {
		log.Fatalf("open mysql: %v", err)
	}

	vectorStore, err := store.NewQdrantStore(cfg.Qdrant.Host, cfg.Qdrant.GRPCPort)
	if err != nil {
		log.Fatalf("open qdrant: %v", err)
	}

	provider := llm.NewProvider(cfg.Chat, cfg.Embedding)

	pingCtx, pingCancel := context.WithTimeout(ctx, 30*time.Second)
	defer pingCancel()
	if err := provider.PingEmbedding(pingCtx); err != nil {
		log.Fatalf("ping embedding service: %v", err)
	}
	log.Println("embedding service is reachable")

	splitter := ingest.NewSplitter(cfg.Chunking.ChunkSize, cfg.Chunking.ChunkOverlap)

	var reranker *rerank.Reranker
	if cfg.Reranker.BaseURL != "" {
		reranker = rerank.New(cfg.Reranker.BaseURL)
		log.Printf("reranker enabled: %s", cfg.Reranker.BaseURL)
	}

	kbService := service.NewKnowledgeBaseService(db, vectorStore, cfg.Embedding.Model)
	documentService := service.NewDocumentService(db, splitter, vectorStore, provider)
	chatService := service.NewChatService(db, vectorStore, provider, reranker)

	internalAPI := handler.NewInternalAPIHandler(kbService, documentService)
	openAI := handler.NewOpenAIHandler(chatService)
	router := server.NewRouter(cfg.AdminAPIKey, internalAPI, openAI)

	srv := &http.Server{
		Addr:              ":" + cfg.AppPort,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("starting server on :%s", cfg.AppPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen and serve: %v", err)
		}
	}()

	<-quit
	log.Println("shutting down server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 15*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server shutdown: %v", err)
	}
	log.Println("server stopped")
}
