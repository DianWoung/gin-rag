package eval

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/dianwang-mac/go-rag/internal/tracebridge"
)

const (
	TargetCaptured = "captured"
	TargetReplay   = "replay"

	StatusScored  = "scored"
	StatusSkipped = "skipped"
	StatusError   = "error"
)

const (
	retrievalPrecisionK             = 4
	queryChunkRelevantOverlapCutoff = 0.1
)

type ResultSummary struct {
	Target       string             `json:"target"`
	ScoredCount  int                `json:"scored_count"`
	SkippedCount int                `json:"skipped_count"`
	ErrorCount   int                `json:"error_count"`
	AverageScore float64            `json:"average_score"`
	MetricScores map[string]float64 `json:"metric_scores,omitempty"`
}

func ScoreChatSample(stored StoredSample, replay *ReplayRun) []EvaluationResult {
	results := scoreTarget(stored.SampleID, "", TargetCaptured, stored.Sample.Answer, stored.Sample)
	if replay != nil {
		results = append(results, scoreTarget(stored.SampleID, replay.ReplayRunID, TargetReplay, replay.Answer, stored.Sample)...)
	}

	return results
}

func SummarizeResults(results []EvaluationResult) []ResultSummary {
	if len(results) == 0 {
		return nil
	}

	grouped := map[string]*ResultSummary{}
	scoreTotals := map[string]float64{}
	for _, result := range results {
		summary, ok := grouped[result.Target]
		if !ok {
			summary = &ResultSummary{
				Target:       result.Target,
				MetricScores: map[string]float64{},
			}
			grouped[result.Target] = summary
		}

		switch result.Status {
		case StatusScored:
			summary.ScoredCount++
			scoreTotals[result.Target] += result.Score
			summary.MetricScores[result.Metric] = result.Score
		case StatusSkipped:
			summary.SkippedCount++
		case StatusError:
			summary.ErrorCount++
		}
	}

	summaries := make([]ResultSummary, 0, len(grouped))
	for _, target := range []string{TargetCaptured, TargetReplay} {
		summary, ok := grouped[target]
		if !ok {
			continue
		}
		if summary.ScoredCount > 0 {
			summary.AverageScore = scoreTotals[target] / float64(summary.ScoredCount)
		}
		summaries = append(summaries, *summary)
	}

	return summaries
}

func scoreTarget(sampleID, replayRunID, target, answer string, sample tracebridge.ChatSample) []EvaluationResult {
	results := []EvaluationResult{
		buildRewriteFidelity(sampleID, replayRunID, target, sample),
		buildRetrievalPrecisionAtK(sampleID, replayRunID, target, sample),
		buildRetrievalRelevance(sampleID, replayRunID, target, answer, sample),
		buildGroundedAnswer(sampleID, replayRunID, target, answer, sample),
		buildCitationCorrectness(sampleID, replayRunID, target, answer, sample),
		buildAbstentionQuality(sampleID, replayRunID, target, answer, sample),
	}
	return results
}

func buildRewriteFidelity(sampleID, replayRunID, target string, sample tracebridge.ChatSample) EvaluationResult {
	original := strings.TrimSpace(firstNonEmpty(sample.OriginalQuery, sample.Question))
	if original == "" {
		return newResult(sampleID, replayRunID, target, "rewrite_fidelity", StatusSkipped, 0, "original query is empty")
	}

	rewritten := strings.TrimSpace(sample.RewrittenQuery)
	if rewritten == "" {
		return newResult(sampleID, replayRunID, target, "rewrite_fidelity", StatusSkipped, 0, "rewritten query is empty")
	}

	score := symmetricTokenOverlapScore(original, rewritten)
	return newResult(sampleID, replayRunID, target, "rewrite_fidelity", StatusScored, score, fmt.Sprintf("query token overlap %.2f", score))
}

func buildRetrievalPrecisionAtK(sampleID, replayRunID, target string, sample tracebridge.ChatSample) EvaluationResult {
	if len(sample.Chunks) == 0 {
		return newResult(sampleID, replayRunID, target, "retrieval_precision_at_k", StatusSkipped, 0, "no retrieved chunks captured")
	}

	query := strings.TrimSpace(firstNonEmpty(sample.RewrittenQuery, sample.OriginalQuery, sample.Question))
	if query == "" {
		return newResult(sampleID, replayRunID, target, "retrieval_precision_at_k", StatusSkipped, 0, "query is empty")
	}

	limit := retrievalPrecisionK
	if len(sample.Chunks) < limit {
		limit = len(sample.Chunks)
	}

	relevant := 0
	for i := 0; i < limit; i++ {
		if overlapScore(query, sample.Chunks[i].Content) >= queryChunkRelevantOverlapCutoff {
			relevant++
		}
	}
	score := float64(relevant) / float64(limit)
	return newResult(sampleID, replayRunID, target, "retrieval_precision_at_k", StatusScored, score, fmt.Sprintf("%d/%d chunks above overlap %.2f", relevant, limit, queryChunkRelevantOverlapCutoff))
}

func buildRetrievalRelevance(sampleID, replayRunID, target, answer string, sample tracebridge.ChatSample) EvaluationResult {
	if len(sample.Chunks) == 0 {
		return newResult(sampleID, replayRunID, target, "retrieval_relevance", StatusSkipped, 0, "no retrieved chunks captured")
	}
	score := overlapScore(answer, chunkCorpus(sample.Chunks))
	return newResult(sampleID, replayRunID, target, "retrieval_relevance", StatusScored, score, fmt.Sprintf("answer/chunk overlap %.2f", score))
}

func buildGroundedAnswer(sampleID, replayRunID, target, answer string, sample tracebridge.ChatSample) EvaluationResult {
	if strings.TrimSpace(answer) == "" {
		return newResult(sampleID, replayRunID, target, "grounded_answer", StatusSkipped, 0, "answer is empty")
	}
	if len(sample.Chunks) == 0 {
		return newResult(sampleID, replayRunID, target, "grounded_answer", StatusSkipped, 0, "no retrieved chunks captured")
	}
	score := overlapScore(answer, chunkCorpus(sample.Chunks))
	return newResult(sampleID, replayRunID, target, "grounded_answer", StatusScored, score, fmt.Sprintf("grounded overlap %.2f", score))
}

func buildCitationCorrectness(sampleID, replayRunID, target, answer string, sample tracebridge.ChatSample) EvaluationResult {
	citationRe := regexp.MustCompile(`\[(\d+)\]`)
	matches := citationRe.FindAllStringSubmatch(answer, -1)
	if len(matches) == 0 {
		return newResult(sampleID, replayRunID, target, "citation_correctness", StatusSkipped, 0, "answer contains no citations")
	}

	valid := 0
	for _, match := range matches {
		index := parsePositiveInt(match[1]) - 1
		if index >= 0 && index < len(sample.Chunks) {
			valid++
		}
	}
	score := float64(valid) / float64(len(matches))
	return newResult(sampleID, replayRunID, target, "citation_correctness", StatusScored, score, fmt.Sprintf("%d/%d citations map to retrieved chunks", valid, len(matches)))
}

func buildAbstentionQuality(sampleID, replayRunID, target, answer string, sample tracebridge.ChatSample) EvaluationResult {
	lower := strings.ToLower(answer)
	abstained := strings.Contains(lower, "not supported") ||
		strings.Contains(lower, "无法根据") ||
		strings.Contains(lower, "不知道") ||
		strings.Contains(lower, "不清楚")

	if abstained {
		score := 1.0
		if len(sample.Chunks) > 0 {
			score = 0.5
		}
		return newResult(sampleID, replayRunID, target, "abstention_quality", StatusScored, score, "answer appears to abstain")
	}

	return newResult(sampleID, replayRunID, target, "abstention_quality", StatusScored, 1.0, "answer does not abstain")
}

func newResult(sampleID, replayRunID, target, metric, status string, score float64, summary string) EvaluationResult {
	return EvaluationResult{
		SampleID:    sampleID,
		ReplayRunID: replayRunID,
		Target:      target,
		Metric:      metric,
		Status:      status,
		Score:       score,
		Summary:     summary,
	}
}

func chunkCorpus(chunks []tracebridge.RetrievedChunk) string {
	parts := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		parts = append(parts, chunk.Content)
	}

	return strings.Join(parts, "\n")
}

func overlapScore(answer, corpus string) float64 {
	answerTokens := tokenSet(answer)
	if len(answerTokens) == 0 {
		return 0
	}
	corpusTokens := tokenSet(corpus)
	matched := 0
	for token := range answerTokens {
		if _, ok := corpusTokens[token]; ok {
			matched++
		}
	}
	return float64(matched) / float64(len(answerTokens))
}

func symmetricTokenOverlapScore(left, right string) float64 {
	leftTokens := tokenSet(left)
	rightTokens := tokenSet(right)
	if len(leftTokens) == 0 || len(rightTokens) == 0 {
		return 0
	}

	matched := 0
	for token := range leftTokens {
		if _, ok := rightTokens[token]; ok {
			matched++
		}
	}

	return float64(2*matched) / float64(len(leftTokens)+len(rightTokens))
}

func tokenSet(text string) map[string]struct{} {
	text = strings.ToLower(text)
	replacer := strings.NewReplacer(",", " ", ".", " ", "。", " ", "，", " ", "\n", " ", "\t", " ")
	text = replacer.Replace(text)
	parts := strings.Fields(text)
	set := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		set[part] = struct{}{}
	}
	return set
}

func parsePositiveInt(text string) int {
	result := 0
	for _, r := range text {
		if r < '0' || r > '9' {
			return 0
		}
		result = result*10 + int(r-'0')
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}
