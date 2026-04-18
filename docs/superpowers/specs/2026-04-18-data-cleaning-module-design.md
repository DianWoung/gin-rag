# Data Cleaning Module Design

## Goal

Introduce a conservative text-cleaning module for ingestion so extracted content is normalized before block building and chunking, without rewriting likely-valid user content.

This work targets high-confidence cleanup only:

- whitespace normalization
- BOM / NUL removal
- repeated blank-line collapse
- consecutive duplicate-line collapse
- high-confidence page-number removal
- high-confidence repeated header/footer removal

It explicitly does not attempt:

- OCR correction
- semantic rewriting
- multi-column reordering
- aggressive paragraph merging
- speculative table repair

## Scope

This module should apply to both:

- text imported through `/api/documents/import-text`
- text extracted from `/api/documents/import-pdf`

The cleaning step should run once, after raw text is available and before `BuildBlocks` / `SplitBlocks`.

## Recommended Approach

Add a dedicated cleaner inside `internal/ingest` and keep it pure and deterministic.

Why this approach:

- `internal/ingest` already owns extraction and structural preparation
- the rules are reusable across PDF and plain-text ingestion
- unit tests can target the cleaner directly without involving DB/Qdrant
- `service/document` stays orchestration-focused instead of absorbing text heuristics

## Architecture

### New Unit

Add `internal/ingest/cleaner.go` with:

- `type Cleaner struct{}`
- `func NewCleaner() *Cleaner`
- `func (c *Cleaner) Clean(raw string) (cleaned string, report CleanReport)`

Add `CleanReport` with counters only, for example:

- `RemovedBlankLines`
- `RemovedDuplicateLines`
- `RemovedPageNumberLines`
- `RemovedRepeatedHeaderFooterLines`
- `Changed`

### Integration Point

Update `DocumentService` so ingestion flow becomes:

1. obtain raw text
2. run cleaner
3. store cleaned content in `documents.content`
4. later index using cleaned content through `BuildBlocks` and `SplitBlocks`

This keeps a single canonical cleaned document body inside MySQL and avoids re-cleaning inconsistently during every re-index.

## Cleaning Rules

Rules must be conservative and ordered from lowest to higher risk.

### Rule 1: Character and newline normalization

- strip BOM
- strip `\x00`
- normalize `\r\n` / `\r` to `\n`
- trim trailing spaces on each line

### Rule 2: Blank-line collapse

- collapse runs of blank lines to a single blank line
- preserve paragraph boundaries

### Rule 3: Consecutive duplicate-line collapse

Only remove duplicates when:

- the same normalized line appears consecutively
- the line is short enough to look like a header/footer/noise rather than paragraph body

Do not remove non-consecutive duplicates. Repeated legitimate headings across sections must remain unless they are obvious page furniture.

### Rule 4: Page-number removal

Only remove high-confidence standalone page-number lines such as:

- `1`
- `12`
- `- 3 -`
- `Page 4`
- `第 5 页`

Only when the full line matches the page-number pattern.

### Rule 5: Repeated header/footer removal

Only remove lines when all conditions hold:

- short normalized line
- appears on multiple separated positions
- strongly suggests page furniture instead of content

This should be implemented with a conservative threshold. If uncertain, keep the line.

## Data Model Impact

No schema change is required for the first version.

`CleanReport` stays in-process only. It can later be added to trace attributes if operators need visibility, but this version should avoid schema churn.

## Observability

If tracing is already active for document import/index:

- add a compact summary string or counters to the relevant span
- avoid logging full cleaned text twice

The important signal is whether cleaning changed the content and what kind of noise was removed.

## Error Handling

Cleaning should not fail for malformed text. It should always return best-effort output.

Rules:

- empty input returns empty output and zeroed report
- cleaning never returns an error in v1
- if a rule cannot confidently classify a line, it must keep the line

## Testing Strategy

### Cleaner unit tests

Add table-driven tests for:

- newline normalization
- blank-line collapse
- duplicate-line collapse
- page-number removal
- repeated header/footer removal
- “do not remove” cases for normal content

### Integration tests

Update document-service tests to confirm cleaned content is what gets stored/indexed.

At least one test should prove that noisy imported content produces cleaner chunks after indexing.

## Risks

Main risk: false positive removal of valid content.

Mitigations:

- keep rules pattern-based and conservative
- avoid non-consecutive dedupe except obvious header/footer cases
- add explicit negative tests for legitimate short lines

## Success Criteria

This work is successful when:

- ingestion path applies cleaner before block/chunk construction
- noisy page numbers and duplicate headers/footers are reduced in stored content
- current chunking and evaluation tests continue to pass
- no aggressive text rewriting is introduced
