package main

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

type dependsOnEntry struct {
	Condition string `yaml:"condition"`
}

type composeDependsOn map[string]dependsOnEntry

func (d *composeDependsOn) UnmarshalYAML(value *yaml.Node) error {
	*d = make(composeDependsOn)

	// short form: depends_on: [a, b]
	if value.Kind == yaml.SequenceNode {
		var list []string
		if err := value.Decode(&list); err != nil {
			return err
		}
		for _, name := range list {
			(*d)[name] = dependsOnEntry{}
		}
		return nil
	}

	// long form: depends_on: { a: {condition: ...} }
	var m map[string]dependsOnEntry
	if err := value.Decode(&m); err != nil {
		return err
	}
	*d = m
	return nil
}

type composeService struct {
	DependsOn   composeDependsOn  `yaml:"depends_on"`
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

	if _, ok := app.DependsOn["embedding"]; !ok {
		t.Fatalf("app depends_on = %v, want embedding", app.DependsOn)
	}

	if got := app.Environment["EMBEDDING_BASE_URL"]; got != "http://embedding:80/v1" && got != "${EMBEDDING_BASE_URL:-http://embedding:80/v1}" {
		t.Fatalf("app EMBEDDING_BASE_URL = %q, want compose default for http://embedding:80/v1", got)
	}
}

