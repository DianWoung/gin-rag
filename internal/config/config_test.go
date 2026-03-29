package config

import (
	"os"
	"testing"
)

func TestLoadFromEnvAppliesDefaults(t *testing.T) {
	t.Setenv("APP_PORT", "")
	t.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/go_rag?parseTime=true")
	t.Setenv("ADMIN_API_KEY", "test-admin-key")
	t.Setenv("QDRANT_HOST", "")
	t.Setenv("QDRANT_GRPC_PORT", "")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_CHAT_MODEL", "")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("EMBEDDING_API_KEY", "")
	t.Setenv("EMBEDDING_BASE_URL", "")
	t.Setenv("EMBEDDING_MODEL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.AppPort != "8080" {
		t.Fatalf("AppPort = %s, want 8080", cfg.AppPort)
	}
	if cfg.Qdrant.Host != "127.0.0.1" {
		t.Fatalf("Qdrant.Host = %s, want 127.0.0.1", cfg.Qdrant.Host)
	}
	if cfg.Qdrant.GRPCPort != 6334 {
		t.Fatalf("Qdrant.GRPCPort = %d, want 6334", cfg.Qdrant.GRPCPort)
	}
	if cfg.Chat.Model != "gpt-4o-mini" {
		t.Fatalf("Chat.Model = %s, want gpt-4o-mini", cfg.Chat.Model)
	}
	if cfg.Embedding.Model != "bge-m3" {
		t.Fatalf("Embedding.Model = %s, want bge-m3", cfg.Embedding.Model)
	}
	if cfg.Embedding.APIKey != "test-key" {
		t.Fatalf("Embedding.APIKey = %s, want test-key", cfg.Embedding.APIKey)
	}
	if cfg.Embedding.BaseURL != "" {
		t.Fatalf("Embedding.BaseURL = %s, want empty", cfg.Embedding.BaseURL)
	}
}

func TestLoadFromEnvRequiresSecretsAndDSN(t *testing.T) {
	for _, key := range []string{
		"MYSQL_DSN",
		"ADMIN_API_KEY",
		"OPENAI_API_KEY",
		"APP_PORT",
		"QDRANT_HOST",
		"QDRANT_GRPC_PORT",
		"OPENAI_CHAT_MODEL",
		"EMBEDDING_API_KEY",
		"EMBEDDING_BASE_URL",
		"EMBEDDING_MODEL",
		"OPENAI_EMBEDDING_MODEL",
		"OPENAI_BASE_URL",
	} {
		t.Setenv(key, "")
	}
	os.Unsetenv("MYSQL_DSN")
	os.Unsetenv("ADMIN_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func TestLoadFromEnvUsesEmbeddingOverrides(t *testing.T) {
	t.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/go_rag?parseTime=true")
	t.Setenv("ADMIN_API_KEY", "test-admin-key")
	t.Setenv("OPENAI_API_KEY", "chat-key")
	t.Setenv("OPENAI_BASE_URL", "https://chat.example.com/v1")
	t.Setenv("OPENAI_CHAT_MODEL", "gpt-4.1-mini")
	t.Setenv("EMBEDDING_API_KEY", "embed-key")
	t.Setenv("EMBEDDING_BASE_URL", "http://127.0.0.1:6008/v1")
	t.Setenv("EMBEDDING_MODEL", "bge-m3")
	t.Setenv("OPENAI_EMBEDDING_MODEL", "legacy-embedding")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Chat.APIKey != "chat-key" {
		t.Fatalf("Chat.APIKey = %s, want chat-key", cfg.Chat.APIKey)
	}
	if cfg.Chat.BaseURL != "https://chat.example.com/v1" {
		t.Fatalf("Chat.BaseURL = %s, want https://chat.example.com/v1", cfg.Chat.BaseURL)
	}
	if cfg.Embedding.APIKey != "embed-key" {
		t.Fatalf("Embedding.APIKey = %s, want embed-key", cfg.Embedding.APIKey)
	}
	if cfg.Embedding.BaseURL != "http://127.0.0.1:6008/v1" {
		t.Fatalf("Embedding.BaseURL = %s, want http://127.0.0.1:6008/v1", cfg.Embedding.BaseURL)
	}
	if cfg.Embedding.Model != "bge-m3" {
		t.Fatalf("Embedding.Model = %s, want bge-m3", cfg.Embedding.Model)
	}
}

func TestLoadFromEnvFallsBackToLegacyEmbeddingEnv(t *testing.T) {
	t.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/go_rag?parseTime=true")
	t.Setenv("ADMIN_API_KEY", "test-admin-key")
	t.Setenv("OPENAI_API_KEY", "chat-key")
	t.Setenv("OPENAI_BASE_URL", "https://chat.example.com/v1")
	t.Setenv("OPENAI_EMBEDDING_MODEL", "legacy-embedding")
	t.Setenv("EMBEDDING_API_KEY", "")
	t.Setenv("EMBEDDING_BASE_URL", "")
	t.Setenv("EMBEDDING_MODEL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Embedding.APIKey != "chat-key" {
		t.Fatalf("Embedding.APIKey = %s, want chat-key", cfg.Embedding.APIKey)
	}
	if cfg.Embedding.BaseURL != "https://chat.example.com/v1" {
		t.Fatalf("Embedding.BaseURL = %s, want https://chat.example.com/v1", cfg.Embedding.BaseURL)
	}
	if cfg.Embedding.Model != "legacy-embedding" {
		t.Fatalf("Embedding.Model = %s, want legacy-embedding", cfg.Embedding.Model)
	}
}

func TestLoadFromEnvStillRequiresOpenAIAPIKeyWhenEmbeddingConfigured(t *testing.T) {
	t.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/go_rag?parseTime=true")
	t.Setenv("ADMIN_API_KEY", "test-admin-key")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", "https://api.openai.com/v1")
	t.Setenv("EMBEDDING_API_KEY", "-")
	t.Setenv("EMBEDDING_BASE_URL", "http://127.0.0.1:6008/v1")
	t.Setenv("EMBEDDING_MODEL", "BAAI/bge-m3")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want OPENAI_API_KEY required error")
	}
}

func TestLoadIncludesPhoenixTracingConfig(t *testing.T) {
	t.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/go_rag?parseTime=true")
	t.Setenv("ADMIN_API_KEY", "test-admin-key")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("PHOENIX_OTLP_ENDPOINT", "http://127.0.0.1:6006")
	t.Setenv("PHOENIX_PROJECT_NAME", "go-rag")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Tracing.Enabled {
		t.Fatal("Tracing.Enabled = false, want true")
	}
	if cfg.Tracing.Endpoint != "http://127.0.0.1:6006" {
		t.Fatalf("Tracing.Endpoint = %q, want http://127.0.0.1:6006", cfg.Tracing.Endpoint)
	}
	if cfg.Tracing.ProjectName != "go-rag" {
		t.Fatalf("Tracing.ProjectName = %q, want go-rag", cfg.Tracing.ProjectName)
	}
	if cfg.Tracing.EventBodyLimit != 8192 {
		t.Fatalf("Tracing.EventBodyLimit = %d, want 8192", cfg.Tracing.EventBodyLimit)
	}
}

func TestLoadRejectsEnabledTracingWithoutEndpoint(t *testing.T) {
	t.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/go_rag?parseTime=true")
	t.Setenv("ADMIN_API_KEY", "test-admin-key")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("PHOENIX_TRACING_ENABLED", "true")
	t.Setenv("PHOENIX_OTLP_ENDPOINT", "")
	t.Setenv("PHOENIX_PROJECT_NAME", "go-rag")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want invalid tracing config error")
	}
}

func TestLoadRequiresAdminAPIKey(t *testing.T) {
	t.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/go_rag?parseTime=true")
	t.Setenv("ADMIN_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "test-key")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want ADMIN_API_KEY required error")
	}
}

func TestLoadIncludesAdminAPIKey(t *testing.T) {
	t.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/go_rag?parseTime=true")
	t.Setenv("ADMIN_API_KEY", "test-admin-key")
	t.Setenv("OPENAI_API_KEY", "test-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AdminAPIKey != "test-admin-key" {
		t.Fatalf("AdminAPIKey = %q, want test-admin-key", cfg.AdminAPIKey)
	}
}
