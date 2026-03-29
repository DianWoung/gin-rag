package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	AppPort     string
	MySQLDSN    string
	AdminAPIKey string
	Qdrant      QdrantConfig
	Chat        ChatConfig
	Embedding   EmbeddingConfig
	Chunking    ChunkingConfig
	Reranker    RerankerConfig
	Tracing     TracingConfig
}

type QdrantConfig struct {
	Host     string
	GRPCPort int
}

type ChatConfig struct {
	BaseURL string
	APIKey  string
	Model   string
}

type EmbeddingConfig struct {
	BaseURL string
	APIKey  string
	Model   string
}

type ChunkingConfig struct {
	ChunkSize    int
	ChunkOverlap int
}

type RerankerConfig struct {
	BaseURL string
}

type TracingConfig struct {
	Enabled        bool
	Endpoint       string
	ProjectName    string
	APIKey         string
	EventBodyLimit int
}

func Load() (*Config, error) {
	mysqlDSN := strings.TrimSpace(os.Getenv("MYSQL_DSN"))
	if mysqlDSN == "" {
		return nil, fmt.Errorf("MYSQL_DSN is required")
	}

	adminAPIKey := strings.TrimSpace(os.Getenv("ADMIN_API_KEY"))
	if adminAPIKey == "" {
		return nil, fmt.Errorf("ADMIN_API_KEY is required")
	}

	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required")
	}

	cfg := &Config{
		AppPort:     readString("APP_PORT", "8080"),
		MySQLDSN:    mysqlDSN,
		AdminAPIKey: adminAPIKey,
		Qdrant: QdrantConfig{
			Host:     readString("QDRANT_HOST", "127.0.0.1"),
			GRPCPort: readInt("QDRANT_GRPC_PORT", 6334),
		},
		Chat: ChatConfig{
			BaseURL: readString("OPENAI_BASE_URL", ""),
			APIKey:  apiKey,
			Model:   readString("OPENAI_CHAT_MODEL", "gpt-4o-mini"),
		},
		Embedding: EmbeddingConfig{
			BaseURL: readFirstString("", "EMBEDDING_BASE_URL", "OPENAI_BASE_URL"),
			APIKey:  readFirstString(apiKey, "EMBEDDING_API_KEY", "OPENAI_API_KEY"),
			Model:   readFirstString("bge-m3", "EMBEDDING_MODEL", "OPENAI_EMBEDDING_MODEL"),
		},
		Chunking: ChunkingConfig{
			ChunkSize:    readInt("CHUNK_SIZE", 800),
			ChunkOverlap: readInt("CHUNK_OVERLAP", 120),
		},
		Reranker: RerankerConfig{
			BaseURL: readString("RERANKER_BASE_URL", ""),
		},
		Tracing: TracingConfig{
			Enabled:        readBool("PHOENIX_TRACING_ENABLED", hasAnyValue("PHOENIX_OTLP_ENDPOINT", "PHOENIX_COLLECTOR_ENDPOINT", "PHOENIX_PROJECT_NAME", "PHOENIX_API_KEY")),
			Endpoint:       readFirstString("", "PHOENIX_OTLP_ENDPOINT", "PHOENIX_COLLECTOR_ENDPOINT"),
			ProjectName:    readString("PHOENIX_PROJECT_NAME", ""),
			APIKey:         readString("PHOENIX_API_KEY", ""),
			EventBodyLimit: readInt("PHOENIX_EVENT_BODY_LIMIT", 8192),
		},
	}

	if cfg.Embedding.BaseURL == "" {
		log.Println("WARNING: EMBEDDING_BASE_URL is empty, embedding requests will fall back to the OpenAI default endpoint")
	}
	if cfg.Tracing.Enabled && cfg.Tracing.Endpoint == "" {
		return nil, fmt.Errorf("PHOENIX_TRACING_ENABLED is true but PHOENIX_OTLP_ENDPOINT is empty")
	}
	if cfg.Tracing.Enabled && cfg.Tracing.ProjectName == "" {
		return nil, fmt.Errorf("PHOENIX_TRACING_ENABLED is true but PHOENIX_PROJECT_NAME is empty")
	}

	return cfg, nil
}

func readString(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	return value
}

func readFirstString(fallback string, keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return value
		}
	}

	return fallback
}

func readInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return parsed
}

func readBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}

	return parsed
}

func hasAnyValue(keys ...string) bool {
	for _, key := range keys {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}

	return false
}
