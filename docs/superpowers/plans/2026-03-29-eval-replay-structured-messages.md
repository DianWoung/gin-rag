# Eval Replay Structured Messages Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist structured prompt messages in trace exports and sample storage so `evalctl replay-sample` and `run-trace` can replay new traces without corrupting multi-paragraph system prompts.

**Architecture:** Keep the existing human-readable flattened prompt text for debugging, but add a machine-readable JSON prompt message payload at the trace, export, and persistence layers. Replay will prefer structured messages when present and fall back to legacy prompt parsing only for old samples.

**Tech Stack:** Go, GORM/MySQL AutoMigrate, Phoenix tracing, Eino schema messages, `go test`

---

## File Map

- Modify: `internal/service/chat.go`
  - Add a JSON trace attribute for structured prompt messages while preserving the existing flattened prompt text.
- Modify: `internal/observability/schema.go`
  - Add a constant for the new trace attribute key.
- Modify: `internal/tracebridge/chat_sample.go`
  - Add a prompt message DTO and expose structured prompt messages on exported samples.
- Modify: `internal/tracebridge/normalize.go`
  - Decode structured prompt messages from traces when available and keep backward-compatible fallback behavior.
- Modify: `internal/tracebridge/normalize_test.go`
  - Cover new structured export and legacy fallback behavior.
- Modify: `internal/eval/models.go`
  - Add `PromptMessagesJSON` to `SampleRecord` and round-trip it through `StoredSample`.
- Add: `internal/eval/models_test.go`
  - Verify sample record serialization/deserialization for structured prompt messages.
- Modify: `internal/eval/replay.go`
  - Prefer structured prompt messages when replaying.
- Modify: `internal/eval/replay_test.go`
  - Add regression coverage for multi-paragraph system prompts and legacy fallback.
- Modify: `README.md`
  - Document that new eval exports persist structured prompt messages for deterministic replay.

## Chunk 1: Structured Prompt Trace Export

### Task 1: Add the new trace attribute key

**Files:**
- Modify: `internal/observability/schema.go`

- [ ] **Step 1: Add the constant for structured prompt messages**

Add a new attribute constant next to the existing prompt-related keys:

```go
AttrPromptMessagesJSON = "rag.prompt_messages_json"
```

- [ ] **Step 2: Commit**

```bash
git add internal/observability/schema.go
git commit -m "chore: add prompt messages trace attribute"
```

### Task 2: Emit structured prompt messages in chat tracing

**Files:**
- Modify: `internal/service/chat.go`
- Test: `internal/tracebridge/normalize_test.go`

- [ ] **Step 1: Write the failing normalize test for structured prompt messages**

Add a test that constructs a prompt span with both:

- `observability.AttrPrompt`
- `observability.AttrPromptMessagesJSON`

And assert `NormalizeChatTrace(...)` exports `PromptMessages` with the correct roles and content.

- [ ] **Step 2: Run the targeted test to verify it fails**

Run:

```bash
go test ./internal/tracebridge -run 'TestNormalizeChatTraceExportsPromptMessages' -v
```

Expected: FAIL because `ChatSample` does not yet expose structured prompt messages.

- [ ] **Step 3: Add minimal implementation**

In `internal/service/chat.go`:

- add a small helper that converts `[]*schema.Message` into a JSON string using lightweight `{role, content}` objects
- when setting prompt span attributes, keep the existing `rag.prompt`
- also set `observability.AttrPromptMessagesJSON`

Do not remove `flattenMessages`.

- [ ] **Step 4: Run the targeted test to verify it passes**

Run:

```bash
go test ./internal/tracebridge -run 'TestNormalizeChatTraceExportsPromptMessages' -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/chat.go internal/tracebridge/normalize_test.go internal/observability/schema.go
git commit -m "feat: trace structured prompt messages"
```

## Chunk 2: Sample Export and Storage Compatibility

### Task 3: Extend exported samples with structured prompt messages

**Files:**
- Modify: `internal/tracebridge/chat_sample.go`
- Modify: `internal/tracebridge/normalize.go`
- Modify: `internal/tracebridge/normalize_test.go`

- [ ] **Step 1: Write the failing tests for normalize fallback behavior**

Add or extend tests for both cases:

1. prompt JSON present -> `PromptMessages` is populated
2. prompt JSON absent -> export still succeeds and `PromptMessages` stays empty

- [ ] **Step 2: Run the targeted tests to verify failure**

Run:

```bash
go test ./internal/tracebridge -run 'TestNormalizeChatTrace(ExportsPromptMessages|BuildsPromptAndChunks)' -v
```

Expected: FAIL until the sample model and normalize logic are updated.

- [ ] **Step 3: Write minimal implementation**

In `internal/tracebridge/chat_sample.go`:

- add `PromptMessage`
- add `PromptMessages []PromptMessage` to `ChatSample`

In `internal/tracebridge/normalize.go`:

- decode `observability.AttrPromptMessagesJSON` when present
- fail if present-but-invalid
- keep legacy export behavior when absent

- [ ] **Step 4: Re-run the tracebridge tests**

Run:

```bash
go test ./internal/tracebridge -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tracebridge/chat_sample.go internal/tracebridge/normalize.go internal/tracebridge/normalize_test.go
git commit -m "feat: export structured prompt messages in eval samples"
```

### Task 4: Persist structured prompt messages in sample records

**Files:**
- Modify: `internal/eval/models.go`
- Add: `internal/eval/models_test.go`

- [ ] **Step 1: Write the failing model round-trip test**

Add a test that:

- builds a `tracebridge.ChatSample` with `PromptMessages`
- passes it through `NewSampleRecord`
- then through `ToStoredSample`
- asserts the structured messages survive intact

- [ ] **Step 2: Run the targeted test to verify it fails**

Run:

```bash
go test ./internal/eval -run 'TestSampleRecordRoundTripsPromptMessages' -v
```

Expected: FAIL because `SampleRecord` does not yet persist the field.

- [ ] **Step 3: Write minimal implementation**

In `internal/eval/models.go`:

- add `PromptMessagesJSON string` to `SampleRecord`
- serialize it in `NewSampleRecord`
- deserialize it in `ToStoredSample`
- keep empty-string compatibility

- [ ] **Step 4: Run the targeted test to verify it passes**

Run:

```bash
go test ./internal/eval -run 'TestSampleRecordRoundTripsPromptMessages' -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/eval/models.go internal/eval/models_test.go
git commit -m "feat: persist structured prompt messages in eval samples"
```

## Chunk 3: Replay Fix and End-to-End Verification

### Task 5: Prefer structured messages during replay

**Files:**
- Modify: `internal/eval/replay.go`
- Modify: `internal/eval/replay_test.go`

- [ ] **Step 1: Write the failing replay regression test**

Add a test where:

- `Prompt` contains a multi-paragraph system prompt that would be split incorrectly by `parsePromptMessages`
- `PromptMessages` contains the correct structured message list
- a fake model records the received messages

Assert replay uses the structured messages and returns `completed`.

Also keep one small legacy fallback test using only `Prompt`.

- [ ] **Step 2: Run the targeted replay tests to verify failure**

Run:

```bash
go test ./internal/eval -run 'TestReplayChatSample' -v
```

Expected: FAIL until replay prefers `PromptMessages`.

- [ ] **Step 3: Write minimal implementation**

In `internal/eval/replay.go`:

- add a helper that converts `[]tracebridge.PromptMessage` to `[]*schema.Message`
- in `ReplayChatSample`, use that helper when structured messages are present
- fall back to `parsePromptMessages(sample.Prompt)` for legacy rows

- [ ] **Step 4: Re-run replay tests**

Run:

```bash
go test ./internal/eval -run 'TestReplayChatSample' -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/eval/replay.go internal/eval/replay_test.go
git commit -m "fix: replay eval samples from structured prompt messages"
```

### Task 6: Document and verify the full chain

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update eval documentation**

Add one short note in the Phoenix/export/replay section that newly exported samples persist structured prompt messages for deterministic replay.

- [ ] **Step 2: Run focused package tests**

Run:

```bash
go test ./internal/tracebridge ./internal/eval -v
```

Expected: PASS

- [ ] **Step 3: Run the full test suite**

Run:

```bash
go test ./...
```

Expected: PASS

- [ ] **Step 4: Run the real Phoenix smoke test**

Run the stack with Phoenix enabled, then:

```bash
curl -sS -H 'Authorization: Bearer change-me' -H 'Content-Type: application/json' -X POST http://localhost:8080/api/knowledge-bases -d '{"name":"eval-chain-fix","description":"evaluation replay fix test"}'
curl -sS -H 'Authorization: Bearer change-me' -H 'Content-Type: application/json' -X POST http://localhost:8080/api/documents/import-text -d '{"knowledge_base_id":<KB_ID>,"title":"eval.txt","content":"Evaluation chain test document. The admin API is protected by ADMIN_API_KEY. Metadata is stored in MySQL and vectors are stored in Qdrant."}'
curl -sS -H 'Authorization: Bearer change-me' -X POST http://localhost:8080/api/documents/<DOC_ID>/index
curl -sS -H 'Content-Type: application/json' http://localhost:8080/v1/chat/completions -d '{"model":"deepseek-chat","knowledge_base_id":<KB_ID>,"temperature":0.2,"stream":false,"messages":[{"role":"user","content":"ADMIN_API_KEY 是做什么的？元数据和向量分别存在哪里？"}]}'
curl -sS 'http://localhost:6006/v1/projects/go-rag/spans?limit=200'
MYSQL_DSN='rag:rag@tcp(127.0.0.1:3306)/go_rag?charset=utf8mb4&parseTime=True&loc=Local' PHOENIX_BASE_URL='http://127.0.0.1:6006' PHOENIX_PROJECT_NAME='go-rag' OPENAI_API_KEY='<key>' OPENAI_BASE_URL='https://api.deepseek.com/v1' OPENAI_CHAT_MODEL='deepseek-chat' go run ./cmd/evalctl run-trace <TRACE_ID>
```

Expected:

- `run-trace` exits `0`
- `replay.status` is `completed`
- `replay.answer` is non-empty

- [ ] **Step 5: Clean up temporary smoke-test data**

Delete the temporary knowledge base created for the smoke test.

- [ ] **Step 6: Commit**

```bash
git add README.md
git commit -m "docs: note deterministic replay sample export"
```
