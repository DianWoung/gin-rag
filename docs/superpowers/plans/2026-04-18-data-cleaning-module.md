# Data Cleaning Module Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a conservative ingestion-time text cleaner that removes high-confidence noise before block building and chunking.

**Architecture:** Implement a pure cleaner in `internal/ingest`, return a small `CleanReport`, and apply it once when documents are created so stored content and later indexing share the same canonical cleaned body. Keep the rules deterministic and conservative to avoid deleting likely-valid content.

**Tech Stack:** Go, `testing`, Gin/Gorm existing service layer, current `internal/ingest` pipeline.

---

## Chunk 1: Cleaner Core

### Task 1: Add failing cleaner tests

**Files:**
- Create: `internal/ingest/cleaner_test.go`
- Reference: `internal/ingest/pdf.go`
- Reference: `docs/superpowers/specs/2026-04-18-data-cleaning-module-design.md`

- [ ] **Step 1: Write table-driven tests for conservative cleaning**

Cover:
- newline normalization / BOM / NUL removal
- blank-line collapse
- consecutive duplicate short-line collapse
- high-confidence page-number removal
- repeated header/footer removal
- negative cases where legitimate short content must remain

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ingest -run 'TestCleaner' -v`
Expected: FAIL because cleaner does not exist yet.

### Task 2: Implement minimal cleaner

**Files:**
- Create: `internal/ingest/cleaner.go`
- Test: `internal/ingest/cleaner_test.go`

- [ ] **Step 1: Add `Cleaner`, `CleanReport`, and `Clean`**

Implement:
- BOM / NUL removal
- newline normalization
- trailing-space trimming
- blank-line collapse
- consecutive duplicate short-line collapse
- standalone page-number removal
- repeated header/footer removal with conservative threshold

- [ ] **Step 2: Run targeted cleaner tests**

Run: `go test ./internal/ingest -run 'TestCleaner' -v`
Expected: PASS

## Chunk 2: Service Integration

### Task 3: Add failing document-service integration test

**Files:**
- Modify: `internal/service/document_test.go`
- Reference: `internal/service/document.go`

- [ ] **Step 1: Add a test proving imported noisy content is cleaned before indexing**

Test should assert:
- stored `documents.content` is cleaned
- indexed chunk content reflects cleaned text

- [ ] **Step 2: Run targeted test to verify it fails**

Run: `go test ./internal/service -run 'Test(Import|Index)Document.*Clean' -v`
Expected: FAIL because service does not yet call cleaner.

### Task 4: Wire cleaner into document creation

**Files:**
- Modify: `internal/service/document.go`

- [ ] **Step 1: Inject cleaner into `DocumentService` and use it in document creation**

Rules:
- clean content once before persisting document
- reuse cleaned content for both text and PDF imports
- keep failures impossible in v1 (best-effort cleaning)

- [ ] **Step 2: Run targeted service tests**

Run: `go test ./internal/service -run 'Test(Import|Index)Document.*Clean|TestIndexDocument' -v`
Expected: PASS

## Chunk 3: Observability and Verification

### Task 5: Add lightweight cleaning observability

**Files:**
- Modify: `internal/service/document.go`
- Optional modify: `internal/observability/schema.go`

- [ ] **Step 1: Record whether cleaning changed content and summary counters**

Keep this compact:
- no full duplicated cleaned body
- counters only

- [ ] **Step 2: Verify no existing tests regress**

Run: `go test ./internal/service ./internal/ingest ./internal/observability -v`
Expected: PASS

### Task 6: Document and run full verification

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update docs for data cleaning behavior and boundaries**

Document:
- what is cleaned
- what is intentionally not cleaned
- where it runs in the ingestion path

- [ ] **Step 2: Run full verification**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/ingest/cleaner.go internal/ingest/cleaner_test.go internal/service/document.go internal/service/document_test.go README.md docs/superpowers/plans/2026-04-18-data-cleaning-module.md
git commit -m "feat: add conservative data cleaning during ingestion"
```
