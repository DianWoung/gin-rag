# Eval Replay Structured Messages Design

## Background

The current evaluation replay pipeline persists only a flattened prompt string in `sample_records.prompt`.

At runtime, the RAG prompt is assembled as structured chat messages:

- one `system` message that includes the retrieved context block
- optional history messages
- one trailing `user` message

Before tracing, the message list is flattened into a human-readable string like `role: content` joined by blank lines. During replay, `eval.ReplayChatSample` tries to reconstruct messages by splitting that string on `\n\n` and `: `.

This is not a stable round-trip format. The system prompt itself contains blank lines and the literal text `Retrieved context:`, so replay can misinterpret prompt content as a new message role. In production testing this causes `replay-sample` and `run-trace` to fail with provider-side request validation errors.

## Goal

Make replay deterministic for newly exported traces by persisting the exact structured prompt messages used for generation, while remaining backward-compatible with existing samples that only have a flattened prompt string.

## Non-Goals

- Reworking the online chat prompt construction flow
- Changing the current human-readable `rag.prompt` trace attribute
- Rewriting or migrating existing historical sample rows
- Redesigning the scoring heuristics

## Recommended Approach

Persist both forms of the prompt:

- keep the existing flattened prompt text for readability and debugging
- add a machine-readable JSON payload for the full prompt message list

Replay should prefer the structured JSON messages when present. Only older samples without this field should fall back to the current text parsing logic.

This fixes the root cause for all newly captured traces with a small, localized schema expansion.

## Design

### 1. Trace payload

In `internal/service/chat.go`, when the RAG prompt span is created, emit one additional attribute alongside `rag.prompt`:

- `rag.prompt_messages_json`

The value is a JSON array of objects shaped like:

```json
[
  {"role":"system","content":"..."},
  {"role":"user","content":"..."}
]
```

`rag.prompt` remains unchanged so traces stay readable in Phoenix.

### 2. Exported sample model

Extend `tracebridge.ChatSample` with a structured prompt field:

- `PromptMessages []PromptMessage`

Add a small DTO type in `internal/tracebridge`:

- `PromptMessage`
  - `Role string`
  - `Content string`

`NormalizeChatTrace` should:

- read `rag.prompt_messages_json` when present
- decode it into `PromptMessages`
- continue exporting `Prompt` as today
- fall back cleanly when the JSON attribute is absent, so older traces still export

If the JSON attribute exists but is malformed, normalization should fail. That is a real trace integrity problem for newly captured traces.

### 3. Persistence model

Extend `eval.SampleRecord` with:

- `PromptMessagesJSON string`

Behavior:

- `NewSampleRecord` serializes `ChatSample.PromptMessages` into this column
- `ToStoredSample` deserializes it back into `ChatSample.PromptMessages`
- empty is allowed for backward compatibility

`store.OpenMySQL` already uses `AutoMigrate`, so the new column will be added without a dedicated migration script.

### 4. Replay behavior

Update `eval.ReplayChatSample` so message selection works in this order:

1. If `sample.PromptMessages` is non-empty, convert those items directly into `schema.Message` values and replay with them.
2. Otherwise, fall back to the existing `parsePromptMessages(sample.Prompt)` behavior for legacy samples.

This keeps old data readable and replayable when possible, while making all new captures deterministic.

## Compatibility

### New traces

New traces exported after this change will contain structured prompt messages and should replay correctly.

### Existing traces

Existing sample rows and older Phoenix traces remain readable because:

- `Prompt` is unchanged
- replay still supports the legacy text fallback path

However, legacy samples that contain multi-paragraph system prompts may still fail to replay correctly. That limitation is accepted for historical data.

## Testing

Add or update tests for:

1. `NormalizeChatTrace` exports `PromptMessages` when `rag.prompt_messages_json` is present.
2. `NormalizeChatTrace` still works when only the old flattened prompt attribute exists.
3. `SampleRecord` round-trips `PromptMessagesJSON`.
4. `ReplayChatSample` prefers structured prompt messages over flattened prompt parsing.
5. A replay fixture with a multi-paragraph system prompt succeeds when structured messages are present.

After implementation, run a real smoke test:

1. Start the stack with Phoenix enabled.
2. Create a temporary knowledge base and indexed document.
3. Trigger one non-streaming chat request.
4. Fetch the resulting `trace_id`.
5. Run `go run ./cmd/evalctl run-trace <trace_id>`.
6. Verify replay status is `completed` instead of `error`.

## Risks

- If JSON serialization in the prompt span is truncated by trace body limits, export may fail for new traces.
- Phoenix traces created before this change will still depend on the legacy fallback path.

The first risk is acceptable because prompt truncation is already treated as a trace fidelity issue today, and this change makes the failure mode explicit rather than silently replaying corrupted prompts.
