package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"

	"github.com/dianwang-mac/go-rag/internal/appdto"
	"github.com/dianwang-mac/go-rag/internal/apperr"
	"github.com/dianwang-mac/go-rag/internal/entity"
	"github.com/dianwang-mac/go-rag/internal/llm"
	"github.com/dianwang-mac/go-rag/internal/rerank"
	"github.com/dianwang-mac/go-rag/internal/store"
)

const (
	retrievalTopK = 4  // final number of chunks fed to LLM
	rerankFetchK  = 20 // broader recall when reranker is available
)

type ChatService struct {
	db       *gorm.DB
	vectors  *store.QdrantStore
	provider *llm.Provider
	reranker *rerank.Reranker // nil when reranking is disabled
}

type ragInput struct {
	Question       string
	History        []*schema.Message
	CollectionName string
	EmbeddingModel string
}

func NewChatService(db *gorm.DB, vectors *store.QdrantStore, provider *llm.Provider, reranker *rerank.Reranker) *ChatService {
	return &ChatService{
		db:       db,
		vectors:  vectors,
		provider: provider,
		reranker: reranker,
	}
}

func (s *ChatService) ChatCompletion(ctx context.Context, req appdto.ChatRequest) (appdto.ChatResult, error) {
	kb, question, history, err := s.prepareChatRequest(ctx, req)
	if err != nil {
		return appdto.ChatResult{}, err
	}
	runner, err := s.buildRAGRunner(ctx, kb, req)
	if err != nil {
		return appdto.ChatResult{}, err
	}

	resp, err := runner.Invoke(ctx, ragInput{
		Question:       question,
		History:        history,
		CollectionName: kb.CollectionName,
		EmbeddingModel: kb.EmbeddingModel,
	})
	if err != nil {
		return appdto.ChatResult{}, fmt.Errorf("invoke rag chain: %w", err)
	}

	answer := strings.TrimSpace(resp.Content)
	usage := appdto.Usage{
		PromptTokens:     estimateTokens(question) + estimateTokens(joinHistory(history)),
		CompletionTokens: estimateTokens(answer),
	}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	modelName := req.Model
	if modelName == "" {
		modelName = s.provider.DefaultChatModel()
	}

	return appdto.ChatResult{
		Model:   modelName,
		Content: answer,
		Usage:   usage,
	}, nil
}

func (s *ChatService) ChatCompletionStream(ctx context.Context, req appdto.ChatRequest) (*schema.StreamReader[appdto.ChatStreamChunk], error) {
	kb, question, history, err := s.prepareChatRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	runner, err := s.buildRAGRunner(ctx, kb, req)
	if err != nil {
		return nil, err
	}

	modelName := req.Model
	if modelName == "" {
		modelName = s.provider.DefaultChatModel()
	}

	upstream, err := runner.Stream(ctx, ragInput{
		Question:       question,
		History:        history,
		CollectionName: kb.CollectionName,
		EmbeddingModel: kb.EmbeddingModel,
	})
	if err != nil {
		return nil, fmt.Errorf("stream rag chain: %w", err)
	}

	stream, writer := schema.Pipe[appdto.ChatStreamChunk](0)
	go func() {
		defer upstream.Close()
		defer writer.Close()

		for {
			msg, recvErr := upstream.Recv()
			if errors.Is(recvErr, io.EOF) {
				return
			}
			if recvErr != nil {
				writer.Send(appdto.ChatStreamChunk{}, recvErr)
				return
			}

			chunk := appdto.ChatStreamChunk{
				Model: modelName,
				Delta: msg.Content,
			}
			if msg.ResponseMeta != nil {
				chunk.FinishReason = msg.ResponseMeta.FinishReason
			}
			if chunk.Delta == "" && chunk.FinishReason == "" {
				continue
			}

			writer.Send(chunk, nil)
		}
	}()

	return stream, nil
}

func (s *ChatService) findKnowledgeBase(ctx context.Context, req appdto.ChatRequest) (*entity.KnowledgeBase, error) {
	query := s.db.WithContext(ctx).Model(&entity.KnowledgeBase{})

	var kb entity.KnowledgeBase
	switch {
	case req.KnowledgeBaseID > 0:
		if err := query.First(&kb, req.KnowledgeBaseID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, apperr.New(http.StatusNotFound, fmt.Errorf("knowledge base not found"))
			}
			return nil, fmt.Errorf("find knowledge base by id: %w", err)
		}
	case strings.TrimSpace(req.KnowledgeBaseName) != "":
		if err := query.Where("name = ?", strings.TrimSpace(req.KnowledgeBaseName)).First(&kb).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, apperr.New(http.StatusNotFound, fmt.Errorf("knowledge base not found"))
			}
			return nil, fmt.Errorf("find knowledge base by name: %w", err)
		}
	default:
		return nil, apperr.New(http.StatusBadRequest, fmt.Errorf("knowledge_base_id or knowledge_base_name is required"))
	}

	return &kb, nil
}

func buildPromptMessages(question string, history []*schema.Message, matches []store.SearchResult) []*schema.Message {
	contextBlock := "No retrieved context."
	if len(matches) > 0 {
		parts := make([]string, 0, len(matches))
		for _, match := range matches {
			parts = append(parts, fmt.Sprintf("[%d] %s", match.ChunkIndex, match.Content))
		}
		contextBlock = strings.Join(parts, "\n\n")
	}

	messages := make([]*schema.Message, 0, len(history)+2)
	messages = append(messages, &schema.Message{
		Role: schema.System,
		Content: "You are a grounded RAG assistant. Use the retrieved context when relevant. " +
			"If the answer is not supported by context, say so clearly.\n\nRetrieved context:\n" + contextBlock,
	})
	messages = append(messages, history...)
	messages = append(messages, &schema.Message{
		Role:    schema.User,
		Content: question,
	})

	return messages
}

func splitMessages(messages []appdto.ChatMessage) (string, []*schema.Message) {
	if len(messages) == 0 {
		return "", nil
	}

	var history []*schema.Message
	lastUserIndex := -1
	for idx := len(messages) - 1; idx >= 0; idx-- {
		if strings.EqualFold(messages[idx].Role, "user") {
			lastUserIndex = idx
			break
		}
	}

	if lastUserIndex < 0 {
		return "", nil
	}

	for idx, message := range messages {
		if idx >= lastUserIndex {
			break
		}
		history = append(history, &schema.Message{
			Role:    schema.RoleType(strings.ToLower(message.Role)),
			Content: message.Content,
		})
	}

	return messages[lastUserIndex].Content, history
}

func joinHistory(history []*schema.Message) string {
	parts := make([]string, 0, len(history))
	for _, message := range history {
		parts = append(parts, message.Content)
	}

	return strings.Join(parts, "\n")
}

func (s *ChatService) rerankMatches(ctx context.Context, query string, matches []store.SearchResult) ([]store.SearchResult, error) {
	texts := make([]string, len(matches))
	for i, m := range matches {
		texts[i] = m.Content
	}

	ranked, err := s.reranker.Rank(ctx, query, texts, retrievalTopK)
	if err != nil {
		return nil, fmt.Errorf("rerank: %w", err)
	}

	reranked := make([]store.SearchResult, len(ranked))
	for i, r := range ranked {
		reranked[i] = matches[r.Index]
		reranked[i].Score = float32(r.Score)
	}
	return reranked, nil
}

func (s *ChatService) prepareChatRequest(ctx context.Context, req appdto.ChatRequest) (*entity.KnowledgeBase, string, []*schema.Message, error) {
	kb, err := s.findKnowledgeBase(ctx, req)
	if err != nil {
		return nil, "", nil, err
	}
	if kb.CollectionName == "" {
		return nil, "", nil, apperr.New(http.StatusBadRequest, fmt.Errorf("knowledge base has no collection"))
	}

	question, history := splitMessages(req.Messages)
	if question == "" {
		return nil, "", nil, apperr.New(http.StatusBadRequest, fmt.Errorf("last user message is required"))
	}

	return kb, question, history, nil
}

func (s *ChatService) buildRAGRunner(ctx context.Context, kb *entity.KnowledgeBase, req appdto.ChatRequest) (compose.Runnable[ragInput, *schema.Message], error) {
	chatModel, err := s.provider.NewChatModel(ctx, req.Model, req.Temperature)
	if err != nil {
		return nil, err
	}

	embedder, err := s.provider.NewEmbedder(ctx, kb.EmbeddingModel)
	if err != nil {
		return nil, err
	}

	chain := compose.NewChain[ragInput, *schema.Message]()
	chain.AppendLambda(compose.InvokableLambda(func(ctx context.Context, in ragInput) ([]*schema.Message, error) {
		vectors, err := embedder.EmbedStrings(ctx, []string{in.Question})
		if err != nil {
			return nil, fmt.Errorf("embed question: %w", err)
		}

		// When reranker is available, cast a wider net and let the
		// cross-encoder pick the best matches.
		fetchK := uint64(retrievalTopK)
		if s.reranker != nil {
			fetchK = rerankFetchK
		}

		matches, err := s.vectors.Search(ctx, in.CollectionName, vectors[0], fetchK)
		if err != nil {
			return nil, err
		}

		if s.reranker != nil && len(matches) > 0 {
			matches, err = s.rerankMatches(ctx, in.Question, matches)
			if err != nil {
				return nil, err
			}
		}

		return buildPromptMessages(in.Question, in.History, matches), nil
	}))
	chain.AppendChatModel(chatModel)

	runner, err := chain.Compile(ctx)
	if err != nil {
		return nil, fmt.Errorf("compile rag chain: %w", err)
	}

	return runner, nil
}
