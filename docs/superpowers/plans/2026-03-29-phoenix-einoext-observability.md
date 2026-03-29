# Phoenix + EinoExt Observability Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Phoenix-backed tracing for the Go RAG service and a CLI-driven offline evaluation loop that exports chat traces, replays persisted prompts, and scores samples with EinoExt.

**Architecture:** Phase 1 instruments the running service with OpenTelemetry spans and structured events through a focused `internal/observability` package. Phase 2 adds offline trace export, normalized sample persistence, prompt-only replay, and EinoExt-based scoring through `internal/phoenix`, `internal/tracebridge`, `internal/eval`, and `cmd/evalctl`.

**Tech Stack:** Go, gin, gorm, MySQL, Qdrant, OpenTelemetry OTLP, Phoenix, Eino, EinoExt

---

## File Map

### New files

- `internal/observability/config.go`
  Purpose: Parse Phoenix / OTLP configuration into a narrow runtime config.
- `internal/observability/schema.go`
  Purpose: Centralize span names, attribute keys, and event names used by live tracing and offline normalization.
- `internal/observability/tracer.go`
  Purpose: Build tracer provider, exporter, and shutdown hooks for Phoenix OTLP.
- `internal/observability/middleware.go`
  Purpose: Start root HTTP spans and attach request metadata.
- `internal/observability/context.go`
  Purpose: Small helpers for span creation from handlers/services.
- `internal/observability/tracer_test.go`
  Purpose: Verify config validation, disabled mode, and provider bootstrap.
- `internal/observability/middleware_test.go`
  Purpose: Verify root span names and request attribute propagation.
- `internal/observability/schema_test.go`
  Purpose: Lock down schema constants used by other packages.
- `internal/phoenix/client.go`
  Purpose: Fetch trace envelopes by trace ID for offline export.
- `internal/phoenix/types.go`
  Purpose: Define `TraceEnvelope`, `TraceSpan`, and event DTOs with stable fields.
- `internal/phoenix/client_test.go`
  Purpose: Verify trace fetch decoding and error mapping.
- `internal/tracebridge/chat_sample.go`
  Purpose: Define normalized chat sample, replay target, warnings, and helper types.
- `internal/tracebridge/normalize.go`
  Purpose: Convert `TraceEnvelope` into a normalized chat sample.
- `internal/tracebridge/normalize_test.go`
  Purpose: Verify root span selection, chunk reconstruction, truncation, and skip/warning behavior.
- `internal/eval/models.go`
  Purpose: GORM models for samples, replay runs, and evaluation results.
- `internal/eval/repository.go`
  Purpose: Repository interfaces and MySQL-backed persistence.
- `internal/eval/repository_test.go`
  Purpose: Verify idempotent export and append-only replay/result behavior.
- `internal/eval/replay.go`
  Purpose: Prompt-only replay runner using persisted prompt and captured model settings.
- `internal/eval/replay_test.go`
  Purpose: Verify replay input/output and model-unavailable errors.
- `internal/eval/metrics.go`
  Purpose: Metric contracts and EinoExt evaluator wiring.
- `internal/eval/metrics_test.go`
  Purpose: Verify score/skip/error behavior for the four metrics.
- `cmd/evalctl/main.go`
  Purpose: CLI entrypoints for `export-trace`, `replay-sample`, and `score-sample`.
- `cmd/evalctl/main_test.go`
  Purpose: Verify CLI argument validation and command routing.

### Existing files to modify

- `internal/config/config.go`
  Purpose: Add Phoenix / OTLP configuration fields.
- `internal/config/config_test.go`
  Purpose: Cover Phoenix config defaults and required combinations.
- `cmd/server/main.go`
  Purpose: Initialize tracing and inject shutdown hooks.
- `internal/server/router.go`
  Purpose: Register tracing middleware.
- `internal/service/chat.go`
  Purpose: Add child spans and structured events for chat flow.
- `internal/service/chat_tracing.go`
  Purpose: Keep chat-specific tracing helpers and event builders out of the main service file when possible.
- `internal/service/document.go`
  Purpose: Add child spans and structured events for ingest flow.
- `internal/service/document_tracing.go`
  Purpose: Keep document-specific tracing helpers and event builders out of the main service file when possible.
- `internal/service/knowledge.go`
  Purpose: Add knowledge-base spans.
- `internal/store/mysql.go`
  Purpose: Add MySQL store-facing spans for query/insert/update operations when service-level spans are too coarse.
- `internal/store/qdrant.go`
  Purpose: Add Qdrant store-facing spans for search, collection management, upsert, and delete operations.
- `internal/handler/openai_test.go`
  Purpose: Keep request path coverage stable while tracing middleware is introduced.
- `internal/handler/internal_api_test.go`
  Purpose: Keep internal API coverage stable while tracing middleware is introduced.
- `internal/store/mysql_tracing_test.go`
  Purpose: Verify MySQL store span names, attributes, and cardinality where store-level wrapping is introduced.
- `internal/store/qdrant_tracing_test.go`
  Purpose: Verify Qdrant store span names, attributes, and cardinality where store-level wrapping is introduced.
- `README.md`
  Purpose: Document Phoenix env vars and `evalctl` workflow after code lands.

### Likely not modified

- `internal/llm/provider.go`

Keep additional files unchanged unless instrumentation cannot be placed cleanly at the service layer.

## Chunk 1: Phase 1 Tracing

### Task 1: Phoenix Config and Tracer Bootstrap

**Files:**
- Create: `internal/observability/config.go`
- Create: `internal/observability/tracer.go`
- Create: `internal/observability/tracer_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Write the failing config tests**

```go
func TestLoadIncludesPhoenixTracingConfig(t *testing.T) {
	t.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/go_rag?parseTime=true")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("PHOENIX_OTLP_ENDPOINT", "http://127.0.0.1:6006")
	t.Setenv("PHOENIX_PROJECT_NAME", "go-rag")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Tracing.Endpoint != "http://127.0.0.1:6006" {
		t.Fatalf("Tracing.Endpoint = %q", cfg.Tracing.Endpoint)
	}
}

func TestLoadRejectsEnabledTracingWithoutEndpoint(t *testing.T) {
	t.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/go_rag?parseTime=true")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("PHOENIX_TRACING_ENABLED", "true")
	t.Setenv("PHOENIX_OTLP_ENDPOINT", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want invalid tracing config error")
	}
}
```

- [ ] **Step 2: Run the config tests to verify they fail**

Run: `go test ./internal/config -run 'TestLoad(IncludesPhoenixTracingConfig|RejectsEnabledTracingWithoutEndpoint|FromEnv)' -v`
Expected: FAIL because `Config` has no tracing fields or tracing validation yet.

- [ ] **Step 3: Add tracing config fields and defaults**

Implement:
- `internal/config/config.go`
  Add `Tracing TracingConfig` to `Config`.
- `internal/observability/config.go`
  Add narrow config with endpoint, headers, project name, disabled flag.
- Parse env vars:
  - `PHOENIX_OTLP_ENDPOINT`
  - `PHOENIX_PROJECT_NAME`
  - `PHOENIX_API_KEY` (optional if server supports anonymous local access)
  - `PHOENIX_TRACING_ENABLED` (default `false`)
  - `PHOENIX_EVENT_BODY_LIMIT` (default `8192`)
- reject invalid enabled configs such as missing endpoint or project name with a clear startup error
- map `PHOENIX_PROJECT_NAME` and `PHOENIX_API_KEY` into OTLP headers in `internal/observability/config.go`
- expose `PHOENIX_EVENT_BODY_LIMIT` on the observability config so `context.go`, `chat_tracing.go`, and `document_tracing.go` can consume one shared limit source

- [ ] **Step 4: Write the failing tracer bootstrap tests**

```go
func TestNewProviderDisabledReturnsNoopShutdown(t *testing.T) {
	cfg := Config{Enabled: false}
	provider, shutdown, err := NewProvider(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	if provider == nil || shutdown == nil {
		t.Fatal("expected non-nil noop provider and shutdown")
	}
}

func TestNewProviderFailsFastOnInvalidEnabledConfig(t *testing.T) {
	cfg := Config{
		Enabled: true,
	}
	if _, _, err := NewProvider(context.Background(), cfg); err == nil {
		t.Fatal("NewProvider() error = nil, want config/bootstrap error")
	}
}
```

- [ ] **Step 5: Run the observability tests to verify they fail**

Run: `go test ./internal/observability -run 'TestNewProvider(DisabledReturnsNoopShutdown|FailsFastOnInvalidEnabledConfig)' -v`
Expected: FAIL because the package, constructor, and bootstrap validation do not exist yet.

- [ ] **Step 6: Implement minimal tracer bootstrap**

Implement:
- `internal/observability/tracer.go`
  - build OTLP HTTP exporter
  - create tracer provider
  - return shutdown func
  - support disabled/noop mode
  - fail fast on invalid enabled configuration or exporter bootstrap failure
  - register the provider as the global OTel tracer provider so middleware and child spans can resolve the same tracer
  - consume prepared OTLP headers containing Phoenix project/auth metadata
- `cmd/server/main.go`
  - initialize provider before router creation
  - defer shutdown on exit

- [ ] **Step 7: Run focused tests**

Run: `go test ./internal/config ./internal/observability ./cmd/server ./internal/server -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/observability/config.go internal/observability/tracer.go internal/observability/tracer_test.go cmd/server/main.go
git commit -m "feat(tracing): bootstrap Phoenix tracer"
```

### Task 2: Root HTTP Spans and Trace Schema

**Files:**
- Create: `internal/observability/schema.go`
- Create: `internal/observability/schema_test.go`
- Create: `internal/observability/middleware.go`
- Create: `internal/observability/middleware_test.go`
- Modify: `internal/server/router.go`
- Test: `internal/handler/openai_test.go`
- Test: `internal/handler/internal_api_test.go`

- [ ] **Step 1: Write failing schema and middleware tests**

```go
func TestRouteSpanName(t *testing.T) {
	if got := RouteSpanName(http.MethodPost, "/v1/chat/completions"); got != "http.v1.chat_completions" {
		t.Fatalf("RouteSpanName() = %q", got)
	}
}

func TestRouteSpanNameCoversInternalAPIRoutes(t *testing.T) {
	tests := map[string]string{
		"/api/knowledge-bases":      "http.api.create_knowledge_base",
		"/api/documents/import-pdf": "http.api.import_pdf_document",
	}
	for route, want := range tests {
		if got := RouteSpanName(http.MethodPost, route); got != want {
			t.Fatalf("RouteSpanName(%q) = %q, want %q", route, got, want)
		}
	}
}

func TestMiddlewareStartsRootSpanForEachRegisteredRoute(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	router := buildTestRouterWithTracing(exporter)

	for _, tc := range []struct {
		method string
		path   string
		want   string
	}{
		{method: http.MethodPost, path: "/v1/chat/completions", want: "http.v1.chat_completions"},
		{method: http.MethodPost, path: "/api/knowledge-bases", want: "http.api.create_knowledge_base"},
		{method: http.MethodGet, path: "/api/knowledge-bases", want: "http.api.list_knowledge_bases"},
		{method: http.MethodDelete, path: "/api/knowledge-bases/1", want: "http.api.delete_knowledge_base"},
		{method: http.MethodPost, path: "/api/documents/import-text", want: "http.api.import_text_document"},
		{method: http.MethodPost, path: "/api/documents/import-pdf", want: "http.api.import_pdf_document"},
		{method: http.MethodPost, path: "/api/documents/1/index", want: "http.api.index_document"},
		{method: http.MethodGet, path: "/api/documents", want: "http.api.list_documents"},
		{method: http.MethodDelete, path: "/api/documents/1", want: "http.api.delete_document"},
	} {
		performRequest(router, tc.method, tc.path)
		assertRootSpanExists(t, exporter, tc.want)
	}
}

func TestMiddlewarePropagatesParentSpanContext(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	router := buildTestRouterWithTracingAndChildSpanHandler(exporter)

	performRequest(router, http.MethodGet, "/test-child-span")

	root := findSpanByName(t, exporter, "http.test_child_span")
	child := findSpanByName(t, exporter, "test.child_span")
	if child.Parent().SpanID() != root.SpanContext().SpanID() {
		t.Fatalf("child parent span id = %s, want %s", child.Parent().SpanID(), root.SpanContext().SpanID())
	}
}
```

- [ ] **Step 2: Run the focused tests to verify they fail**

Run: `go test ./internal/observability -run 'Test(RouteSpanName|MiddlewareStartsRootSpanForEachRegisteredRoute|MiddlewarePropagatesParentSpanContext)' -v`
Expected: FAIL because schema helpers and middleware do not exist yet.

- [ ] **Step 3: Implement trace schema**

Implement:
- root span names from the spec
- common attributes for request ID, KB ID, document ID, collection name, and trace role
- chat attributes for model, temperature, query previews, match count, top K, rerank flag, score list, prompt chars, answer chars, and finish reason
- ingest attributes for file name, source type, chunk count, vector dimension, and document status
- error attributes for error type and error message
- event names used later by chat/document spans
- event metadata keys for `length`, `part_index`, `part_total`, `chunk_index`, `document_id`, `citation_label`, `retrieval_order`, `sample_correlation_id`, and `rag.telemetry_truncated`

Add assertions in `internal/observability/schema_test.go` for:
- route span names
- all internal API root span names from the spec
- required attribute key constants
- required event name constants
- required event metadata key constants

Add emitted-trace assertions in middleware/service/store tests for:
- `rag.trace_role`
- KB/document/collection IDs when available
- `sample_correlation_id` on events that can be normalized later

- [ ] **Step 4: Implement HTTP tracing middleware**

Implement:
- start root span from route template, not raw URL
- attach method, route, status code, and request ID
- store tracer/span in context for handlers/services

- [ ] **Step 5: Wire middleware into the router**

Modify `internal/server/router.go` to register the tracing middleware before handler routes.

- [ ] **Step 6: Update handler tests for middleware without weakening API assertions**

Required updates:
- keep existing response status and payload assertions intact
- adjust router construction to include middleware
- add one assertion that a representative chat route still returns the same body/status under middleware
- add one assertion that a representative internal API route still returns the same body/status under middleware

- [ ] **Step 7: Run focused tests**

Run: `go test ./internal/observability ./internal/handler -run 'Test(ChatCompletions|ImportPDFDocument|RouteSpanName|RouteSpanNameCoversInternalAPIRoutes|MiddlewareStartsRootSpanForEachRegisteredRoute|MiddlewarePropagatesParentSpanContext)' -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/observability/schema.go internal/observability/schema_test.go internal/observability/middleware.go internal/observability/middleware_test.go internal/server/router.go internal/handler/openai_test.go internal/handler/internal_api_test.go
git commit -m "feat(tracing): add root HTTP spans"
```

### Task 3: Chat Service Child Spans and Content Events

**Files:**
- Create: `internal/observability/context.go`
- Modify: `internal/service/chat.go`
- Create: `internal/service/chat_tracing.go`
- Create: `internal/service/chat_tracing_test.go`

- [ ] **Step 1: Write the failing chat tracing tests**

```go
func TestChatCompletionEmitsQueryRewriteAndPromptEvents(t *testing.T) {
	service := newTracedChatServiceWithFakes(
		fakeRewrite("rewritten question"),
		fakeAnswer("grounded answer [1]"),
		fakeMatches(
			match(101, "doc one", "chunk one"),
			match(102, "doc two", "chunk two"),
		),
	)

	result, spans := invokeChatAndCollectSpans(t, service)
	assertContentEqual(t, result.Content, "grounded answer [1]")
	assertSpanNames(t, spans,
		"chat.prepare_request",
		"chat.rewrite_query",
		"embed.query",
		"qdrant.search",
		"chat.build_sources",
		"chat.build_prompt",
		"llm.generate",
		"chat.build_response",
	)
	assertEventCount(t, spans, "input.question", 1)
	assertRetrievedChunkEvent(t, spans, 0, 101, "doc one")
	assertRetrievedChunkEvent(t, spans, 1, 102, "doc two")
	assertPromptPartsContiguous(t, spans)
	assertAnswerPartsPresent(t, spans)
}

func TestChatCompletionStreamRecordsPartialAnswerOnStreamError(t *testing.T) {
	service := newTracedStreamingChatServiceWithError("partial ", io.ErrUnexpectedEOF)

	_, spans := invokeStreamingChatAndCollectSpans(t, service)
	assertSpanNames(t, spans, "llm.stream")
	assertAnswerPartsContain(t, spans, "partial ")
	assertStreamWarningPresent(t, spans)
}

func TestChatCompletionEmitsRerankSpanWhenRerankerEnabled(t *testing.T) {
	service := newTracedChatServiceWithReranker(
		fakeMatches(match(101, "doc one", "chunk one"), match(102, "doc two", "chunk two")),
	)

	_, spans := invokeChatAndCollectSpans(t, service)
	assertSpanNamePresent(t, spans, "rerank.rank")
}

func TestChatCompletionSkipsRerankSpanWhenRerankerDisabled(t *testing.T) {
	service := newTracedChatServiceWithoutReranker(
		fakeMatches(match(101, "doc one", "chunk one")),
	)

	_, spans := invokeChatAndCollectSpans(t, service)
	assertSpanNameAbsent(t, spans, "rerank.rank")
}
```

- [ ] **Step 2: Run the targeted test to verify it fails**

Run: `go test ./internal/service -run TestChatCompletionEmitsQueryRewriteAndPromptEvents -v`
Expected: FAIL because chat spans/events do not exist.

- [ ] **Step 3: Add span helper utilities**

Implement `internal/observability/context.go` with helpers such as:
- `StartChildSpan(ctx, name)`
- `AddTextEvent(span, eventName, text, attrs...)`
- `SetIDs(span, kbID, docID, collection)`
- `AddChunkedTextEvents(span, eventName, text, limit, attrs...)`
- `MarkSpanTruncated(span)`

- [ ] **Step 4: Instrument `internal/service/chat.go`**

Add service-owned spans/events for:
- `chat.prepare_request`
- `chat.rewrite_query`
- `embed.query`
- `rerank.rank`
- `chat.build_sources`
- `chat.build_prompt`
- `llm.generate`
- `llm.stream`
- `chat.build_response`

Keep file boundaries explicit:
- keep `internal/service/chat.go` focused on control flow and service orchestration
- move event-building, chunking, and span-attribute helpers into `internal/service/chat_tracing.go`

Record:
- `input.question`
- `input.history`
- `input.retrieved_chunk`
- `input.prompt_part`
- `output.answer_part`
- preview attributes for original/retrieval query
- event attributes for `length`, `part_index`, `part_total`, `chunk_index`, `retrieval_order`, and truncation markers
- common attributes including `rag.trace_role`
- stable `sample_correlation_id` on emitted events when available

- [ ] **Step 5: Handle streaming end states**

Ensure the implementation records:
- complete final answer when stream finishes normally
- partial answer plus warning metadata when stream ends with error or cancellation

- [ ] **Step 6: Add payload-limit tests and implementation**

Write and satisfy concrete tests:

```go
func TestChatCompletionMarksTruncationOnOversizedPrompt(t *testing.T) {
	service := newTracedChatServiceWithOversizedPrompt(limit: 16)

	_, spans := invokeChatAndCollectSpans(t, service)
	assertEventHasAttribute(t, spans, "input.prompt_part", "rag.telemetry_truncated", true)
}
```

Also verify:
- long answer bodies emitted as one or more `output.answer_part` events
- instrumentation fails open when long-body emission hits configured limits
- event body size limit loaded from `PHOENIX_EVENT_BODY_LIMIT` with default `8192`

- [ ] **Step 7: Run focused tests**

Run: `go test ./internal/service -run 'TestChatCompletion(EmitsQueryRewriteAndPromptEvents|StreamRecordsPartialAnswerOnStreamError|MarksTruncationOnOversizedPrompt|EmitsRerankSpanWhenRerankerEnabled|SkipsRerankSpanWhenRerankerDisabled)' -v`
Expected: PASS.

- [ ] **Step 8: Run broader regression tests**

Run: `go test ./internal/handler ./internal/service -v`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/observability/context.go internal/service/chat.go internal/service/chat_tracing.go internal/service/chat_tracing_test.go
git commit -m "feat(tracing): instrument chat service"
```

### Task 4: Ingest, Knowledge, and Store-Facing Spans

**Files:**
- Modify: `internal/service/document.go`
- Create: `internal/service/document_tracing.go`
- Modify: `internal/service/knowledge.go`
- Modify: `internal/store/mysql.go`
- Modify: `internal/store/qdrant.go`
- Create: `internal/service/document_tracing_test.go`
- Create: `internal/service/knowledge_tracing_test.go`
- Create: `internal/store/mysql_tracing_test.go`
- Create: `internal/store/qdrant_tracing_test.go`

- [ ] **Step 1: Write the failing ingest tracing tests**

```go
func TestIndexDocumentEmitsChunkAndVectorEvents(t *testing.T) {
	service := newTracedDocumentServiceWithFakes(
		fakeDocumentContent("alpha beta gamma"),
		fakeChunkSplit("alpha", "beta"),
		fakeVectors([][]float32{{1, 0, 0}, {0, 1, 0}}),
	)

	_, spans := invokeIndexDocumentAndCollectSpans(t, service)
	assertSpanNames(t, spans,
		"document.split_chunks",
		"embed.document_chunks",
		"mysql.update_document_status",
	)
	assertEventCount(t, spans, "document.raw_text", 1)
	assertDocumentChunkEventsContiguous(t, spans, 2)
	assertAttributeEquals(t, spans, "rag.chunk_count", 2)
	assertAttributeEquals(t, spans, "rag.vector_dimension", 3)
}

func TestDeleteKnowledgeBaseEmitsCollectionDeleteSpan(t *testing.T) {
	service := newTracedKnowledgeServiceWithCollection("kb_1")

	_ = invokeDeleteKnowledgeBaseAndCollectSpans(t, service)
	assertSpanNamePresent(t, collectedSpans(t), "qdrant.delete_collection")
}

func TestQdrantDeletePointsCreatesSpan(t *testing.T) {
	store := newTracedQdrantStoreForTest()

	_ = store.DeletePoints(testContextWithSpan(), "kb_1", []string{"p1"})
	assertStoreSpanWithCollection(t, collectedSpans(t), "qdrant.delete_points", "kb_1")
}

func TestMySQLQueryCreatesSpan(t *testing.T) {
	db := newTracedMySQLForTest()

	_ = runListDocumentsQuery(t, db)
	assertSpanNamePresent(t, collectedSpans(t), "mysql.query")
}

func TestIngestSpanCoverage(t *testing.T) {
	textService := newTracedDocumentServiceWithFakes(
		fakeDocumentContent("alpha beta gamma"),
		fakeChunkSplit("alpha", "beta"),
		fakeVectors([][]float32{{1, 0, 0}, {0, 1, 0}}),
	)
	pdfService := newTracedPDFDocumentServiceWithFakes([]byte("%PDF-1.4 fake pdf"))

	_, textSpans := invokeImportTextAndIndexAndCollectSpans(t, textService)
	_, pdfSpans := invokeImportPDFAndCollectSpans(t, pdfService)
	assertSpanNamePresent(t, textSpans, "document.import_text")
	assertSpanNamePresent(t, textSpans, "mysql.insert")
	assertSpanNamePresent(t, textSpans, "mysql.update")
	assertSpanNamePresent(t, pdfSpans, "document.read_pdf_input")
	assertSpanNamePresent(t, pdfSpans, "document.extract_pdf_text")
}

func TestStoreSpanCoverage(t *testing.T) {
	store := newTracedQdrantStoreForTest()

	_ = store.Search(testContextWithSpan(), "kb_1", []float32{1, 0, 0}, 2)
	_ = store.EnsureCollection(testContextWithSpan(), "kb_1", 3)
	_ = store.UpsertChunks(testContextWithSpan(), "kb_1", fakeChunkVectors())
	assertSpanNamePresent(t, collectedSpans(t), "qdrant.search")
	assertSpanNamePresent(t, collectedSpans(t), "qdrant.ensure_collection")
	assertSpanNamePresent(t, collectedSpans(t), "qdrant.upsert_chunks")
}
```

- [ ] **Step 2: Run the targeted tests to verify they fail**

Run: `go test ./internal/service ./internal/store -run 'Test(IndexDocumentEmitsChunkAndVectorEvents|DeleteKnowledgeBaseEmitsCollectionDeleteSpan|QdrantDeletePointsCreatesSpan|MySQLQueryCreatesSpan)' -v`
Expected: FAIL because ingest/knowledge spans do not exist.

- [ ] **Step 3: Instrument `internal/service/document.go`**

Add service-owned spans/events for:
- `document.import_text`
- `document.read_pdf_input`
- `document.extract_pdf_text`
- `document.split_chunks`
- `embed.document_chunks`
- `mysql.insert_chunks`
- `mysql.update_document_status`

- [ ] **Step 4: Instrument `internal/service/knowledge.go`**

Keep knowledge flow aligned to the spec:
- root HTTP spans stay in middleware only
- service-level instrumentation adds attributes and child store spans only where needed
- `DeleteKnowledgeBase` must emit collection delete behavior through the Qdrant store span path
- keep document event-building and payload-limit helpers in `internal/service/document_tracing.go` so `document.go` stays focused on ingest flow

- [ ] **Step 5: Add store-facing spans**

Instrument store access so the trace includes:
- `mysql.query`
- `mysql.insert`
- `mysql.update`
- `qdrant.search`
- `qdrant.ensure_collection`
- `qdrant.upsert_chunks`
- `qdrant.delete_points`
- `qdrant.delete_collection`

Keep the wrapping focused:
- use store-level spans only where service-level spans are too coarse
- preserve existing store APIs and call signatures
- treat `mysql.*`, `qdrant.search`, `qdrant.ensure_collection`, `qdrant.upsert_chunks`, `qdrant.delete_points`, and `qdrant.delete_collection` as store-owned; service code must not emit duplicate spans with those names

- [ ] **Step 6: Add payload-limit handling for ingest text events**

Write and satisfy tests for:
- oversized `document.raw_text` kept as a single event, truncated in place with visible metadata
- oversized `document.chunk_text` events marked with `rag.telemetry_truncated=true`
- ingest instrumentation failing open when payload limits are exceeded
- event body size limit loaded from `PHOENIX_EVENT_BODY_LIMIT` with default `8192`

- [ ] **Step 7: Add explicit verification for knowledge and store spans**

Add assertions for:
- span names and cardinality
- KB/document/collection identifiers where applicable
- delete operations emitting Qdrant delete spans
- list/query operations emitting MySQL query spans
- coverage for:
  - `document.import_text`
  - `document.read_pdf_input`
  - `document.extract_pdf_text`
  - `mysql.insert`
  - `mysql.update`
  - `qdrant.ensure_collection`
  - `qdrant.upsert_chunks`
  - `qdrant.search`
- emitted `rag.trace_role` and stable `sample_correlation_id` where available

- [ ] **Step 8: Run focused service and store tests**

Run: `go test ./internal/service ./internal/store -run 'Test(IndexDocumentEmitsChunkAndVectorEvents|DeleteKnowledgeBaseEmitsCollectionDeleteSpan|QdrantDeletePointsCreatesSpan|MySQLQueryCreatesSpan|IngestSpanCoverage|StoreSpanCoverage)' -v`
Expected: PASS.

- [ ] **Step 9: Run package regression tests**

Run: `go test ./internal/...`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/service/document.go internal/service/document_tracing.go internal/service/knowledge.go internal/service/document_tracing_test.go internal/service/knowledge_tracing_test.go internal/store/mysql.go internal/store/qdrant.go internal/store/mysql_tracing_test.go internal/store/qdrant_tracing_test.go
git commit -m "feat(tracing): instrument ingest and knowledge flows"
```

## Chunk 2: Phase 2 Export, Replay, and Evaluation

### Task 5: Sample Persistence Models and Repository

**Files:**
- Create: `internal/eval/models.go`
- Create: `internal/eval/repository.go`
- Create: `internal/eval/repository_test.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Write the failing repository tests**

```go
func TestSaveSampleIsIdempotentByTraceID(t *testing.T) {
	// Save the same trace twice and assert one sample row remains.
}

func TestReplayRunsAppendWithoutMutatingPriorRows(t *testing.T) {
	// Save two replay runs for one sample and assert both remain.
}
```

- [ ] **Step 2: Run the repository tests to verify they fail**

Run: `go test ./internal/eval -run 'Test(SaveSampleIsIdempotentByTraceID|ReplayRunsAppendWithoutMutatingPriorRows)' -v`
Expected: FAIL because the package and repository do not exist.

- [ ] **Step 3: Add GORM models and repository interfaces**

Implement:
- sample record with `capture_status`, `warnings_json`, `telemetry_truncated`
- replay run record with `status`, `model`, `prompt_json`, `answer_text`
- evaluation result record with `target`, `replay_run_id`, `metric_name`, `score`, `status`

- [ ] **Step 4: Add migration/bootstrap hook**

Modify `cmd/server/main.go` to auto-migrate the new evaluation tables next to existing metadata models.

- [ ] **Step 5: Run focused tests**

Run: `go test ./internal/eval -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/eval/models.go internal/eval/repository.go internal/eval/repository_test.go cmd/server/main.go
git commit -m "feat(eval): add sample persistence"
```

### Task 6: Phoenix Trace Reader and Trace Normalization

**Files:**
- Create: `internal/phoenix/types.go`
- Create: `internal/phoenix/client.go`
- Create: `internal/phoenix/client_test.go`
- Create: `internal/tracebridge/chat_sample.go`
- Create: `internal/tracebridge/normalize.go`
- Create: `internal/tracebridge/normalize_test.go`

- [ ] **Step 1: Write the failing Phoenix client tests**

```go
func TestFetchTraceDecodesEnvelope(t *testing.T) {
	// Serve a fixture JSON payload and assert TraceEnvelope fields.
}
```

- [ ] **Step 2: Run the client tests to verify they fail**

Run: `go test ./internal/phoenix -run TestFetchTraceDecodesEnvelope -v`
Expected: FAIL because the client package does not exist.

- [ ] **Step 3: Implement `TraceEnvelope` and client**

`TraceEnvelope` must include:
- trace ID
- span IDs and parent span IDs
- span name and status
- span attributes
- ordered events with timestamps and attributes

Implement error mapping for:
- not found
- malformed response
- auth/config failure

- [ ] **Step 4: Write the failing normalization tests**

```go
func TestNormalizeChatTraceBuildsPromptAndChunks(t *testing.T) {
	// Assert sample prompt, chunks, warnings, and replay settings.
}

func TestNormalizeChatTraceFailsOnTruncatedPrompt(t *testing.T) {
	// Assert fatal export failure when prompt parts are truncated.
}
```

- [ ] **Step 5: Run the normalization tests to verify they fail**

Run: `go test ./internal/tracebridge -run 'TestNormalizeChatTrace(BuildsPromptAndChunks|FailsOnTruncatedPrompt)' -v`
Expected: FAIL because normalization code does not exist.

- [ ] **Step 6: Implement normalized sample conversion**

Implement:
- root span selection for `http.v1.chat_completions`
- prompt part reconstruction
- answer part reconstruction
- retrieved chunk reconstruction
- warning generation for missing history / missing citations / incomplete streams
- replay settings capture (`replay_model`, `replay_temperature`, `replay_provider`)

- [ ] **Step 7: Run focused tests**

Run: `go test ./internal/phoenix ./internal/tracebridge -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/phoenix/types.go internal/phoenix/client.go internal/phoenix/client_test.go internal/tracebridge/chat_sample.go internal/tracebridge/normalize.go internal/tracebridge/normalize_test.go
git commit -m "feat(eval): add Phoenix trace normalization"
```

### Task 7: Export CLI and Replay Runner

**Files:**
- Create: `cmd/evalctl/main.go`
- Create: `cmd/evalctl/main_test.go`
- Create: `internal/eval/replay.go`
- Create: `internal/eval/replay_test.go`

- [ ] **Step 1: Write the failing CLI tests**

```go
func TestEvalctlExportTraceRequiresTraceID(t *testing.T) {
	// Assert `export-trace` without arg exits non-zero.
}
```

- [ ] **Step 2: Run the CLI tests to verify they fail**

Run: `go test ./cmd/evalctl -run TestEvalctlExportTraceRequiresTraceID -v`
Expected: FAIL because the CLI does not exist.

- [ ] **Step 3: Implement `evalctl export-trace`**

Wire:
- config load
- Phoenix client
- trace exporter
- repository save

Output:
- sample ID
- warning count
- capture status

- [ ] **Step 4: Write the failing replay tests**

```go
func TestReplayChatSampleUsesPersistedPromptAndSettings(t *testing.T) {
	// Assert replay uses prompt/model/temperature from the sample.
}
```

- [ ] **Step 5: Run replay tests to verify they fail**

Run: `go test ./internal/eval -run TestReplayChatSampleUsesPersistedPromptAndSettings -v`
Expected: FAIL because replay runner does not exist.

- [ ] **Step 6: Implement replay runner and CLI command**

Implement:
- prompt-only replay
- model-unavailable error
- `evalctl replay-sample <sample_id>`
- replay run persistence

- [ ] **Step 7: Run focused tests**

Run: `go test ./cmd/evalctl ./internal/eval -run 'Test(EvalctlExportTraceRequiresTraceID|ReplayChatSampleUsesPersistedPromptAndSettings)' -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add cmd/evalctl/main.go cmd/evalctl/main_test.go internal/eval/replay.go internal/eval/replay_test.go
git commit -m "feat(eval): add export and replay CLI"
```

### Task 8: EinoExt Metrics, Score CLI, and Final Verification

**Files:**
- Create: `internal/eval/metrics.go`
- Create: `internal/eval/metrics_test.go`
- Modify: `cmd/evalctl/main.go`
- Modify: `README.md`

- [ ] **Step 1: Write the failing metric tests**

```go
func TestCitationCorrectnessSkipsWhenChunkReferenceMissing(t *testing.T) {
	// Assert status=skipped and summary explains the missing mapping.
}

func TestGroundedAnswerScoresCapturedAndReplayTargetsSeparately(t *testing.T) {
	// Assert two result rows with target=captured and target=replay.
}
```

- [ ] **Step 2: Run the metric tests to verify they fail**

Run: `go test ./internal/eval -run 'Test(CitationCorrectnessSkipsWhenChunkReferenceMissing|GroundedAnswerScoresCapturedAndReplayTargetsSeparately)' -v`
Expected: FAIL because metric runner does not exist.

- [ ] **Step 3: Implement the four metric contracts**

Implement:
- `retrieval_relevance`
- `grounded_answer`
- `citation_correctness`
- `abstention_quality`

Rules:
- statuses only `scored`, `skipped`, `error`
- warnings for limited/truncated artifacts go into result summary
- results distinguish `target=captured` vs `target=replay`

- [ ] **Step 4: Add `score-sample` command**

Implement:
- load sample
- load latest replay run if present
- score captured target
- score replay target when replay exists
- persist append-only results

- [ ] **Step 5: Update README**

Document:
- Phoenix env vars
- how to run `evalctl export-trace`
- how to run `evalctl replay-sample`
- how to run `evalctl score-sample`

- [ ] **Step 6: Run focused tests**

Run: `go test ./internal/eval ./cmd/evalctl -v`
Expected: PASS.

- [ ] **Step 7: Run final verification**

Run: `go test ./...`
Expected: PASS across the repository.

- [ ] **Step 8: Commit**

```bash
git add internal/eval/metrics.go internal/eval/metrics_test.go cmd/evalctl/main.go README.md
git commit -m "feat(eval): add scoring workflow"
```
