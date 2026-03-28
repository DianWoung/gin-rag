package appdto

type CreateKnowledgeBaseRequest struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	EmbeddingModel string `json:"embedding_model"`
}

type ImportTextDocumentRequest struct {
	KnowledgeBaseID uint   `json:"knowledge_base_id" form:"knowledge_base_id"`
	Title           string `json:"title" form:"title"`
	Content         string `json:"content" form:"content"`
	SourceType      string `json:"source_type" form:"source_type"`
}

type ImportPDFDocumentRequest struct {
	KnowledgeBaseID uint   `json:"knowledge_base_id" form:"knowledge_base_id"`
	Title           string `json:"title" form:"title"`
	FileName        string `json:"file_name" form:"file_name"`
	FilePath        string `json:"file_path" form:"file_path"`
	Content         []byte `json:"content"`
}
