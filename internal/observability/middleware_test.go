package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestMiddlewareStartsRootSpanForEachRegisteredRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	exporter, restore := installTestTracerProvider(t)
	defer restore()

	router := gin.New()
	router.Use(Middleware())
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Status(http.StatusAccepted)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("span count = %d, want 1", len(spans))
	}
	if spans[0].Name != "http.v1.chat_completions" {
		t.Fatalf("span name = %q, want http.v1.chat_completions", spans[0].Name)
	}
	attrs := spanAttributes(spans[0].Attributes)
	if attrs[AttrTraceRole] != TraceRoleHTTPRequest {
		t.Fatalf("rag.trace_role = %q, want %q", attrs[AttrTraceRole], TraceRoleHTTPRequest)
	}
	if attrs[AttrHTTPStatusCode] != "202" {
		t.Fatalf("http.response.status_code = %q, want 202", attrs[AttrHTTPStatusCode])
	}
}

func TestMiddlewarePropagatesParentSpanContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	exporter, restore := installTestTracerProvider(t)
	defer restore()

	router := gin.New()
	router.Use(Middleware())
	router.GET("/healthz", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	parentCtx, parent := otel.Tracer("test").Start(context.Background(), "parent")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil).WithContext(parentCtx)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	parent.End()

	spans := exporter.GetSpans()
	var childFound bool
	for _, span := range spans {
		if span.Name != "http.healthz" {
			continue
		}
		childFound = true
		if span.Parent.SpanID() != parent.SpanContext().SpanID() {
			t.Fatalf("parent span id = %s, want %s", span.Parent.SpanID(), parent.SpanContext().SpanID())
		}
	}
	if !childFound {
		t.Fatal("http.healthz span not found")
	}
}

func installTestTracerProvider(t *testing.T) (*tracetest.InMemoryExporter, func()) {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)

	return exporter, func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
	}
}

func spanAttributes(attrs []attribute.KeyValue) map[string]string {
	values := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		values[string(attr.Key)] = attr.Value.Emit()
	}

	return values
}
