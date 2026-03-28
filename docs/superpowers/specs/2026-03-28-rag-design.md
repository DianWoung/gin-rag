# Go RAG MVP Design

## Goal

Build a minimal but runnable RAG service in Go using gin, gorm, MySQL, Eino, and Qdrant, with:

- internal knowledge-base/document ingestion APIs
- MySQL metadata models for knowledge bases, documents, and document chunks
- Qdrant vector indexing and retrieval
- an OpenAI-compatible `POST /v1/chat/completions` endpoint

## Recommended Approach

Use a layered HTTP service with:

- gin for transport
- gorm for metadata persistence
- a thin Qdrant repository for vectors and payloads
- Eino compose chain for retrieval plus generation orchestration
- OpenAI-compatible upstream chat and embedding providers through Eino OpenAI components

This keeps the MVP small while leaving clean seams for future loaders, auth, streaming, and async indexing.

## Architecture

`gin handler -> service -> gorm/qdrant -> Eino chain -> upstream LLM`

- `knowledge_bases` defines one logical collection and stores vector dimension plus collection name
- `documents` stores import metadata and raw content
- `document_chunks` stores chunk text, ordering, and Qdrant point IDs
- indexing is synchronous for the MVP: split, embed, persist chunks, upsert vectors
- chat completion retrieves by KB, builds grounded prompt, invokes Eino chat chain, and maps response to OpenAI format

## Error Handling

- centralized JSON error format for HTTP handlers
- service-level wrapped errors
- basic validation for required fields
- explicit `400` for unsupported streaming in MVP

## Known MVP Boundaries

- no auth or multi-tenant isolation
- no async jobs or progress tracking
- no advanced document parsers beyond plain text upload/import
- no SSE streaming output
