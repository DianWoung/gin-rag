package appdto

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model             string        `json:"model"`
	KnowledgeBaseID   uint          `json:"knowledge_base_id"`
	KnowledgeBaseName string        `json:"knowledge_base_name"`
	Mode              string        `json:"mode,omitempty"`
	MaxSteps          int           `json:"max_steps,omitempty"`
	DocumentIDs       []uint        `json:"document_ids,omitempty"`
	SourceTypes       []string      `json:"source_types,omitempty"`
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
	Model    string        `json:"model"`
	Content  string        `json:"content"`
	Usage    Usage         `json:"usage"`
	Metadata *ChatMetadata `json:"metadata,omitempty"`
}

type ChatStreamChunk struct {
	Model        string `json:"-"`
	Delta        string `json:"-"`
	FinishReason string `json:"-"`
}

type ChatMetadata struct {
	Mode   string      `json:"mode,omitempty"`
	Agent  *AgentTrace `json:"agent,omitempty"`
	Source string      `json:"source,omitempty"`
}

type AgentTrace struct {
	Steps []AgentStep `json:"steps,omitempty"`
}

type AgentStep struct {
	Step           int    `json:"step"`
	Query          string `json:"query"`
	RetrievedCount int    `json:"retrieved_count"`
	Action         string `json:"action"`
}
