# Reliability and Admin Security Design

## Goal

Improve the current Go RAG service in two specific areas without changing the deployment model or introducing async infrastructure:

- make document indexing recoverable and retryable when MySQL and Qdrant operations partially fail
- isolate `/api/*` as an authenticated admin surface while keeping `/v1/chat/completions` as the inference surface

This design intentionally keeps the current single-process, synchronous indexing model.

## Scope

This design covers:

- document indexing state transitions and failure handling
- compensating actions between MySQL metadata writes and Qdrant vector writes
- admin API authentication and routing boundaries
- removal of unsafe remote file-path PDF import over HTTP
- targeted tests for failure and auth paths

This design does not cover:

- async jobs, queues, or background workers
- multi-tenant authorization
- rate limiting or WAF concerns
- splitting the service into multiple binaries
- major refactors of the current handler/service/store layering

## Recommended Approach

Use a minimal-intrusion hardening pass:

- keep the current `handler -> service -> store` structure
- keep synchronous indexing triggered by `POST /api/documents/:id/index`
- add an explicit document indexing state machine
- make failure paths deterministic with compensating cleanup
- protect `/api/*` with a shared admin credential
- remove `file_path` from the HTTP contract for PDF import

This keeps rollout risk low while fixing the most serious correctness and exposure issues in the current code.

## Existing Problems

### Partial Index Writes

The current indexing flow writes `document_chunks` to MySQL before Qdrant upsert. If Qdrant fails afterward:

- the document remains effectively unindexed in Qdrant
- MySQL already contains chunk rows
- subsequent retries are blocked because the service interprets existing chunk rows as "already indexed"

This is the highest-priority reliability issue.

### Admin Surface Is Not Isolated

The current router exposes:

- knowledge-base lifecycle operations
- document import
- document indexing
- document deletion

without authentication. These routes are operationally privileged and should not share the same trust boundary as the chat inference API.

### Remote File Read via HTTP

`ImportPDFDocumentRequest` currently allows `file_path`, and the service reads that path from the server filesystem. Over HTTP this is an unsafe capability because a caller can attempt to import any readable PDF file on the host.

## Architecture

The service remains a single binary:

`gin router -> auth middleware (admin only) -> handler -> service -> mysql/qdrant/llm`

Two logical API surfaces are introduced:

- inference surface: `/v1/chat/completions`
- admin surface: `/api/*`

The separation is enforced by routing and middleware, not by process split.

## Document State Model

The `documents.status` field becomes a real state machine with the following values:

- `imported`: content exists, vectors/chunks not yet committed
- `indexing`: an indexing attempt is in progress
- `indexed`: chunks are committed and vectors are present in Qdrant
- `failed`: the last indexing attempt failed and the document is eligible for retry

### Valid Transitions

- `imported -> indexing`
- `failed -> indexing`
- `indexing -> indexed`
- `indexing -> failed`

The service must reject indexing requests when status is already `indexed`.

For `indexing`, the service should treat it as non-retryable from another concurrent request and return a conflict-style application error.

## Indexing Flow

### High-Level Flow

The indexing endpoint keeps the same synchronous request model but changes the order of operations:

1. Load document and knowledge base
2. Atomically move the document status from `imported` or `failed` to `indexing`
3. Split document content
4. Generate embeddings
5. Ensure Qdrant collection exists
6. Prepare deterministic point IDs and chunk rows
7. Upsert vectors into Qdrant
8. In a MySQL transaction:
   - delete any stale chunk rows for this document
   - insert the new `document_chunks`
   - update document status to `indexed`
   - clear `error_message`
9. If step 8 fails, delete the just-written Qdrant points and mark the document `failed`
10. If any step before step 8 fails, mark the document `failed`

### Why Qdrant First

For this repository, Qdrant is the harder system to roll back implicitly because it lives outside the SQL transaction boundary. Writing Qdrant first and then compensating on SQL failure gives a reliable recovery path:

- on Qdrant failure, no SQL chunk rows are committed
- on SQL failure after Qdrant success, the service knows exactly which points to delete

This is still not a distributed transaction, but it is deterministic and operationally recoverable.

### Idempotency and Retry

Retries are allowed only from `failed` and `imported`.

To support safe retries:

- the service must not treat existing chunk rows alone as proof of success
- stale chunk rows for a failed prior attempt must be removed before re-inserting
- the final source of truth for success is `documents.status == indexed`

### Error Recording

When indexing fails, the service should:

- set `documents.status = failed`
- persist a concise `error_message`
- avoid leaving stale chunk rows when the failure happens after partial SQL work

The error message is operator-facing and should be short enough to inspect in list/detail responses.

## Concurrency Rules

This design assumes multiple requests may target the same document.

Required behavior:

- only one request may move a document into `indexing`
- a second concurrent request should fail cleanly once the first has claimed the state

Implementation guidance:

- use an atomic `UPDATE ... WHERE id = ? AND status IN ('imported','failed')`
- verify rows affected before proceeding

This is sufficient for the current single-process deployment and remains safe under multiple app replicas.

## Admin API Security

### Contract

All `/api/*` routes require an admin credential.

Initial mechanism:

- `Authorization: Bearer <ADMIN_API_KEY>`

Optional compatibility support:

- `X-API-Key: <ADMIN_API_KEY>`

The middleware should accept one form or both, but the codebase should document a single preferred mechanism.

### Configuration

Add:

- `ADMIN_API_KEY`

Startup behavior:

- if any admin routes are registered and `ADMIN_API_KEY` is empty, startup fails

This is intentional. Silent insecure startup is not acceptable for privileged routes.

### Scope

The new admin middleware applies only to:

- `/api/knowledge-bases`
- `/api/documents/*`

It must not apply to:

- `/healthz`
- `/v1/chat/completions`

## PDF Import Contract Change

### Change

Remove `file_path` from the HTTP request DTO for PDF import.

Accepted admin-side import modes become:

- multipart upload with `file`
- existing text JSON import for plain text documents

### Rationale

Server-side local path reads over HTTP create unnecessary filesystem exposure. If local file import is still needed for operators, it should live in:

- a CLI
- a private maintenance command
- or a future internal-only job runner

It should not remain in a remotely callable HTTP handler.

## Module Changes

### `internal/config`

Add admin auth config:

- `AdminAPIKey string`

`Load()` should validate it when admin routes are enabled.

### `internal/server`

Introduce an admin group:

- `api := router.Group("/api")`
- `api.Use(authMiddleware)`

The inference route remains outside this group.

### `internal/handler`

Changes:

- add admin auth middleware wiring
- stop binding or honoring `file_path` in PDF import requests
- return auth failures as `401` or `403` consistently

### `internal/service/document`

Changes:

- formalize status transitions
- stop using `chunkCount > 0` as the sole guard for "already indexed"
- add compensating cleanup on Qdrant/MySQL split failures
- allow retry when status is `failed`

### `internal/store`

No major redesign is needed, but the existing `DeletePoints` path becomes part of the indexing compensation flow and must be treated as required cleanup rather than optional utility.

## Failure Handling Rules

### Failure Before Qdrant Upsert

Examples:

- split failure
- embedding failure
- vector dimension mismatch

Result:

- mark document `failed`
- do not create chunk rows
- do not leave new Qdrant vectors

### Failure After Qdrant Upsert But Before SQL Commit

Examples:

- chunk insert failure
- document status update failure

Result:

- delete the newly written Qdrant points
- roll back SQL transaction
- mark document `failed`

If compensation deletion also fails, return the original error plus cleanup context and keep the document in `failed` for operator visibility.

### Failure While Marking `failed`

If the service cannot update the failure state, that error should be surfaced prominently because it prevents safe retry semantics. This is a secondary but still important operational failure.

## Testing Strategy

This design requires tests in the layers that currently have the weakest coverage.

### Unit or Narrow Integration Tests

Add tests for:

- `imported -> indexing -> indexed`
- `failed -> indexing -> indexed`
- concurrent claim attempt on the same document
- Qdrant upsert failure leads to `failed` and no chunk rows
- SQL chunk insert failure after successful Qdrant upsert triggers `DeletePoints`
- `indexed` document cannot be indexed again
- `/api/*` without admin credential is rejected
- `/v1/chat/completions` remains accessible without admin credential
- PDF import no longer accepts `file_path`

### Regression Focus

The most important regressions to prevent are:

- documents getting stuck permanently after a failed index attempt
- admin routes accidentally left unauthenticated
- chat traffic being blocked by admin auth

## Rollout Plan

1. Add config and admin middleware
2. Remove `file_path` from request DTO and handler path
3. Refactor indexing state transitions and compensation logic
4. Add tests for failure and auth paths
5. Update README and `.env.example`

This order reduces the chance of shipping the indexing changes without the required operational safeguards.

## Risks

- The indexing flow remains synchronous, so long-running requests are still possible.
- Compensation logic improves consistency but is not equivalent to a distributed transaction.
- Existing documents with stale chunk rows from older failures may need one-time cleanup or forced transition to `failed`.

These risks are acceptable for this milestone because the goal is hardening, not architectural expansion.

## Success Criteria

This design is successful when:

- failed indexing attempts leave documents retryable
- Qdrant/MySQL partial failures no longer strand documents in an inconsistent terminal state
- all `/api/*` routes require admin auth
- `file_path` can no longer be used to read server-local PDFs over HTTP
- the change lands without altering the public inference API
