package tracebridge

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/dianwang-mac/go-rag/internal/observability"
	"github.com/dianwang-mac/go-rag/internal/phoenix"
)

func NormalizeChatTrace(trace phoenix.TraceEnvelope) (ChatSample, []ExportWarning, error) {
	if trace.TraceID == "" {
		return ChatSample{}, nil, fmt.Errorf("trace id is required")
	}

	root := trace.RootSpan
	if root == nil || root.Name != "http.v1.chat_completions" {
		return ChatSample{}, nil, fmt.Errorf("trace %q is not a chat completion trace", trace.TraceID)
	}

	chatSpan := findSpan(trace.Spans, observability.SpanChatCompletion)
	if chatSpan == nil {
		chatSpan = findSpan(trace.Spans, observability.SpanChatCompletionStream)
	}
	if chatSpan == nil {
		return ChatSample{}, nil, fmt.Errorf("chat completion span not found")
	}

	promptSpan := findSpan(trace.Spans, observability.SpanChatRAGPrompt)
	if promptSpan == nil {
		return ChatSample{}, nil, fmt.Errorf("chat prompt span not found")
	}

	prompt := stringAttr(promptSpan.Attributes, observability.AttrPrompt)
	if prompt == "" {
		return ChatSample{}, nil, fmt.Errorf("prompt attribute missing")
	}
	if isTruncated(prompt) {
		return ChatSample{}, nil, fmt.Errorf("prompt is truncated in trace payload")
	}

	question := stringAttr(chatSpan.Attributes, observability.AttrQuestion)
	answer := stringAttr(chatSpan.Attributes, observability.AttrAnswer)

	warnings := make([]ExportWarning, 0, 2)
	if answer == "" {
		warnings = append(warnings, ExportWarning{
			Code:    "missing_answer",
			Message: "trace does not include final answer content",
		})
	}

	promptMessages, err := promptMessagesAttr(promptSpan.Attributes, observability.AttrPromptMessagesJSON)
	if err != nil {
		return ChatSample{}, nil, err
	}

	chunkTexts := splitJoinedText(stringAttr(promptSpan.Attributes, observability.AttrRetrievedChunks))
	chunks := make([]RetrievedChunk, 0, len(chunkTexts))
	for index, content := range chunkTexts {
		if isTruncated(content) {
			warnings = append(warnings, ExportWarning{
				Code:    "truncated_chunk",
				Message: fmt.Sprintf("retrieved chunk %d is truncated", index),
			})
		}
		chunks = append(chunks, RetrievedChunk{
			Index:   index,
			Content: content,
		})
	}

	sample := ChatSample{
		TraceID:           trace.TraceID,
		ProjectName:       trace.ProjectName,
		RootSpanName:      root.Name,
		Question:          question,
		OriginalQuery:     stringAttr(promptSpan.Attributes, observability.AttrOriginalQuery),
		RewrittenQuery:    stringAttr(promptSpan.Attributes, observability.AttrRewrittenQuery),
		Answer:            answer,
		Prompt:            prompt,
		PromptMessages:    promptMessages,
		Model:             stringAttr(chatSpan.Attributes, "rag.model"),
		Temperature:       float32(numberAttr(chatSpan.Attributes, "rag.temperature")),
		KnowledgeBaseID:   uint(intAttr(chatSpan.Attributes, observability.AttrKnowledgeBaseID)),
		KnowledgeBaseName: stringAttr(chatSpan.Attributes, observability.AttrKnowledgeBaseName),
		CollectionName:    stringAttr(chatSpan.Attributes, observability.AttrCollectionName),
		EmbeddingModel:    stringAttr(chatSpan.Attributes, observability.AttrEmbeddingModel),
		Chunks:            chunks,
	}
	if sample.OriginalQuery == "" {
		sample.OriginalQuery = question
	}
	if sample.RewrittenQuery == "" {
		sample.RewrittenQuery = sample.OriginalQuery
	}

	return sample, warnings, nil
}

func findSpan(spans []phoenix.TraceSpan, name string) *phoenix.TraceSpan {
	for i := range spans {
		if spans[i].Name == name {
			return &spans[i]
		}
	}

	return nil
}

func stringAttr(attrs map[string]any, key string) string {
	if attrs == nil {
		return ""
	}
	value, ok := attrs[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(typed)
	}
}

func intAttr(attrs map[string]any, key string) int {
	value := stringAttr(attrs, key)
	if value == "" {
		return 0
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}

func numberAttr(attrs map[string]any, key string) float64 {
	value, ok := attrs[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

func splitJoinedText(value string) []string {
	if value == "" {
		return nil
	}

	return strings.Split(value, "\n---\n")
}

func isTruncated(value string) bool {
	return strings.Contains(value, "...(truncated)")
}

func promptMessagesAttr(attrs map[string]any, key string) ([]PromptMessage, error) {
	raw := stringAttr(attrs, key)
	if raw == "" {
		return nil, nil
	}

	var messages []PromptMessage
	if err := json.Unmarshal([]byte(raw), &messages); err != nil {
		return nil, fmt.Errorf("decode prompt messages: %w", err)
	}

	return messages, nil
}
