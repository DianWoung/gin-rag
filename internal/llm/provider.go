package llm

import (
	"context"
	"fmt"
	"time"

	openaiembedding "github.com/cloudwego/eino-ext/components/embedding/openai"
	openaichat "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/embedding"
	einomodel "github.com/cloudwego/eino/components/model"

	"github.com/dianwang-mac/go-rag/internal/config"
)

type Provider struct {
	chat      config.ChatConfig
	embedding config.EmbeddingConfig
}

func NewProvider(chatCfg config.ChatConfig, embeddingCfg config.EmbeddingConfig) *Provider {
	return &Provider{
		chat:      chatCfg,
		embedding: embeddingCfg,
	}
}

func (p *Provider) DefaultChatModel() string {
	return p.chat.Model
}

func (p *Provider) NewChatModel(ctx context.Context, modelName string, temperature float32) (einomodel.BaseChatModel, error) {
	if modelName == "" {
		modelName = p.chat.Model
	}

	chatModel, err := openaichat.NewChatModel(ctx, &openaichat.ChatModelConfig{
		APIKey:      p.chat.APIKey,
		BaseURL:     p.chat.BaseURL,
		Model:       modelName,
		Timeout:     60 * time.Second,
		Temperature: &temperature,
	})
	if err != nil {
		return nil, fmt.Errorf("new chat model: %w", err)
	}

	return chatModel, nil
}

func (p *Provider) PingEmbedding(ctx context.Context) error {
	embedder, err := p.NewEmbedder(ctx, "")
	if err != nil {
		return fmt.Errorf("create embedder: %w", err)
	}

	vectors, err := embedder.EmbedStrings(ctx, []string{"ping"})
	if err != nil {
		return fmt.Errorf("embedding ping request: %w", err)
	}
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return fmt.Errorf("embedding returned empty vectors")
	}

	return nil
}

func (p *Provider) NewEmbedder(ctx context.Context, modelName string) (embedding.Embedder, error) {
	if modelName == "" {
		modelName = p.embedding.Model
	}

	embedder, err := openaiembedding.NewEmbedder(ctx, &openaiembedding.EmbeddingConfig{
		APIKey:  p.embedding.APIKey,
		BaseURL: p.embedding.BaseURL,
		Model:   modelName,
		Timeout: 60 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("new embedder: %w", err)
	}

	return embedder, nil
}
