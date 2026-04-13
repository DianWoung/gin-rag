package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"

	"github.com/dianwang-mac/go-rag/internal/appdto"
)

type ChatRequest = appdto.ChatRequest
type ChatResult = appdto.ChatResult
type ChatStreamChunk = appdto.ChatStreamChunk
type Usage = appdto.Usage

type ChatService interface {
	ChatCompletion(ctx context.Context, req ChatRequest) (ChatResult, error)
	ChatCompletionStream(ctx context.Context, req ChatRequest) (*schema.StreamReader[ChatStreamChunk], error)
}

type OpenAIHandler struct {
	service ChatService
}

func NewOpenAIHandler(service ChatService) *OpenAIHandler {
	return &OpenAIHandler{service: service}
}

func (h *OpenAIHandler) ChatCompletions(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, badRequest("invalid chat completion payload"))
		return
	}
	if len(req.Messages) == 0 {
		writeError(c, badRequest("messages are required"))
		return
	}
	if req.Stream {
		h.streamChatCompletions(c, req)
		return
	}

	result, err := h.service.ChatCompletion(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}

	modelName := result.Model
	if modelName == "" {
		modelName = req.Model
	}

	c.JSON(http.StatusOK, gin.H{
		"id":      "chatcmpl-mvp",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   modelName,
		"choices": []gin.H{
			{
				"index": 0,
				"message": gin.H{
					"role":    "assistant",
					"content": result.Content,
				},
				"finish_reason": "stop",
			},
		},
		"usage": result.Usage,
		"metadata": result.Metadata,
	})
}

func (h *OpenAIHandler) streamChatCompletions(c *gin.Context, req ChatRequest) {
	stream, err := h.service.ChatCompletionStream(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}
	defer stream.Close()

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		writeError(c, fmt.Errorf("streaming is not supported by the response writer"))
		return
	}

	created := time.Now().Unix()
	modelName := req.Model
	if modelName == "" {
		modelName = "unknown"
	}

	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	sentRole := false
	sentFinish := false
	for {
		chunk, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return
		}
		if chunk.Model != "" {
			modelName = chunk.Model
		}
		if chunk.Delta == "" && chunk.FinishReason == "" {
			continue
		}

		delta := map[string]string{}
		if !sentRole {
			delta["role"] = "assistant"
			sentRole = true
		}
		if chunk.Delta != "" {
			delta["content"] = chunk.Delta
		}
		if chunk.FinishReason != "" {
			sentFinish = true
		}

		if err := writeSSEChunk(c, gin.H{
			"id":      "chatcmpl-mvp",
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   modelName,
			"choices": []gin.H{
				{
					"index":         0,
					"delta":         delta,
					"finish_reason": nullableFinishReason(chunk.FinishReason),
				},
			},
		}); err != nil {
			return
		}
		flusher.Flush()
	}

	if !sentFinish {
		if err := writeSSEChunk(c, gin.H{
			"id":      "chatcmpl-mvp",
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   modelName,
			"choices": []gin.H{
				{
					"index":         0,
					"delta":         map[string]string{},
					"finish_reason": "stop",
				},
			},
		}); err == nil {
			flusher.Flush()
		}
	}

	_, _ = c.Writer.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()
}

func writeSSEChunk(c *gin.Context, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	_, err = c.Writer.Write([]byte("data: " + string(raw) + "\n\n"))
	return err
}

func nullableFinishReason(reason string) any {
	if reason == "" {
		return nil
	}

	return reason
}
