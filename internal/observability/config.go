package observability

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	appconfig "github.com/dianwang-mac/go-rag/internal/config"
)

const (
	InstrumentationName   = "github.com/dianwang-mac/go-rag/internal/observability"
	ServiceName           = "go-rag"
	DefaultEventBodyLimit = 8192
	ProjectNameAttribute  = "openinference.project.name"
)

type Config struct {
	Enabled        bool
	Endpoint       string
	ProjectName    string
	APIKey         string
	EventBodyLimit int
	Headers        map[string]string
}

func FromTracingConfig(cfg appconfig.TracingConfig) (Config, error) {
	obsCfg := Config{
		Enabled:        cfg.Enabled,
		Endpoint:       normalizeEndpoint(cfg.Endpoint),
		ProjectName:    strings.TrimSpace(cfg.ProjectName),
		APIKey:         strings.TrimSpace(cfg.APIKey),
		EventBodyLimit: cfg.EventBodyLimit,
	}
	if obsCfg.EventBodyLimit <= 0 {
		obsCfg.EventBodyLimit = DefaultEventBodyLimit
	}
	if !obsCfg.Enabled {
		return obsCfg, nil
	}
	if obsCfg.Endpoint == "" {
		return Config{}, fmt.Errorf("tracing enabled but Phoenix OTLP endpoint is empty")
	}
	if obsCfg.ProjectName == "" {
		return Config{}, fmt.Errorf("tracing enabled but Phoenix project name is empty")
	}
	obsCfg.Headers = buildHeaders(obsCfg.APIKey)

	return obsCfg, nil
}

func buildHeaders(apiKey string) map[string]string {
	if apiKey == "" {
		return nil
	}

	return map[string]string{
		"authorization": "Bearer " + apiKey,
	}
}

func normalizeEndpoint(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	cleanPath := path.Clean(parsed.Path)
	switch cleanPath {
	case ".", "/":
		parsed.Path = "/v1/traces"
	case "/v1/traces":
	default:
		parsed.Path = cleanPath
	}

	return parsed.String()
}
