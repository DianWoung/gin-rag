package tracebridge

type ChatSample struct {
	TraceID           string          `json:"trace_id"`
	ProjectName       string          `json:"project_name"`
	RootSpanName      string          `json:"root_span_name"`
	Question          string          `json:"question"`
	Answer            string          `json:"answer"`
	Prompt            string          `json:"prompt"`
	Model             string          `json:"model,omitempty"`
	Temperature       float32         `json:"temperature,omitempty"`
	KnowledgeBaseID   uint            `json:"knowledge_base_id,omitempty"`
	KnowledgeBaseName string          `json:"knowledge_base_name,omitempty"`
	CollectionName    string          `json:"collection_name,omitempty"`
	EmbeddingModel    string          `json:"embedding_model,omitempty"`
	Chunks            []RetrievedChunk `json:"chunks,omitempty"`
}

type RetrievedChunk struct {
	Index   int    `json:"index"`
	Content string `json:"content"`
}

type ExportWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
