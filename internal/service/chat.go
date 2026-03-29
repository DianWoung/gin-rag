package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"strings"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"

	"github.com/dianwang-mac/go-rag/internal/appdto"
	"github.com/dianwang-mac/go-rag/internal/apperr"
	"github.com/dianwang-mac/go-rag/internal/entity"
	"github.com/dianwang-mac/go-rag/internal/llm"
	"github.com/dianwang-mac/go-rag/internal/observability"
	"github.com/dianwang-mac/go-rag/internal/rerank"
	"github.com/dianwang-mac/go-rag/internal/store"
	"go.opentelemetry.io/otel/attribute"
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

func (s *ChatService) ChatCompletion(ctx context.Context, req appdto.ChatRequest) (result appdto.ChatResult, err error) {
	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanChatCompletion,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceChat),
		attribute.Int(observability.AttrHistoryLength, len(req.Messages)),
		observability.TextAttribute("rag.model", req.Model),
		attribute.Float64("rag.temperature", float64(req.Temperature)),
	)
	defer func() {
		if result.Content != "" {
			span.SetAttributes(
				observability.TextAttribute(observability.AttrAnswer, result.Content),
				attribute.Int("rag.total_tokens", result.Usage.TotalTokens),
			)
		}
		observability.RecordError(span, err)
		span.End()
	}()

	kb, question, history, err := s.prepareChatRequest(ctx, req)
	if err != nil {
		return appdto.ChatResult{}, err
	}
	span.SetAttributes(
		attribute.Int(observability.AttrKnowledgeBaseID, int(kb.ID)),
		observability.TextAttribute(observability.AttrKnowledgeBaseName, kb.Name),
		attribute.String(observability.AttrCollectionName, kb.CollectionName),
		attribute.String(observability.AttrEmbeddingModel, kb.EmbeddingModel),
		observability.TextAttribute(observability.AttrQuestion, question),
		attribute.Int(observability.AttrHistoryLength, len(history)),
	)
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

	result = appdto.ChatResult{
		Model:   modelName,
		Content: answer,
		Usage:   usage,
	}

	return result, nil
}

func (s *ChatService) ChatCompletionStream(ctx context.Context, req appdto.ChatRequest) (stream *schema.StreamReader[appdto.ChatStreamChunk], err error) {
	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanChatCompletionStream,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceChat),
		attribute.Int(observability.AttrHistoryLength, len(req.Messages)),
		observability.TextAttribute("rag.model", req.Model),
		attribute.Float64("rag.temperature", float64(req.Temperature)),
	)
	defer func() {
		observability.RecordError(span, err)
		span.End()
	}()

	kb, question, history, err := s.prepareChatRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.Int(observability.AttrKnowledgeBaseID, int(kb.ID)),
		observability.TextAttribute(observability.AttrKnowledgeBaseName, kb.Name),
		attribute.String(observability.AttrCollectionName, kb.CollectionName),
		attribute.String(observability.AttrEmbeddingModel, kb.EmbeddingModel),
		observability.TextAttribute(observability.AttrQuestion, question),
		attribute.Int(observability.AttrHistoryLength, len(history)),
	)

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

func (s *ChatService) findKnowledgeBase(ctx context.Context, req appdto.ChatRequest) (kb *entity.KnowledgeBase, err error) {
	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanChatFindKnowledgeBase,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceChat),
		attribute.Int(observability.AttrKnowledgeBaseID, int(req.KnowledgeBaseID)),
		observability.TextAttribute(observability.AttrKnowledgeBaseName, req.KnowledgeBaseName),
	)
	defer func() {
		if kb != nil {
			span.SetAttributes(
				attribute.Int(observability.AttrKnowledgeBaseID, int(kb.ID)),
				observability.TextAttribute(observability.AttrKnowledgeBaseName, kb.Name),
				attribute.String(observability.AttrCollectionName, kb.CollectionName),
			)
		}
		observability.RecordError(span, err)
		span.End()
	}()

	query := s.db.WithContext(ctx).Model(&entity.KnowledgeBase{})

	var model entity.KnowledgeBase
	switch {
	case req.KnowledgeBaseID > 0:
		if err = query.First(&model, req.KnowledgeBaseID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				err = apperr.New(http.StatusNotFound, fmt.Errorf("knowledge base not found"))
				return nil, err
			}
			err = fmt.Errorf("find knowledge base by id: %w", err)
			return nil, err
		}
	case strings.TrimSpace(req.KnowledgeBaseName) != "":
		if err = query.Where("name = ?", strings.TrimSpace(req.KnowledgeBaseName)).First(&model).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				err = apperr.New(http.StatusNotFound, fmt.Errorf("knowledge base not found"))
				return nil, err
			}
			err = fmt.Errorf("find knowledge base by name: %w", err)
			return nil, err
		}
	default:
		err = apperr.New(http.StatusBadRequest, fmt.Errorf("knowledge_base_id or knowledge_base_name is required"))
		return nil, err
	}

	kb = &model
	return kb, nil
}

type sourceMatch struct {
	Index    int
	DocTitle string
	Content  string
}

func buildPromptMessages(question string, history []*schema.Message, sources []sourceMatch) []*schema.Message {
	contextBlock := "No retrieved context."
	if len(sources) > 0 {
		parts := make([]string, 0, len(sources))
		for _, src := range sources {
			parts = append(parts, fmt.Sprintf("[%d] (Source: %q)\n%s", src.Index+1, src.DocTitle, src.Content))
		}
		contextBlock = strings.Join(parts, "\n\n")
	}

	messages := make([]*schema.Message, 0, len(history)+2)
	messages = append(messages, &schema.Message{
		Role: schema.System,
		Content: "You are a grounded RAG assistant. Answer the user's question based on the retrieved context below. " +
			"Cite sources using [N] notation (e.g. [1], [2]) when your answer draws from a specific passage. " +
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

func (s *ChatService) rerankMatches(ctx context.Context, query string, matches []store.SearchResult) (reranked []store.SearchResult, err error) {
	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanChatRerank,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceChat),
		observability.TextAttribute(observability.AttrOriginalQuery, query),
		attribute.Int(observability.AttrMatchCount, len(matches)),
	)
	defer func() {
		span.SetAttributes(attribute.Int(observability.AttrMatchCount, len(reranked)))
		observability.RecordError(span, err)
		span.End()
	}()

	texts := make([]string, len(matches))
	for i, m := range matches {
		texts[i] = m.Content
	}

	ranked, err := s.reranker.Rank(ctx, query, texts, retrievalTopK)
	if err != nil {
		return nil, fmt.Errorf("rerank: %w", err)
	}

	reranked = make([]store.SearchResult, len(ranked))
	for i, r := range ranked {
		reranked[i] = matches[r.Index]
		reranked[i].Score = float32(r.Score)
	}
	return reranked, nil
}

const queryRewritePrompt = `Given the following conversation history and a follow-up question, ` +
	`rewrite the follow-up question as a standalone question that captures the full intent. ` +
	`Output ONLY the rewritten question with no preamble.`

// rewriteQuery uses the LLM to condense conversation history and the latest
// question into a single standalone retrieval query. This resolves pronouns,
// omissions, and implicit references so that embedding search works correctly
// for multi-turn conversations.
func (s *ChatService) rewriteQuery(ctx context.Context, chatModel einomodel.BaseChatModel, question string, history []*schema.Message) (rewritten string, err error) {
	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanChatRewriteQuery,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceChat),
		observability.TextAttribute(observability.AttrOriginalQuery, question),
		attribute.Int(observability.AttrHistoryLength, len(history)),
	)
	defer func() {
		if rewritten != "" {
			span.SetAttributes(observability.TextAttribute(observability.AttrRewrittenQuery, rewritten))
		}
		observability.RecordError(span, err)
		span.End()
	}()

	var historyText strings.Builder
	for _, msg := range history {
		historyText.WriteString(fmt.Sprintf("%s: %s\n", msg.Role, msg.Content))
	}

	messages := []*schema.Message{
		{Role: schema.System, Content: queryRewritePrompt},
		{Role: schema.User, Content: fmt.Sprintf("Conversation history:\n%s\nFollow-up question: %s", historyText.String(), question)},
	}

	resp, err := chatModel.Generate(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("generate rewritten query: %w", err)
	}

	rewritten = strings.TrimSpace(resp.Content)
	if rewritten == "" {
		return question, nil
	}

	log.Printf("query rewrite: %q -> %q", question, rewritten)
	return rewritten, nil
}

func (s *ChatService) prepareChatRequest(ctx context.Context, req appdto.ChatRequest) (kb *entity.KnowledgeBase, question string, history []*schema.Message, err error) {
	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanChatPrepareRequest,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceChat),
		attribute.Int(observability.AttrHistoryLength, len(req.Messages)),
	)
	defer func() {
		if kb != nil {
			span.SetAttributes(
				attribute.Int(observability.AttrKnowledgeBaseID, int(kb.ID)),
				attribute.String(observability.AttrCollectionName, kb.CollectionName),
			)
		}
		if question != "" {
			span.SetAttributes(observability.TextAttribute(observability.AttrQuestion, question))
		}
		span.SetAttributes(attribute.Int(observability.AttrHistoryLength, len(history)))
		observability.RecordError(span, err)
		span.End()
	}()

	kb, err = s.findKnowledgeBase(ctx, req)
	if err != nil {
		return nil, "", nil, err
	}
	if kb.CollectionName == "" {
		return nil, "", nil, apperr.New(http.StatusBadRequest, fmt.Errorf("knowledge base has no collection"))
	}

	question, history = splitMessages(req.Messages)
	if question == "" {
		return nil, "", nil, apperr.New(http.StatusBadRequest, fmt.Errorf("last user message is required"))
	}

	return kb, question, history, nil
}

func (s *ChatService) buildRAGRunner(ctx context.Context, kb *entity.KnowledgeBase, req appdto.ChatRequest) (runner compose.Runnable[ragInput, *schema.Message], err error) {
	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanChatBuildRunner,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceChat),
		attribute.Int(observability.AttrKnowledgeBaseID, int(kb.ID)),
		attribute.String(observability.AttrCollectionName, kb.CollectionName),
		attribute.String(observability.AttrEmbeddingModel, kb.EmbeddingModel),
		observability.TextAttribute("rag.model", req.Model),
		attribute.Float64("rag.temperature", float64(req.Temperature)),
	)
	defer func() {
		observability.RecordError(span, err)
		span.End()
	}()

	chatModel, err := s.provider.NewChatModel(ctx, req.Model, req.Temperature)
	if err != nil {
		return nil, err
	}

	embedder, err := s.provider.NewEmbedder(ctx, kb.EmbeddingModel)
	if err != nil {
		return nil, err
	}

	chain := compose.NewChain[ragInput, *schema.Message]()
	chain.AppendLambda(compose.InvokableLambda(func(ctx context.Context, in ragInput) (messages []*schema.Message, err error) {
		ctx, promptSpan := observability.StartSpan(
			ctx,
			observability.SpanChatRAGPrompt,
			attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceChat),
			attribute.String(observability.AttrCollectionName, in.CollectionName),
			attribute.String(observability.AttrEmbeddingModel, in.EmbeddingModel),
			observability.TextAttribute(observability.AttrQuestion, in.Question),
			attribute.Int(observability.AttrHistoryLength, len(in.History)),
		)
		defer func() {
			if len(messages) > 0 {
				promptSpan.SetAttributes(observability.TextAttribute(observability.AttrPrompt, flattenMessages(messages)))
			}
			observability.RecordError(promptSpan, err)
			promptSpan.End()
		}()

		// When conversation history exists, rewrite the question into a
		// standalone retrieval query so that pronouns and omissions are
		// resolved before embedding.
		retrievalQuery := in.Question
		if len(in.History) > 0 {
			rewritten, rwErr := s.rewriteQuery(ctx, chatModel, in.Question, in.History)
			if rwErr != nil {
				// Non-fatal: fall back to the original question.
				log.Printf("query rewrite failed, using original question: %v", rwErr)
			} else if rewritten != "" {
				retrievalQuery = rewritten
			}
		}

		embedCtx, embedSpan := observability.StartSpan(
			ctx,
			observability.SpanChatEmbedQuery,
			attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceChat),
			observability.TextAttribute(observability.AttrOriginalQuery, retrievalQuery),
		)
		vectors, err := embedder.EmbedStrings(embedCtx, []string{retrievalQuery})
		if err != nil {
			err = fmt.Errorf("embed question: %w", err)
			observability.RecordError(embedSpan, err)
			embedSpan.End()
			return nil, err
		}
		embedSpan.SetAttributes(attribute.Int("rag.query_vector_dim", len(vectors[0])))
		embedSpan.End()

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

		reranked := false
		if s.reranker != nil && len(matches) > 0 {
			matches, err = s.rerankMatches(ctx, retrievalQuery, matches)
			if err != nil {
				return nil, err
			}
			reranked = true
		}

		logRetrieval(in.Question, retrievalQuery, matches, reranked)

		sources := s.buildSources(ctx, matches)
		promptSpan.SetAttributes(
			observability.TextAttribute(observability.AttrOriginalQuery, in.Question),
			observability.TextAttribute(observability.AttrRewrittenQuery, retrievalQuery),
			attribute.Int(observability.AttrMatchCount, len(matches)),
			attribute.Bool(observability.AttrReranked, reranked),
			observability.TextListAttribute(observability.AttrRetrievedChunks, matchContents(matches)),
		)

		messages = buildPromptMessages(in.Question, in.History, sources)
		return messages, nil
	}))
	chain.AppendChatModel(chatModel)

	runner, err = chain.Compile(ctx)
	if err != nil {
		return nil, fmt.Errorf("compile rag chain: %w", err)
	}

	return runner, nil
}

// buildSources looks up document titles for the search results and returns
// sourceMatch slices suitable for prompt construction with citations.
func (s *ChatService) buildSources(ctx context.Context, matches []store.SearchResult) (sources []sourceMatch) {
	ctx, span := observability.StartSpan(
		ctx,
		observability.SpanChatBuildSources,
		attribute.String(observability.AttrTraceRole, observability.TraceRoleServiceChat),
		attribute.Int(observability.AttrMatchCount, len(matches)),
	)
	defer span.End()

	if len(matches) == 0 {
		return nil
	}

	// Collect unique document IDs.
	docIDSet := make(map[uint]struct{})
	for _, m := range matches {
		docIDSet[m.DocumentID] = struct{}{}
	}
	docIDs := make([]uint, 0, len(docIDSet))
	for id := range docIDSet {
		docIDs = append(docIDs, id)
	}

	// Batch-fetch titles from MySQL.
	titleMap := make(map[uint]string)
	var docs []entity.Document
	if err := s.db.WithContext(ctx).Select("id, title").Where("id IN ?", docIDs).Find(&docs).Error; err != nil {
		log.Printf("failed to fetch document titles: %v", err)
	} else {
		for _, d := range docs {
			titleMap[d.ID] = d.Title
		}
	}

	sources = make([]sourceMatch, len(matches))
	titles := make([]string, 0, len(matches))
	for i, m := range matches {
		title := titleMap[m.DocumentID]
		if title == "" {
			title = fmt.Sprintf("doc_%d", m.DocumentID)
		}
		titles = append(titles, title)
		sources[i] = sourceMatch{
			Index:    i,
			DocTitle: title,
			Content:  m.Content,
		}
	}
	span.SetAttributes(observability.TextListAttribute("rag.source_titles", titles))
	return sources
}

// logRetrieval emits a structured log entry with retrieval metrics for offline
// quality analysis.
func logRetrieval(originalQuery, retrievalQuery string, matches []store.SearchResult, reranked bool) {
	scores := make([]float64, 0, len(matches))
	docIDs := make([]uint, 0, len(matches))
	for _, m := range matches {
		scores = append(scores, float64(m.Score))
		docIDs = append(docIDs, m.DocumentID)
	}

	attrs := []any{
		slog.String("original_query", originalQuery),
		slog.Int("match_count", len(matches)),
		slog.Bool("reranked", reranked),
		slog.Any("scores", scores),
		slog.Any("doc_ids", docIDs),
	}
	if retrievalQuery != originalQuery {
		attrs = append(attrs, slog.String("retrieval_query", retrievalQuery))
	}

	slog.Info("retrieval", attrs...)
}

func flattenMessages(messages []*schema.Message) string {
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		parts = append(parts, fmt.Sprintf("%s: %s", message.Role, message.Content))
	}

	return strings.Join(parts, "\n\n")
}

func matchContents(matches []store.SearchResult) []string {
	contents := make([]string, 0, len(matches))
	for _, match := range matches {
		contents = append(contents, match.Content)
	}

	return contents
}
