package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	AppPort   string
	MySQLDSN  string
	Qdrant    QdrantConfig
	Chat      ChatConfig
	Embedding EmbeddingConfig
	Chunking  ChunkingConfig
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

func Load() (*Config, error) {
	mysqlDSN := strings.TrimSpace(os.Getenv("MYSQL_DSN"))
	if mysqlDSN == "" {
		return nil, fmt.Errorf("MYSQL_DSN is required")
	}

	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required")
	}

	cfg := &Config{
		AppPort:  readString("APP_PORT", "8080"),
		MySQLDSN: mysqlDSN,
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
	}

	if cfg.Embedding.BaseURL == "" {
		log.Println("WARNING: EMBEDDING_BASE_URL is empty, embedding requests will fall back to the OpenAI default endpoint")
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
