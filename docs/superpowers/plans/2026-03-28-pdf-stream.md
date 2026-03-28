# PDF Import And Chat Stream Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add PDF upload import and OpenAI-compatible streaming chat completions without changing unrelated behavior.

**Architecture:** Extend the existing internal document import surface with a PDF-specific extraction path that still stores a normal `documents` row and reuses the current synchronous indexing flow. Extend the chat service with a stream-capable path that uses Eino's native model streaming and map emitted chunks into OpenAI-style SSE events in the HTTP handler.

**Tech Stack:** Go, gin, gorm, CloudWeGo Eino, OpenAI-compatible upstream APIs, a Go PDF text extraction library

---

## Chunk 1: PDF Import

### Task 1: Add failing tests for PDF request parsing and service wiring

**Files:**
- Create: `internal/handler/internal_api_test.go`
- Create: `internal/service/document_test.go`

- [ ] Write failing tests for multipart PDF upload parsing and PDF import service behavior
- [ ] Run the focused tests to confirm they fail for the missing PDF path
- [ ] Implement the minimum code needed to make those tests pass
- [ ] Re-run the focused tests

### Task 2: Implement PDF extraction integration

**Files:**
- Create: `internal/ingest/pdf.go`
- Modify: `internal/service/document.go`
- Modify: `internal/handler/internal_api.go`
- Modify: `internal/server/router.go`

- [ ] Add a PDF extractor abstraction and a production implementation using a mature Go PDF text extraction library
- [ ] Add `POST /api/documents/import-pdf` with multipart upload support as the primary path
- [ ] Ensure imported PDF text is persisted as a normal document and indexed through existing chunking/vector code
- [ ] Keep text import behavior unchanged

## Chunk 2: Streaming Chat

### Task 3: Add failing tests for SSE formatting

**Files:**
- Modify: `internal/handler/openai_test.go`
- Modify: `internal/service/chat.go`

- [ ] Add failing handler tests for `stream=true` SSE responses
- [ ] Run the focused tests to confirm they fail before implementation
- [ ] Implement the minimum streaming service/handler changes
- [ ] Re-run the focused tests

### Task 4: Wire Eino stream output into OpenAI-style SSE

**Files:**
- Modify: `internal/appdto/chat.go`
- Modify: `internal/handler/openai.go`
- Modify: `internal/service/chat.go`

- [ ] Add a streaming method that retrieves context, invokes Eino `Stream`, and emits incremental assistant text chunks
- [ ] Format chunk events as `chat.completion.chunk` SSE payloads and terminate with `data: [DONE]`
- [ ] Preserve existing non-streaming behavior and response shape

## Chunk 3: Docs And Verification

### Task 5: Update docs and verify

**Files:**
- Modify: `README.md`
- Modify: `.env.example`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] Document the PDF import endpoint, extraction limitations, and streaming behavior
- [ ] Add or update any dependency metadata required by the PDF library
- [ ] Run focused tests for the changed packages
- [ ] Run `go test ./... -count=1`
