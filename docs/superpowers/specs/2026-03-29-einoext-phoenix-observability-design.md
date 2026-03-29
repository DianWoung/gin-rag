# EinoExt + Phoenix Observability and Evaluation Design

## Goal

Build a stable quality loop for this Go RAG service using:

- Phoenix for production tracing and span-level troubleshooting
- EinoExt for offline evaluation of RAG quality
- a thin internal bridge that turns production traces into reproducible evaluation samples

The first milestone is a complete, working loop:

1. trace real requests in production
2. identify bad runs in Phoenix
3. export those runs into normalized evaluation samples
4. replay and score them offline with EinoExt

## Scope

This design covers:

- full request tracing for `chat`, `ingest`, `internal API`, and store access
- trace payloads that aim to keep full request and response content, lossless under configured limits and visibly truncated otherwise
- a normalized sample model for chat replay and chat evaluation
- a half-automatic issue loop from Phoenix trace to EinoExt score for chat requests

This design does not cover:

- CI gating
- multi-tenant privacy controls
- automated feedback into prompt or retrieval tuning
- dashboard product work beyond what Phoenix already provides
- ingest replay or ingest quality scoring

## Recommended Approach

Use a mixed architecture:

- Phoenix owns online observability
- EinoExt owns offline quality scoring
- project-local glue code owns trace normalization and replay

This keeps responsibilities clear:

- Phoenix is optimized for runtime inspection and span-level debugging
- EinoExt remains the programmable evaluation layer for business-specific scoring
- the service code stays close to its current layered structure

For milestone one, offline evaluation is deterministic by design:

- traces capture immutable chat artifacts
- export persists those artifacts into a normalized sample
- evaluation uses the persisted sample only
- evaluation does not rerun live retrieval against mutable knowledge-base state

This keeps score interpretation stable even if the knowledge base changes after the original request.

## Existing Context

The repository already uses:

- `github.com/cloudwego/eino`
- `github.com/cloudwego/eino-ext` OpenAI chat and embedding components
- layered service boundaries in `internal/service`
- explicit RAG stages in `internal/service/chat.go`
- synchronous ingest flow in `internal/service/document.go`

Those seams are sufficient for the first implementation. No large refactor is required before observability work starts.

## Architecture

Request flow after this design:

`gin handler -> tracing middleware -> service spans -> store/LLM spans -> Phoenix`

Quality loop after this design:

`Phoenix trace -> normalized chat sample -> offline chat replay -> EinoExt evaluation -> persisted evaluation result`

### Units

#### `internal/observability`

Responsibilities:

- initialize OpenTelemetry tracer/exporter for Phoenix OTLP
- provide helpers for starting spans and attaching consistent attributes
- expose middleware and service helpers without embedding Phoenix-specific logic into business code
- own the live trace schema used by handlers, services, and stores

Must not:

- decide business evaluation rules
- know sample scoring semantics

#### `internal/tracebridge`

Responsibilities:

- interpret the live trace schema emitted by `internal/observability`
- extract trace-backed replay input into normalized samples

Required interface for milestone one:

- `NormalizeChatTrace(trace TraceEnvelope) (ChatSample, []ExportWarning, error)`

Must not:

- start transport clients
- call EinoExt evaluators directly
- be imported by live request handlers or services

#### `internal/traceexport`

Responsibilities:

- own the operator-facing export workflow from Phoenix trace to normalized chat sample
- accept a trace ID as input
- fetch the trace from Phoenix using a dedicated client
- invoke `internal/tracebridge` to normalize trace data
- write the normalized sample through the evaluation repository boundary

Required interface for milestone one:

- `ExportChatTrace(traceID string) (ChatSample, []ExportWarning, error)`

Must not:

- score samples
- own span emission during live requests

#### `internal/phoenix`

Responsibilities:

- provide the dedicated client used by `internal/traceexport`
- isolate Phoenix-specific trace retrieval logic from the rest of the application

Required interface for milestone one:

- `FetchTrace(traceID string) (TraceEnvelope, error)`

`TraceEnvelope` must contain, at minimum:

- trace ID
- root span metadata
- child spans
- span attributes
- span events with timestamps and event attributes
- request timestamp range

Must not:

- emit live request spans
- normalize traces into samples

#### `internal/eval`

Responsibilities:

- define the normalized evaluation sample model
- replay a sample using persisted chat artifacts
- run EinoExt evaluators
- persist evaluation outputs

Required interface for milestone one:

- `ReplayChatSample(sample ChatSample) (ReplayRun, error)`
- `ScoreChatSample(sample ChatSample, replay *ReplayRun) ([]EvaluationResult, error)`

Must not:

- own production tracing configuration
- inspect HTTP or database internals directly

Dependency rule for milestone one:

- business code imports only `internal/observability`
- offline export code imports `internal/phoenix`, `internal/tracebridge`, and `internal/eval`
- `internal/tracebridge` consumes the schema emitted by `internal/observability`

## Trace Coverage

Initial rollout tracing covers the full service:

- chat request flow
- document import and indexing flow
- knowledge-base operations
- internal API handlers
- MySQL and Qdrant access

This is broad on purpose. The user explicitly prioritized end-to-end troubleshooting over minimal initial coverage.

Initial rollout evaluation covers chat requests only.

Ingest, internal API, and store traces are observability-only in the first milestone. They are not converted into replay samples or scored by EinoExt in the first implementation plan.

## Span Design

Use one trace per inbound request with child spans for major business steps.

### Root spans

- `http.v1.chat_completions`
- `http.api.create_knowledge_base`
- `http.api.list_knowledge_bases`
- `http.api.delete_knowledge_base`
- `http.api.import_text_document`
- `http.api.import_pdf_document`
- `http.api.index_document`
- `http.api.list_documents`
- `http.api.delete_document`

### Chat child spans

- `chat.prepare_request`
- `chat.rewrite_query`
- `embed.query`
- `qdrant.search`
- `rerank.rank`
- `chat.build_sources`
- `chat.build_prompt`
- `llm.generate`
- `llm.stream`
- `chat.build_response`

### Ingest child spans

- `document.import_text`
- `document.read_pdf_input`
- `document.extract_pdf_text`
- `document.split_chunks`
- `embed.document_chunks`
- `qdrant.ensure_collection`
- `mysql.insert_chunks`
- `qdrant.upsert_chunks`
- `mysql.update_document_status`

### Store and metadata spans

- `mysql.query`
- `mysql.insert`
- `mysql.update`
- `qdrant.delete_points`
- `qdrant.delete_collection`

## Trace Payload Policy

The user chose to record full content.

That means traces will include:

- full user question
- full chat history
- rewritten retrieval query
- retrieved chunk content
- full constructed prompt
- full model answer
- full extracted document text during ingest

To keep traces usable, the implementation should still distinguish between:

- attributes for short scalar metadata
- span events for long content bodies

Rules:

- short metadata such as IDs, counts, model names, scores, and durations go into attributes
- long text such as prompts, answers, and chunk bodies go into named span events
- each long event should still record length metadata for quick filtering

For milestone one, "full content" means:

- lossless capture when event bodies remain under configured exporter limits
- visible truncation metadata when limits are exceeded
- no silent truncation

This avoids turning every span into a large flat attribute map while preserving complete debugging context.

## Trace Attribute Schema

Common attributes:

- `rag.request_id`
- `rag.trace_role`
- `rag.knowledge_base_id`
- `rag.document_id`
- `rag.collection_name`

Chat attributes:

- `rag.model`
- `rag.temperature`
- `rag.original_query_preview`
- `rag.retrieval_query_preview`
- `rag.match_count`
- `rag.top_k`
- `rag.reranked`
- `rag.doc_ids`
- `rag.scores`
- `rag.prompt_chars`
- `rag.answer_chars`
- `rag.finish_reason`

Ingest attributes:

- `rag.file_name`
- `rag.source_type`
- `rag.chunk_count`
- `rag.vector_dimension`
- `rag.document_status`

Error attributes:

- `error.type`
- `error.message`

## Trace Event Schema

Named events should be consistent across requests:

- `input.question`
- `input.history`
- `input.retrieved_chunk`
- `input.prompt_part`
- `output.answer_part`
- `document.raw_text`
- `document.chunk_text`

Each event should carry:

- the full text body
- a length field
- a stable sample correlation field when available

Preview attribute rule:

- query-like attributes store a short preview only
- full query, prompt, answer, and chunk bodies live in events
- normalization must prefer event bodies over preview attributes whenever both exist

Event cardinality rules:

- `input.question`: exactly one event per chat request
- `input.history`: zero or one event per chat request
- `input.retrieved_chunk`: one event per retrieved chunk, with `chunk_index`, `document_id`, and retrieval order
- `input.prompt_part`: one or more events per prompt build, with `part_index` and `part_total`
- `output.answer_part`: one or more events per final answer, with `part_index` and `part_total`
- `document.raw_text`: zero or one event per ingest request
- `document.chunk_text`: one event per stored chunk, with `chunk_index`

Normalization rules:

- retrieved chunks are reconstructed by sorting `input.retrieved_chunk` events by `chunk_index`
- prompts are reconstructed by sorting `input.prompt_part` events by `part_index`
- answers are reconstructed by sorting `output.answer_part` events by `part_index`
- document chunks are reconstructed by sorting `document.chunk_text` events by `chunk_index`
- repeated events with the same event name and the same `chunk_index` or `part_index` are invalid and should fail sample export

Payload limit policy:

- live request processing must fail open when telemetry payloads are too large
- long bodies are emitted in chunked events instead of one serialized array event
- if a single event body exceeds the configured exporter cap, the body is truncated and marked with `rag.telemetry_truncated=true`
- truncation must never block request success, but it must be visible in traces and exported samples
- a sample with truncated prompt parts is not replayable and export must fail
- a sample with truncated answer parts is capturable but answer-based scoring must be marked limited
- a sample with truncated retrieved chunk content is capturable, but `retrieval_relevance` and `citation_correctness` must be marked limited or skipped
- incomplete streamed answers are capturable with warning if prompt parts are complete
- incomplete streamed answers must skip `grounded_answer` and `citation_correctness`

## Evaluation Sample Model

Define a normalized chat sample shape that is independent from Phoenix internals:

- `sample_id`
- `source_trace_id`
- `created_at`
- `created_from`
- `scenario`
- `knowledge_base_id`
- `document_ids`
- `question`
- `history`
- `retrieval_query`
- `retrieved_chunks`
- `prompt`
- `replay_model`
- `replay_temperature`
- `replay_provider`
- `actual_answer`
- `citations`
- `expected_answer`
- `reference_facts`
- `labels`
- `capture_status`
- `export_warnings`
- `telemetry_truncated`

`created_from` is one of:

- `production_failure`
- `low_quality_trace`
- `manual_case`

`expected_answer` is optional. Some samples will rely on reference facts or groundedness checks rather than exact-match answers.

`capture_status` is one of:

- `complete`
- `warning`
- `failed`

`retrieved_chunks` is an ordered list of objects with:

- `chunk_id`
- `chunk_index`
- `document_id`
- `document_title`
- `content`
- `score`

`citations` is an ordered list of objects with:

- `citation_label`
- `chunk_id`
- `chunk_index`

`export_warnings` stores non-fatal export issues such as:

- truncated prompt parts
- truncated answer parts
- missing optional events
- missing citation metadata

`telemetry_truncated` is true when any long-body event was truncated during export.

## Export Eligibility Matrix

For chat trace export in milestone one:

- required and fatal if missing:
  - one `http.v1.chat_completions` root span
  - one `input.question` event
  - one complete prompt reconstructed from `input.prompt_part`
- optional with warning:
  - `input.history`
  - `actual_answer`
  - citation metadata
- fatal:
  - multiple candidate chat root spans
  - duplicate prompt parts
  - duplicate answer parts
  - duplicate retrieved chunk indexes
  - traces that are not chat requests
- warning and metric skip:
  - citation labels that do not resolve to a retrieved chunk
  - missing expected answer
  - missing reference facts

If export succeeds with warnings, the sample remains usable and `export_warnings` records the reason.

## Evaluation Metrics

The first evaluator set is intentionally small and aligned to current RAG behavior:

- `retrieval_relevance`
- `grounded_answer`
- `citation_correctness`
- `abstention_quality`

Definitions:

- `retrieval_relevance`: do retrieved chunks materially support the question
- `grounded_answer`: is the answer supported by retrieved context
- `citation_correctness`: do emitted citations point to relevant supporting chunks
- `abstention_quality`: when support is insufficient, does the system refuse cleanly instead of hallucinating

Metric contract for milestone one:

- each metric returns `score` as `float64` in the range `[0, 1]`
- each metric returns `status` as one of `scored`, `skipped`, or `error`
- skipped metrics must include a short `summary`
- errored metrics must include an `error_message`

Metric inputs and skip behavior:

- `retrieval_relevance`
  - inputs: `question`, `retrieved_chunks`
  - skip when retrieved chunk content is truncated or absent
- `grounded_answer`
  - inputs: `actual_answer` or replayed answer, `retrieved_chunks`
  - skip when no answer text exists
- `citation_correctness`
  - inputs: `citations`, `retrieved_chunks`, answer text
  - skip when no citations are emitted or citation labels cannot be resolved
- `abstention_quality`
  - inputs: answer text, `retrieved_chunks`, optional `reference_facts`
  - skip only when no answer text exists

## Replay Flow

Replay runs should use the normalized chat sample instead of raw Phoenix payloads.

Replay mode for milestone one:

- replay is generation-only
- replay always uses the persisted `prompt` as the single source of truth
- replay reuses the captured `replay_model`, `replay_temperature`, and `replay_provider`
- if the captured model is unavailable later, replay run status is `error` and no fallback model is chosen automatically
- replay does not call Qdrant search, reranker, or live document lookup
- retrieval metrics score the captured retrieval artifacts from the original run
- answer metrics score both the original answer and the replayed answer when a replay run exists

Replay sequence:

1. load a saved sample
2. read the persisted prompt from the sample
3. rerun generation from that persisted prompt only
4. save the replay artifacts
5. score the captured sample and replay output with EinoExt evaluators
6. save the evaluation result

This makes evaluation resilient to future changes in Phoenix trace representation.

## Persistence

Evaluation persistence can start small.

Required persisted records:

- raw normalized sample
- replay output
- evaluator scores
- evaluator summary or failure reason

For the first implementation, persistence uses MySQL because the service already depends on it and the data is relational.

Required repository boundary:

- `SaveSample(sample)`
- `GetSample(sampleID)`
- `ListSamples(filter)`
- `SaveReplayRun(run)`
- `ListReplayRuns(sampleID)`
- `SaveEvaluationResult(result)`
- `ListEvaluationResults(sampleID)`

Repository ownership for milestone one:

- the MySQL-backed repository lives under `internal/eval`
- `internal/traceexport` depends on that repository through an interface, not through raw SQL

Lifecycle rules:

- exporting the same `trace_id` twice is idempotent by default and returns the existing sample
- replay runs are append-only; a sample may have multiple replay runs over time
- evaluation results are append-only and tied to either the captured sample or a specific replay run
- rescoring never mutates prior stored results; it creates new result rows

Required sample record fields:

- `sample_id`
- `source_trace_id`
- `request_kind`
- `created_from`
- `status`
- `payload_json`
- `warnings_json`
- `telemetry_truncated`
- `created_at`
- `updated_at`

Required evaluation record fields:

- `result_id`
- `sample_id`
- `metric_name`
- `score`
- `summary`
- `status`
- `error_message`
- `created_at`

Required replay-run record fields:

- `replay_run_id`
- `sample_id`
- `status`
- `model`
- `prompt_json`
- `answer_text`
- `citations_json`
- `trace_reference`
- `error_message`
- `created_at`

Required evaluation result fields must also include:

- `replay_run_id`
- `status`
- `score`
- `summary`
- `error_message`

`ExportWarning` contains:

- `code`
- `message`
- `span_name`
- `event_name`
- `severity`

## Operator Entrypoints

Milestone one uses a thin CLI owned by the repository:

- `cmd/evalctl export-trace <trace_id>`
- `cmd/evalctl replay-sample <sample_id>`
- `cmd/evalctl score-sample <sample_id>`

These commands orchestrate the libraries in `internal/traceexport` and `internal/eval`. No admin HTTP endpoint is part of milestone one.

This is enough to support export, replay, scoring, and operator lookup without defining the final query UI up front.

## Operational Flow

First-release workflow is half-automatic:

1. inspect a bad trace in Phoenix
2. copy its trace ID into a CLI export command
3. convert it into a normalized sample
4. run offline replay and evaluation
5. review the score output
6. decide whether to adjust retrieval, prompt, or model behavior

The design deliberately avoids fully automatic feedback at this stage. Stability and debuggability are higher priority than automation.

## Error Handling

Tracing failures must not break business requests.

Rules:

- if Phoenix exporter initialization fails at startup, fail fast with a clear configuration error
- if per-request export fails at runtime, log it and continue request processing
- if sample export fails, preserve the original trace and return an actionable error
- if evaluator execution fails, store the failure state with reason instead of silently dropping the sample
- if a trace cannot be fetched from Phoenix, fail the export command clearly without mutating sample state
- if telemetry payloads are truncated, preserve the truncation marker in the stored sample status or metadata
- if a streamed answer ends early or with a model error, export the partial answer with warning and skip answer-dependent metrics

## Testing Strategy

Implementation planning should include tests for:

- tracer initialization and disabled-state behavior
- span creation around chat and ingest flows
- trace-to-sample normalization
- replay correctness for representative chat samples
- evaluator execution on grounded, ungrounded, and citation cases

The design does not require a full production integration environment for the first pass, but it does require deterministic local verification for the new glue code.

## Rollout Plan

Implementation is intentionally split into two phases.

Phase 1: observability instrumentation

1. Phoenix tracer bootstrap and HTTP middleware
2. chat service spans and content events
3. ingest, internal API, and store spans

Phase 2: offline evaluation pipeline

4. MySQL persistence for samples, replay runs, and evaluation results
5. trace normalization and sample export
6. EinoExt replay and evaluator runner

This order gives immediate observability value before the full evaluation loop is complete while keeping planning boundaries explicit.

## Risks

- full-content tracing may create heavy traces for large PDFs or long prompts
- replay outputs may still differ from the original run if the model or prompt-building code changes between capture and replay
- evaluator quality may be misleading if samples lack reference facts or clear expected behavior

Mitigations:

- keep long bodies in chunked events rather than bloated attribute maps
- persist enough sample content to replay without depending on mutable trace storage or live retrieval
- start with a small, explicit metric set and expand only after reviewing real samples

## Success Criteria

The design is successful when:

- every major chat and ingest request appears in Phoenix with usable child spans
- a failing trace can be converted into a normalized sample without manual log reconstruction
- a saved sample can be replayed offline
- EinoExt produces scores or explicit skips for the first four metrics on replayed samples
- developers can move from bad production run to scored offline reproduction in one workflow
