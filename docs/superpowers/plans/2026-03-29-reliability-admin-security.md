# Reliability and Admin Security Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make document indexing retryable after partial failures and protect `/api/*` with admin authentication while removing unsafe PDF file-path import over HTTP.

**Architecture:** Keep the existing single-process `handler -> service -> store` layout and synchronous indexing flow. Harden it with an explicit document status state machine, compensating cleanup between Qdrant and MySQL, and a dedicated admin middleware applied only to the management routes.

**Tech Stack:** Go 1.25, gin, gorm, MySQL, Qdrant, Eino, OpenTelemetry

---

## File Map

### Existing files to modify

- `cmd/server/main.go`
  - Wire the admin auth config into router construction.
- `internal/config/config.go`
  - Add `AdminAPIKey` to config and validate it.
- `internal/config/config_test.go`
  - Cover config loading and validation for admin auth.
- `internal/server/router.go`
  - Apply auth middleware only to `/api/*`.
- `internal/appdto/knowledge.go`
  - Remove `FilePath` from `ImportPDFDocumentRequest`.
- `internal/handler/internal_api.go`
  - Stop accepting `file_path` and keep only multipart upload for PDF.
- `internal/handler/internal_api_test.go`
  - Cover auth failures and PDF contract change.
- `internal/service/document.go`
  - Replace the current chunk-count guard with state-based indexing and compensation logic.
- `README.md`
  - Document admin auth and PDF import contract change.
- `.env.example`
  - Add `ADMIN_API_KEY`.

### New files to create

- `internal/handler/admin_auth.go`
  - Bearer/API-key middleware for `/api/*`.
- `internal/handler/admin_auth_test.go`
  - Focused middleware tests.
- `internal/service/document_test.go`
  - Service tests for indexing success, retry, and failure compensation.

## Chunk 1: Admin Surface Isolation

### Task 1: Add admin auth config

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `.env.example`

- [ ] **Step 1: Write the failing config tests**

Add tests in `internal/config/config_test.go` for:

```go
func TestLoadRequiresAdminAPIKey() {}
func TestLoadIncludesAdminAPIKey() {}
```

The first test should set `MYSQL_DSN` and `OPENAI_API_KEY`, leave `ADMIN_API_KEY` empty, and expect `Load()` to fail. The second should set `ADMIN_API_KEY=test-admin-key` and verify it is exposed on the returned config.

- [ ] **Step 2: Run the focused config tests and verify failure**

Run: `go test ./internal/config -run 'TestLoadRequiresAdminAPIKey|TestLoadIncludesAdminAPIKey' -v`

Expected: FAIL because `Config` does not contain `AdminAPIKey` and `Load()` does not validate it.

- [ ] **Step 3: Implement admin auth config**

Update `internal/config/config.go`:

```go
type Config struct {
    AppPort     string
    MySQLDSN    string
    AdminAPIKey string
    // ...
}
```

Load it with:

```go
adminAPIKey := strings.TrimSpace(os.Getenv("ADMIN_API_KEY"))
if adminAPIKey == "" {
    return nil, fmt.Errorf("ADMIN_API_KEY is required")
}
```

Set:

```go
cfg := &Config{
    AdminAPIKey: adminAPIKey,
    // ...
}
```

Add `ADMIN_API_KEY=change-me` to `.env.example`.

- [ ] **Step 4: Re-run focused config tests**

Run: `go test ./internal/config -run 'TestLoadRequiresAdminAPIKey|TestLoadIncludesAdminAPIKey' -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go .env.example
git commit -m "feat: require admin api key configuration"
```

### Task 2: Add admin middleware and route isolation

**Files:**
- Create: `internal/handler/admin_auth.go`
- Create: `internal/handler/admin_auth_test.go`
- Modify: `internal/server/router.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Write the failing middleware tests**

Create `internal/handler/admin_auth_test.go` with tests covering:

```go
func TestAdminAuthAllowsBearerToken() {}
func TestAdminAuthAllowsXAPIKey() {}
func TestAdminAuthRejectsMissingCredential() {}
func TestAdminAuthRejectsWrongCredential() {}
```

Use a small gin router and a dummy `/api/ping` handler that returns `200`.

- [ ] **Step 2: Write the failing router-isolation test**

Add a focused test in `internal/handler/admin_auth_test.go` or `internal/server/router_test.go` that verifies:

- `/api/...` returns `401` without admin auth
- `/v1/chat/completions` is not blocked by admin middleware

Stub the chat handler and internal handler methods minimally.

- [ ] **Step 3: Run the focused auth tests and verify failure**

Run: `go test ./internal/handler ./internal/server -run 'AdminAuth|Router' -v`

Expected: FAIL because middleware and router wiring do not exist yet.

- [ ] **Step 4: Implement the middleware**

Create `internal/handler/admin_auth.go`:

```go
func AdminAuthMiddleware(expectedKey string) gin.HandlerFunc
```

Behavior:

- prefer `Authorization: Bearer <key>`
- also accept `X-API-Key: <key>`
- abort with `401` and JSON error when missing or invalid

Keep the error body consistent with the existing error shape.

- [ ] **Step 5: Wire middleware only onto admin routes**

Change `internal/server/router.go` to:

```go
func NewRouter(adminAPIKey string, internalAPI *handler.InternalAPIHandler, openAI *handler.OpenAIHandler) *gin.Engine
```

Apply:

```go
api := router.Group("/api")
api.Use(handler.AdminAuthMiddleware(adminAPIKey))
```

Update `cmd/server/main.go` to pass `cfg.AdminAPIKey`.

- [ ] **Step 6: Re-run the focused auth tests**

Run: `go test ./internal/handler ./internal/server -run 'AdminAuth|Router' -v`

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/handler/admin_auth.go internal/handler/admin_auth_test.go internal/server/router.go cmd/server/main.go
git commit -m "feat: protect admin api routes"
```

### Task 3: Remove unsafe PDF file-path import

**Files:**
- Modify: `internal/appdto/knowledge.go`
- Modify: `internal/handler/internal_api.go`
- Modify: `internal/handler/internal_api_test.go`

- [ ] **Step 1: Write the failing request-contract test**

Add a test to `internal/handler/internal_api_test.go` that sends JSON like:

```json
{"knowledge_base_id":3,"title":"report.pdf","file_path":"/tmp/report.pdf"}
```

Expected behavior:

- request should be rejected with `400`
- service should not receive a `FilePath`

- [ ] **Step 2: Run the focused PDF handler test and verify failure**

Run: `go test ./internal/handler -run 'PDF|ImportPDF' -v`

Expected: FAIL because the current handler/service path still accepts `file_path`.

- [ ] **Step 3: Remove `file_path` from the HTTP DTO and parser**

Update `internal/appdto/knowledge.go`:

```go
type ImportPDFDocumentRequest struct {
    KnowledgeBaseID uint   `json:"knowledge_base_id" form:"knowledge_base_id"`
    Title           string `json:"title" form:"title"`
    FileName        string `json:"file_name" form:"file_name"`
    Content         []byte `json:"content"`
}
```

Update `parseImportPDFRequest` in `internal/handler/internal_api.go` so that:

- multipart remains supported
- JSON requests without binary content are rejected
- no filesystem path is read in handler or service

- [ ] **Step 4: Re-run the focused PDF handler tests**

Run: `go test ./internal/handler -run 'PDF|ImportPDF' -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/appdto/knowledge.go internal/handler/internal_api.go internal/handler/internal_api_test.go
git commit -m "feat: remove pdf file path import from http api"
```

## Chunk 2: Indexing Reliability and Retryability

### Task 4: Add document indexing state-machine tests

**Files:**
- Create: `internal/service/document_test.go`

- [ ] **Step 1: Write the failing service tests for status transitions**

Create tests that model:

```go
func TestIndexDocumentMarksIndexedOnSuccess() {}
func TestIndexDocumentAllowsRetryFromFailed() {}
func TestIndexDocumentRejectsIndexedDocument() {}
func TestIndexDocumentRejectsConcurrentIndexingState() {}
```

Use a test MySQL database pattern already used by the repo where possible; if none exists, start with narrow service tests using test doubles for vector store and embedder seams introduced in the next task.

- [ ] **Step 2: Write the failing service tests for compensation**

Also add:

```go
func TestIndexDocumentMarksFailedWhenQdrantUpsertFails() {}
func TestIndexDocumentDeletesQdrantPointsWhenChunkInsertFails() {}
```

The second test must assert that `DeletePoints` is called with the same generated point IDs that were upserted.

- [ ] **Step 3: Run the focused document service tests and verify failure**

Run: `go test ./internal/service -run 'IndexDocument' -v`

Expected: FAIL because the current code is chunk-count based and does not compensate.

### Task 5: Introduce minimal seams for document indexing dependencies

**Files:**
- Modify: `internal/service/document.go`
- Create or Modify: `internal/service/document_test.go`

- [ ] **Step 1: Add small internal interfaces for testability**

Inside `internal/service/document.go`, introduce narrow interfaces instead of binding directly to concrete types everywhere:

```go
type documentVectorStore interface {
    EnsureCollection(ctx context.Context, collectionName string, dimension int) error
    UpsertChunks(ctx context.Context, collectionName string, chunks []store.ChunkVector) error
    DeletePoints(ctx context.Context, collectionName string, pointIDs []string) error
}

type embeddingProvider interface {
    NewEmbedder(ctx context.Context, modelName string) (embedding.Embedder, error)
}
```

Keep `NewDocumentService(...)` returning the same public service, but store the interfaces internally so tests can inject fakes.

- [ ] **Step 2: Re-run the focused service tests**

Run: `go test ./internal/service -run 'IndexDocument' -v`

Expected: still FAIL, but now tests should compile with fakes.

- [ ] **Step 3: Commit**

```bash
git add internal/service/document.go internal/service/document_test.go
git commit -m "refactor: add test seams for document indexing"
```

### Task 6: Implement state-based indexing claim and terminal updates

**Files:**
- Modify: `internal/service/document.go`
- Modify: `internal/service/document_test.go`

- [ ] **Step 1: Replace chunk-count gate with status gate**

Change `IndexDocument` so that it:

- loads the document
- rejects `indexed`
- rejects `indexing`
- allows `imported` and `failed`

Then atomically claims the document:

```go
result := s.db.WithContext(ctx).
    Model(&entity.Document{}).
    Where("id = ? AND status IN ?", doc.ID, []string{"imported", "failed"}).
    Updates(map[string]any{
        "status": "indexing",
        "error_message": "",
    })
```

If `RowsAffected == 0`, return an application error indicating the document is no longer indexable in its current state.

- [ ] **Step 2: Add helper methods for status updates**

Add focused helpers in `internal/service/document.go`:

```go
func (s *DocumentService) markDocumentFailed(ctx context.Context, documentID uint, message string) error
func (s *DocumentService) markDocumentIndexed(ctx context.Context, tx *gorm.DB, documentID uint) error
```

These helpers keep state writes consistent and reusable across failure branches.

- [ ] **Step 3: Re-run focused service tests**

Run: `go test ./internal/service -run 'IndexDocument' -v`

Expected: some tests still FAIL because compensation and SQL transaction logic are not finished yet.

### Task 7: Implement compensation-safe write ordering

**Files:**
- Modify: `internal/service/document.go`
- Modify: `internal/service/document_test.go`

- [ ] **Step 1: Prepare deterministic point IDs before external writes**

Generate `dbChunks` and `vectorChunks` before upsert, and collect `pointIDs` in a slice so they can be used for compensation.

- [ ] **Step 2: Change the write order**

Implement this order:

1. `EnsureCollection`
2. `UpsertChunks`
3. MySQL transaction:
   - delete stale chunk rows for the document
   - create new chunk rows
   - update document to `indexed`

- [ ] **Step 3: Add compensation on SQL failure**

If the transaction fails after Qdrant upsert succeeds:

```go
cleanupErr := s.vectors.DeletePoints(ctx, kb.CollectionName, pointIDs)
```

Then:

- attempt to mark the document `failed`
- return an error that preserves both the SQL failure and cleanup failure context when both happen

- [ ] **Step 4: Add failure-state handling for pre-upsert errors**

For split, embed, dimension mismatch, and upsert failures:

- set document status to `failed`
- persist a concise `error_message`
- return the original error

- [ ] **Step 5: Re-run focused service tests**

Run: `go test ./internal/service -run 'IndexDocument' -v`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/service/document.go internal/service/document_test.go
git commit -m "fix: make document indexing retryable and compensating"
```

## Chunk 3: Documentation and Final Verification

### Task 8: Update operator documentation

**Files:**
- Modify: `README.md`
- Modify: `.env.example`

- [ ] **Step 1: Document admin auth**

Update `README.md` to state:

- all `/api/*` routes require `ADMIN_API_KEY`
- preferred header is `Authorization: Bearer <ADMIN_API_KEY>`

- [ ] **Step 2: Document PDF import contract**

Update `README.md` to remove any mention of remote `file_path` import and state that PDF import is multipart upload only.

- [ ] **Step 3: Re-read docs diff**

Run: `git diff -- README.md .env.example`

Expected: only admin auth and PDF contract language changes.

- [ ] **Step 4: Commit**

```bash
git add README.md .env.example
git commit -m "docs: document admin auth and pdf import changes"
```

### Task 9: Run full verification

**Files:**
- Modify if needed: any failing tests uncovered by verification

- [ ] **Step 1: Run package-level tests for touched areas**

Run:

```bash
go test ./internal/config ./internal/handler ./internal/server ./internal/service -v
```

Expected: PASS

- [ ] **Step 2: Run full repository test suite**

Run:

```bash
go test ./...
```

Expected: PASS

- [ ] **Step 3: Review final diff**

Run:

```bash
git status --short
git diff --stat HEAD~4..HEAD
```

Expected:

- only intended files changed
- no uncommitted implementation leftovers

- [ ] **Step 4: Final commit if verification fixes were needed**

```bash
git add <touched-files>
git commit -m "test: fix verification regressions"
```

## Local Plan Review Notes

This session cannot rely on automatic subagent plan review unless explicitly authorized. Before execution, manually check:

- the plan keeps public chat API behavior unchanged
- auth is applied only to `/api/*`
- indexing remains synchronous
- every reliability task has a direct regression test

