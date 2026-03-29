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
- trace payloads that keep full request and response content
- a normalized sample model for replay and evaluation
- a half-automatic issue loop from Phoenix trace to EinoExt score

This design does not cover:

- CI gating
- multi-tenant privacy controls
- automated feedback into prompt or retrieval tuning
- dashboard product work beyond what Phoenix already provides

## Recommended Approach

Use a mixed architecture:

- Phoenix owns online observability
- EinoExt owns offline quality scoring
- project-local glue code owns trace normalization and replay

This keeps responsibilities clear:

- Phoenix is optimized for runtime inspection and span-level debugging
- EinoExt remains the programmable evaluation layer for business-specific scoring
- the service code stays close to its current layered structure

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

`Phoenix trace -> normalized sample -> offline replay -> EinoExt evaluation -> persisted evaluation result`

### Units

#### `internal/observability`

Responsibilities:

- initialize OpenTelemetry tracer/exporter for Phoenix OTLP
- provide helpers for starting spans and attaching consistent attributes
- expose middleware and service helpers without embedding Phoenix-specific logic into business code

Must not:

- decide business evaluation rules
- know sample scoring semantics

#### `internal/tracebridge`

Responsibilities:

- define shared attribute keys and event shapes
- map business objects into trace payloads
- extract trace-backed replay input into normalized samples

Must not:

- start transport clients
- call EinoExt evaluators directly

#### `internal/eval`

Responsibilities:

- define the normalized evaluation sample model
- replay a sample against the current service implementation
- run EinoExt evaluators
- persist evaluation outputs

Must not:

- own production tracing configuration
- inspect HTTP or database internals directly

## Trace Coverage

Initial rollout covers the full service:

- chat request flow
- document import and indexing flow
- knowledge-base operations
- internal API handlers
- MySQL and Qdrant access

This is broad on purpose. The user explicitly prioritized end-to-end troubleshooting over minimal initial coverage.

## Span Design

Use one trace per inbound request with child spans for major business steps.

### Root spans

- `http.chat_completions`
- `http.import_text_document`
- `http.import_pdf_document`
- `http.index_document`
- `http.create_knowledge_base`
- `http.delete_document`
- `http.delete_knowledge_base`

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
- `rag.original_query`
- `rag.retrieval_query`
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
- `input.retrieved_chunks`
- `input.prompt_full`
- `output.answer`
- `document.raw_text`
- `document.chunk_text`

Each event should carry:

- the full text body
- a length field
- a stable sample correlation field when available

## Evaluation Sample Model

Define a normalized sample shape that is independent from Phoenix internals:

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
- `actual_answer`
- `citations`
- `expected_answer`
- `reference_facts`
- `labels`

`created_from` is one of:

- `production_failure`
- `low_quality_trace`
- `manual_case`

`expected_answer` is optional. Some samples will rely on reference facts or groundedness checks rather than exact-match answers.

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

## Replay Flow

Replay runs should use the normalized sample instead of raw Phoenix payloads.

Replay sequence:

1. load a saved sample
2. rebuild the request context
3. rerun the current chat pipeline
4. compare the new run with the original sample
5. score it with EinoExt evaluators
6. save the evaluation result

This makes evaluation resilient to future changes in Phoenix trace representation.

## Persistence

Evaluation persistence can start small.

Required persisted records:

- raw normalized sample
- replay output
- evaluator scores
- evaluator summary or failure reason

Storage location is implementation-defined, but it must be queryable from the application without scraping logs.

## Operational Flow

First-release workflow is half-automatic:

1. inspect a bad trace in Phoenix
2. mark it for export
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

## Testing Strategy

Implementation planning should include tests for:

- tracer initialization and disabled-state behavior
- span creation around chat and ingest flows
- trace-to-sample normalization
- replay correctness for representative chat samples
- evaluator execution on grounded, ungrounded, and citation cases

The design does not require a full production integration environment for the first pass, but it does require deterministic local verification for the new glue code.

## Rollout Plan

Implement in this order:

1. Phoenix tracer bootstrap and HTTP middleware
2. chat service spans and content events
3. ingest and knowledge-base spans
4. trace normalization and sample export
5. EinoExt replay and evaluator runner
6. persistence for samples and scores

This order gives immediate observability value before the full evaluation loop is complete.

## Risks

- full-content tracing may create heavy traces for large PDFs or long prompts
- replay can drift if knowledge-base state changes between original run and offline evaluation
- evaluator quality may be misleading if samples lack reference facts or clear expected behavior

Mitigations:

- keep long bodies in events rather than bloated attribute maps
- persist enough sample content to replay without depending on mutable trace storage
- start with a small, explicit metric set and expand only after reviewing real samples

## Success Criteria

The design is successful when:

- every major chat and ingest request appears in Phoenix with usable child spans
- a failing trace can be converted into a normalized sample without manual log reconstruction
- a saved sample can be replayed offline
- EinoExt produces scores for the first four metrics on replayed samples
- developers can move from bad production run to scored offline reproduction in one workflow
