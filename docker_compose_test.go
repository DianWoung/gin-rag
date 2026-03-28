package main

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	DependsOn  []string          `yaml:"depends_on"`
	Environment map[string]string `yaml:"environment"`
}

func TestDockerComposeWiresEmbeddingService(t *testing.T) {
	raw, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}

	var cfg composeFile
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal docker-compose.yml: %v", err)
	}

	if _, ok := cfg.Services["embedding"]; !ok {
		t.Fatal("embedding service missing from docker-compose.yml")
	}

	app, ok := cfg.Services["app"]
	if !ok {
		t.Fatal("app service missing from docker-compose.yml")
	}

	if !contains(app.DependsOn, "embedding") {
		t.Fatalf("app depends_on = %v, want embedding", app.DependsOn)
	}

	if got := app.Environment["EMBEDDING_BASE_URL"]; got != "http://embedding:80/v1" {
		t.Fatalf("app EMBEDDING_BASE_URL = %q, want http://embedding:80/v1", got)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}

	return false
}
