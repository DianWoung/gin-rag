package observability

import (
	"net/http"
	"strings"
)

const (
	AttrHTTPMethod     = "http.request.method"
	AttrHTTPRoute      = "http.route"
	AttrHTTPPath       = "url.path"
	AttrHTTPStatusCode = "http.response.status_code"
	AttrTraceRole      = "rag.trace_role"
	AttrKnowledgeBaseID   = "rag.knowledge_base_id"
	AttrKnowledgeBaseName = "rag.knowledge_base_name"
	AttrDocumentID        = "rag.document_id"
	AttrCollectionName    = "rag.collection_name"
	AttrEmbeddingModel    = "rag.embedding_model"
	AttrQuestion          = "rag.question"
	AttrAnswer            = "rag.answer"
	AttrPrompt            = "rag.prompt"
	AttrPromptMessagesJSON = "rag.prompt_messages_json"
	AttrOriginalQuery     = "rag.query.original"
	AttrRewrittenQuery    = "rag.query.rewritten"
	AttrRetrievedChunks   = "rag.retrieved_chunks"
	AttrChunkBodies       = "rag.chunk_bodies"
	AttrChunkCount        = "rag.chunk_count"
	AttrMatchCount        = "rag.match_count"
	AttrHistoryLength     = "rag.history_length"
	AttrReranked          = "rag.reranked"
	AttrTitle             = "rag.title"
	AttrSourceType        = "rag.source_type"

	TraceRoleHTTPRequest = "http.request"
	TraceRoleServiceChat = "service.chat"
	TraceRoleServiceDocument = "service.document"
	TraceRoleServiceKnowledgeBase = "service.knowledge_base"
	TraceRoleVectorStore = "store.vector"
)

const (
	SpanChatCompletion         = "service.chat.completion"
	SpanChatCompletionStream   = "service.chat.completion_stream"
	SpanChatFindKnowledgeBase  = "service.chat.find_knowledge_base"
	SpanChatPrepareRequest     = "service.chat.prepare_request"
	SpanChatBuildRunner        = "service.chat.build_runner"
	SpanChatRewriteQuery       = "service.chat.rewrite_query"
	SpanChatEmbedQuery         = "service.chat.embed_query"
	SpanChatRAGPrompt          = "service.chat.rag_prompt"
	SpanChatRerank             = "service.chat.rerank"
	SpanChatBuildSources       = "service.chat.build_sources"
	SpanDocumentImportText     = "service.document.import_text"
	SpanDocumentImportPDF      = "service.document.import_pdf"
	SpanDocumentIndex          = "service.document.index"
	SpanDocumentDelete         = "service.document.delete"
	SpanDocumentList           = "service.document.list"
	SpanDocumentCreate         = "service.document.create"
	SpanDocumentSplit          = "service.document.split"
	SpanDocumentEmbedChunks    = "service.document.embed_chunks"
	SpanKnowledgeBaseCreate    = "service.knowledge_base.create"
	SpanKnowledgeBaseDelete    = "service.knowledge_base.delete"
	SpanKnowledgeBaseList      = "service.knowledge_base.list"
	SpanQdrantEnsureCollection = "store.qdrant.ensure_collection"
	SpanQdrantUpsertChunks     = "store.qdrant.upsert_chunks"
	SpanQdrantSearch           = "store.qdrant.search"
	SpanQdrantDeletePoints     = "store.qdrant.delete_points"
	SpanQdrantDeleteCollection = "store.qdrant.delete_collection"
)

var routeSpanNames = map[string]string{
	http.MethodGet + " /healthz":                  "http.healthz",
	http.MethodPost + " /v1/chat/completions":    "http.v1.chat_completions",
	http.MethodPost + " /api/knowledge-bases":    "http.api.create_knowledge_base",
	http.MethodGet + " /api/knowledge-bases":     "http.api.list_knowledge_bases",
	http.MethodDelete + " /api/knowledge-bases/:id": "http.api.delete_knowledge_base",
	http.MethodPost + " /api/documents/import-text": "http.api.import_text_document",
	http.MethodPost + " /api/documents/import-pdf":  "http.api.import_pdf_document",
	http.MethodPost + " /api/documents/:id/index":   "http.api.index_document",
	http.MethodDelete + " /api/documents/:id":       "http.api.delete_document",
	http.MethodGet + " /api/documents":              "http.api.list_documents",
}

func RouteSpanName(method, route string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	route = strings.TrimSpace(route)
	if route == "" {
		return "http.unknown"
	}
	if name, ok := routeSpanNames[method+" "+route]; ok {
		return name
	}

	route = strings.TrimPrefix(route, "/")
	replacer := strings.NewReplacer("/", ".", "-", "_", ":", "", "{", "", "}", "")
	route = replacer.Replace(route)
	route = strings.Trim(route, ".")
	if route == "" {
		return "http.unknown"
	}

	return "http." + route
}
