package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/dianwang-mac/go-rag/internal/config"
	"github.com/dianwang-mac/go-rag/internal/eval"
	"github.com/dianwang-mac/go-rag/internal/llm"
	"github.com/dianwang-mac/go-rag/internal/phoenix"
	"github.com/dianwang-mac/go-rag/internal/store"
	"github.com/dianwang-mac/go-rag/internal/tracebridge"
	"gorm.io/gorm"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: evalctl <export-trace <trace_id> | replay-sample <sample_id> | score-sample <sample_id> | run-trace <trace_id> | compare-samples <sample_id...>>")
	}

	switch args[0] {
	case "export-trace":
		if len(args) < 2 || args[1] == "" {
			return fmt.Errorf("export-trace requires a trace id")
		}
		return exportTrace(args[1], stdout)
	case "replay-sample":
		if len(args) < 2 || args[1] == "" {
			return fmt.Errorf("replay-sample requires a sample id")
		}
		return replaySample(args[1], stdout)
	case "score-sample":
		if len(args) < 2 || args[1] == "" {
			return fmt.Errorf("score-sample requires a sample id")
		}
		return scoreSample(args[1], stdout)
	case "run-trace":
		if len(args) < 2 || args[1] == "" {
			return fmt.Errorf("run-trace requires a trace id")
		}
		return runTrace(args[1], stdout)
	case "compare-samples":
		sampleIDs := parseSampleIDs(args[1:])
		if len(sampleIDs) == 0 {
			return fmt.Errorf("compare-samples requires at least one sample id")
		}
		return compareSamples(sampleIDs, stdout)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

type comparisonPayload struct {
	SampleIDs    []string              `json:"sample_ids"`
	FocusMetrics []string              `json:"focus_metrics"`
	BySample     []sampleComparison    `json:"by_sample"`
	Aggregate    []aggregateComparison `json:"aggregate"`
}

type sampleComparison struct {
	SampleID       string                              `json:"sample_id"`
	Question       string                              `json:"question"`
	OriginalQuery  string                              `json:"original_query,omitempty"`
	RewrittenQuery string                              `json:"rewritten_query,omitempty"`
	ChunkCount     int                                 `json:"chunk_count"`
	Scores         map[string]map[string]metricOutcome `json:"scores"`
}

type metricOutcome struct {
	Status  string  `json:"status"`
	Score   float64 `json:"score"`
	Summary string  `json:"summary,omitempty"`
}

type aggregateComparison struct {
	Target       string  `json:"target"`
	Metric       string  `json:"metric"`
	Count        int     `json:"count"`
	AverageScore float64 `json:"average_score"`
}

var focusMetrics = []string{"retrieval_precision_at_k", "grounded_answer"}

func exportTrace(traceID string, stdout io.Writer) error {
	db, err := openEvalDB()
	if err != nil {
		return err
	}
	repo := eval.NewRepository(db)

	cfg, err := phoenix.ConfigFromEnv()
	if err != nil {
		return err
	}

	client, err := phoenix.NewClient(cfg)
	if err != nil {
		return err
	}

	trace, err := client.FetchTrace(context.Background(), traceID)
	if err != nil {
		return err
	}

	sample, warnings, err := tracebridge.NormalizeChatTrace(trace)
	if err != nil {
		return err
	}
	stored, err := repo.SaveSample(eval.StoredSample{
		Sample:   sample,
		Warnings: warnings,
	})
	if err != nil {
		return err
	}

	payload := struct {
		SampleID string                      `json:"sample_id"`
		Sample   tracebridge.ChatSample      `json:"sample"`
		Warnings []tracebridge.ExportWarning `json:"warnings,omitempty"`
	}{
		SampleID: stored.SampleID,
		Sample:   stored.Sample,
		Warnings: stored.Warnings,
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func replaySample(sampleID string, stdout io.Writer) error {
	db, err := openEvalDB()
	if err != nil {
		return err
	}
	repo := eval.NewRepository(db)

	stored, err := repo.GetSample(sampleID)
	if err != nil {
		return err
	}

	providerCfg, err := replayProvider()
	if err != nil {
		return err
	}

	run, err := eval.ReplayChatSample(context.Background(), providerCfg, stored)
	if err != nil {
		return err
	}
	run, err = repo.SaveReplayRun(run)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(run)
}

func scoreSample(sampleID string, stdout io.Writer) error {
	db, err := openEvalDB()
	if err != nil {
		return err
	}
	repo := eval.NewRepository(db)

	stored, err := repo.GetSample(sampleID)
	if err != nil {
		return err
	}
	replay, err := repo.GetLatestReplayRun(sampleID)
	if err != nil {
		return err
	}

	results := eval.ScoreChatSample(stored, replay)
	if err := repo.SaveEvaluationResults(results); err != nil {
		return err
	}

	payload := struct {
		Results  []eval.EvaluationResult `json:"results"`
		Summary  []eval.ResultSummary    `json:"summary"`
		ReplayID string                  `json:"replay_run_id,omitempty"`
	}{
		Results: results,
		Summary: eval.SummarizeResults(results),
	}
	if replay != nil {
		payload.ReplayID = replay.ReplayRunID
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func runTrace(traceID string, stdout io.Writer) error {
	db, err := openEvalDB()
	if err != nil {
		return err
	}
	repo := eval.NewRepository(db)

	cfg, err := phoenix.ConfigFromEnv()
	if err != nil {
		return err
	}
	client, err := phoenix.NewClient(cfg)
	if err != nil {
		return err
	}

	trace, err := client.FetchTrace(context.Background(), traceID)
	if err != nil {
		return err
	}

	sample, warnings, err := tracebridge.NormalizeChatTrace(trace)
	if err != nil {
		return err
	}
	stored, err := repo.SaveSample(eval.StoredSample{
		Sample:   sample,
		Warnings: warnings,
	})
	if err != nil {
		return err
	}

	providerCfg, err := replayProvider()
	if err != nil {
		return err
	}
	replay, err := eval.ReplayChatSample(context.Background(), providerCfg, stored)
	if err != nil {
		return err
	}
	replay, err = repo.SaveReplayRun(replay)
	if err != nil {
		return err
	}

	results := eval.ScoreChatSample(stored, &replay)
	if err := repo.SaveEvaluationResults(results); err != nil {
		return err
	}

	payload := struct {
		SampleID string                      `json:"sample_id"`
		Replay   eval.ReplayRun              `json:"replay"`
		Warnings []tracebridge.ExportWarning `json:"warnings,omitempty"`
		Results  []eval.EvaluationResult     `json:"results"`
		Summary  []eval.ResultSummary        `json:"summary"`
	}{
		SampleID: stored.SampleID,
		Replay:   replay,
		Warnings: stored.Warnings,
		Results:  results,
		Summary:  eval.SummarizeResults(results),
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func compareSamples(sampleIDs []string, stdout io.Writer) error {
	db, err := openEvalDB()
	if err != nil {
		return err
	}
	repo := eval.NewRepository(db)

	samples, err := repo.GetSamples(sampleIDs)
	if err != nil {
		return err
	}

	missing := make([]string, 0)
	for _, id := range sampleIDs {
		if _, ok := samples[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("samples not found: %s", strings.Join(missing, ", "))
	}

	results, err := repo.GetLatestEvaluationResults(sampleIDs)
	if err != nil {
		return err
	}

	payload := buildComparisonPayload(sampleIDs, samples, results, focusMetrics)
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func buildComparisonPayload(
	sampleIDs []string,
	samples map[string]eval.StoredSample,
	results []eval.EvaluationResult,
	metrics []string,
) comparisonPayload {
	metricSet := make(map[string]struct{}, len(metrics))
	for _, metric := range metrics {
		metricSet[metric] = struct{}{}
	}

	byID := make(map[string]*sampleComparison, len(sampleIDs))
	bySample := make([]sampleComparison, 0, len(sampleIDs))
	for _, sampleID := range sampleIDs {
		stored := samples[sampleID]
		item := sampleComparison{
			SampleID:       sampleID,
			Question:       stored.Sample.Question,
			OriginalQuery:  stored.Sample.OriginalQuery,
			RewrittenQuery: stored.Sample.RewrittenQuery,
			ChunkCount:     len(stored.Sample.Chunks),
			Scores:         map[string]map[string]metricOutcome{},
		}
		bySample = append(bySample, item)
		byID[sampleID] = &bySample[len(bySample)-1]
	}

	type agg struct {
		total float64
		count int
	}
	aggMap := map[string]*agg{}
	for _, result := range results {
		if _, ok := metricSet[result.Metric]; !ok {
			continue
		}
		sample, ok := byID[result.SampleID]
		if !ok {
			continue
		}
		if _, ok := sample.Scores[result.Target]; !ok {
			sample.Scores[result.Target] = map[string]metricOutcome{}
		}
		sample.Scores[result.Target][result.Metric] = metricOutcome{
			Status:  result.Status,
			Score:   result.Score,
			Summary: result.Summary,
		}

		if result.Status != eval.StatusScored {
			continue
		}
		key := result.Target + "|" + result.Metric
		if _, ok := aggMap[key]; !ok {
			aggMap[key] = &agg{}
		}
		aggMap[key].total += result.Score
		aggMap[key].count++
	}

	aggregate := make([]aggregateComparison, 0, len(aggMap))
	targetOrder := []string{eval.TargetCaptured, eval.TargetReplay}
	for _, target := range targetOrder {
		for _, metric := range metrics {
			key := target + "|" + metric
			entry, ok := aggMap[key]
			if !ok {
				continue
			}
			average := 0.0
			if entry.count > 0 {
				average = entry.total / float64(entry.count)
			}
			aggregate = append(aggregate, aggregateComparison{
				Target:       target,
				Metric:       metric,
				Count:        entry.count,
				AverageScore: average,
			})
		}
	}

	sort.SliceStable(aggregate, func(i, j int) bool {
		if aggregate[i].Target == aggregate[j].Target {
			return aggregate[i].Metric < aggregate[j].Metric
		}
		return aggregate[i].Target < aggregate[j].Target
	})

	return comparisonPayload{
		SampleIDs:    sampleIDs,
		FocusMetrics: metrics,
		BySample:     bySample,
		Aggregate:    aggregate,
	}
}

func parseSampleIDs(args []string) []string {
	seen := map[string]struct{}{}
	ids := make([]string, 0, len(args))
	for _, arg := range args {
		for _, part := range strings.Split(arg, ",") {
			id := strings.TrimSpace(part)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return ids
}

func openEvalDB() (*gorm.DB, error) {
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		return nil, fmt.Errorf("MYSQL_DSN is required")
	}
	return store.OpenMySQL(dsn)
}

func replayProvider() (*llm.Provider, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required for replay")
	}

	chatCfg := config.ChatConfig{
		BaseURL: os.Getenv("OPENAI_BASE_URL"),
		APIKey:  apiKey,
		Model:   firstNonEmpty(os.Getenv("OPENAI_CHAT_MODEL"), "gpt-4o-mini"),
	}
	embeddingCfg := config.EmbeddingConfig{
		BaseURL: firstNonEmpty(os.Getenv("EMBEDDING_BASE_URL"), os.Getenv("OPENAI_BASE_URL")),
		APIKey:  firstNonEmpty(os.Getenv("EMBEDDING_API_KEY"), apiKey),
		Model:   firstNonEmpty(os.Getenv("EMBEDDING_MODEL"), os.Getenv("OPENAI_EMBEDDING_MODEL"), "bge-m3"),
	}

	return llm.NewProvider(chatCfg, embeddingCfg), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}
