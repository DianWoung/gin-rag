package appdto

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model             string        `json:"model"`
	KnowledgeBaseID   uint          `json:"knowledge_base_id"`
	KnowledgeBaseName string        `json:"knowledge_base_name"`
	Messages          []ChatMessage `json:"messages"`
	Temperature       float32       `json:"temperature"`
	Stream            bool          `json:"stream"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatResult struct {
	Model   string `json:"model"`
	Content string `json:"content"`
	Usage   Usage  `json:"usage"`
}

type ChatStreamChunk struct {
	Model        string `json:"-"`
	Delta        string `json:"-"`
	FinishReason string `json:"-"`
}
