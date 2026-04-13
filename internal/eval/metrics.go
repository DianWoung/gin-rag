package eval

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/dianwang-mac/go-rag/internal/appdto"
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
	tableCellCoverageCutoff         = 0.1
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
	results := scoreTarget(stored.SampleID, "", TargetCaptured, stored.Sample.Answer, stored.AgentSteps, stored.Sample)
	if replay != nil {
		results = append(results, scoreTarget(stored.SampleID, replay.ReplayRunID, TargetReplay, replay.Answer, replay.AgentSteps, stored.Sample)...)
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

func scoreTarget(sampleID, replayRunID, target, answer string, agentSteps []appdto.AgentStep, sample tracebridge.ChatSample) []EvaluationResult {
	results := []EvaluationResult{
		buildRewriteFidelity(sampleID, replayRunID, target, sample),
		buildRetrievalPrecisionAtK(sampleID, replayRunID, target, sample),
		buildTableCellAccuracy(sampleID, replayRunID, target, answer, sample),
		buildRetrievalRelevance(sampleID, replayRunID, target, answer, sample),
		buildGroundedAnswer(sampleID, replayRunID, target, answer, sample),
		buildCitationCorrectness(sampleID, replayRunID, target, answer, sample),
		buildAbstentionQuality(sampleID, replayRunID, target, answer, sample),
		buildAgentStepEfficiency(sampleID, replayRunID, target, answer, agentSteps),
		buildAgentInvalidSearchRatio(sampleID, replayRunID, target, answer, agentSteps),
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

func buildTableCellAccuracy(sampleID, replayRunID, target, answer string, sample tracebridge.ChatSample) EvaluationResult {
	if strings.TrimSpace(answer) == "" {
		return newResult(sampleID, replayRunID, target, "table_cell_accuracy", StatusSkipped, 0, "answer is empty")
	}

	cells := extractTableCellTokens(sample.Chunks)
	if len(cells) == 0 {
		return newResult(sampleID, replayRunID, target, "table_cell_accuracy", StatusSkipped, 0, "no table-like chunks captured")
	}

	answerTokens := tokenSet(answer)
	if len(answerTokens) == 0 {
		return newResult(sampleID, replayRunID, target, "table_cell_accuracy", StatusSkipped, 0, "answer has no comparable tokens")
	}

	covered := 0
	for token := range answerTokens {
		if _, ok := cells[token]; ok {
			covered++
		}
	}
	score := float64(covered) / float64(len(answerTokens))
	if score < tableCellCoverageCutoff {
		return newResult(sampleID, replayRunID, target, "table_cell_accuracy", StatusScored, score, fmt.Sprintf("answer/cell overlap %.2f below cutoff %.2f", score, tableCellCoverageCutoff))
	}
	return newResult(sampleID, replayRunID, target, "table_cell_accuracy", StatusScored, score, fmt.Sprintf("answer/cell overlap %.2f", score))
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

func buildAgentStepEfficiency(sampleID, replayRunID, target, answer string, agentSteps []appdto.AgentStep) EvaluationResult {
	totalSteps, _ := parseAgentStepStats(answer, agentSteps)
	if totalSteps == 0 {
		return newResult(sampleID, replayRunID, target, "agent_step_efficiency", StatusSkipped, 0, "agent step markers not found")
	}
	score := 1.0 / float64(totalSteps)
	return newResult(sampleID, replayRunID, target, "agent_step_efficiency", StatusScored, score, fmt.Sprintf("total agent steps %d", totalSteps))
}

func buildAgentInvalidSearchRatio(sampleID, replayRunID, target, answer string, agentSteps []appdto.AgentStep) EvaluationResult {
	totalSteps, invalidSteps := parseAgentStepStats(answer, agentSteps)
	if totalSteps == 0 {
		return newResult(sampleID, replayRunID, target, "agent_invalid_search_ratio", StatusSkipped, 0, "agent step markers not found")
	}
	score := float64(invalidSteps) / float64(totalSteps)
	return newResult(sampleID, replayRunID, target, "agent_invalid_search_ratio", StatusScored, score, fmt.Sprintf("%d/%d steps retrieved zero chunks", invalidSteps, totalSteps))
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

func extractTableCellTokens(chunks []tracebridge.RetrievedChunk) map[string]struct{} {
	set := map[string]struct{}{}
	for _, chunk := range chunks {
		lines := splitNonEmptyLines(chunk.Content)
		if !looksTableLike(lines) {
			continue
		}
		for _, line := range lines {
			for _, col := range splitTableColumns(line) {
				for token := range tokenSet(col) {
					set[token] = struct{}{}
				}
			}
		}
	}
	return set
}

func looksTableLike(lines []string) bool {
	if len(lines) < 2 {
		return false
	}
	total := 0
	valid := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		total++
		if len(splitTableColumns(line)) >= 2 {
			valid++
		}
	}
	if total < 2 || valid < 2 {
		return false
	}
	return float64(valid)/float64(total) >= 0.6
}

func splitTableColumns(line string) []string {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	if strings.Contains(line, "|") {
		raw := strings.Split(line, "|")
		return trimNonEmpty(raw)
	}
	if strings.Contains(line, "\t") {
		raw := strings.Split(line, "\t")
		return trimNonEmpty(raw)
	}
	re := regexp.MustCompile(`\s{2,}`)
	raw := re.Split(line, -1)
	return trimNonEmpty(raw)
}

func splitNonEmptyLines(text string) []string {
	raw := strings.Split(text, "\n")
	return trimNonEmpty(raw)
}

func trimNonEmpty(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}

func parseAgentStepStats(answer string, agentSteps []appdto.AgentStep) (totalSteps int, invalidSteps int) {
	if len(agentSteps) > 0 {
		return agentStepStats(agentSteps)
	}
	return agentStepStats(extractAgentSteps(answer))
}
