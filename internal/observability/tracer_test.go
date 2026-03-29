package observability

import (
	"context"
	"testing"
)

func TestNewProviderDisabledReturnsNoopShutdown(t *testing.T) {
	cfg := Config{Enabled: false}

	provider, shutdown, err := NewProvider(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	if provider == nil {
		t.Fatal("provider = nil, want non-nil")
	}
	if shutdown == nil {
		t.Fatal("shutdown = nil, want non-nil")
	}
}

func TestNewProviderFailsFastOnInvalidEnabledConfig(t *testing.T) {
	cfg := Config{Enabled: true}

	if _, _, err := NewProvider(context.Background(), cfg); err == nil {
		t.Fatal("NewProvider() error = nil, want config/bootstrap error")
	}
}
