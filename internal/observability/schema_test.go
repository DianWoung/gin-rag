package observability

import (
	"net/http"
	"strings"
	"testing"
)

func TestRouteSpanName(t *testing.T) {
	if got := RouteSpanName(http.MethodPost, "/v1/chat/completions"); got != "http.v1.chat_completions" {
		t.Fatalf("RouteSpanName() = %q, want http.v1.chat_completions", got)
	}
}

func TestRouteSpanNameCoversInternalAPIRoutes(t *testing.T) {
	tests := map[string]string{
		http.MethodPost + " /api/knowledge-bases":      "http.api.create_knowledge_base",
		http.MethodPost + " /api/documents/import-pdf": "http.api.import_pdf_document",
		http.MethodGet + " /api/documents":             "http.api.list_documents",
	}

	for key, want := range tests {
		parts := strings.SplitN(key, " ", 2)
		if got := RouteSpanName(parts[0], parts[1]); got != want {
			t.Fatalf("RouteSpanName(%q, %q) = %q, want %q", parts[0], parts[1], got, want)
		}
	}
}
