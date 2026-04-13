package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/dianwang-mac/go-rag/internal/appdto"
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
		return fmt.Errorf("usage: evalctl <export-trace <trace_id> | replay-sample <sample_id> | score-sample <sample_id> | run-trace <trace_id> | compare-samples <sample_id...> | annotate-sample <sample_id> --reviewer <name> --retrieval <0-1> --grounded <0-1> --citation <0-1> --abstention <0-1>>")
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
	case "annotate-sample":
		return annotateSample(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

type comparisonPayload struct {
	SampleIDs       []string              `json:"sample_ids"`
	FocusMetrics    []string              `json:"focus_metrics"`
	BySample        []sampleComparison    `json:"by_sample"`
	Aggregate       []aggregateComparison `json:"aggregate"`
	ManualAggregate manualAggregate       `json:"manual_aggregate"`
}

type sampleComparison struct {
	SampleID          string                              `json:"sample_id"`
	Question          string                              `json:"question"`
	OriginalQuery     string                              `json:"original_query,omitempty"`
	RewrittenQuery    string                              `json:"rewritten_query,omitempty"`
	ChunkCount        int                                 `json:"chunk_count"`
	AgentTrace        *agentTraceSummary                  `json:"agent_trace,omitempty"`
	Scores            map[string]map[string]metricOutcome `json:"scores"`
	ManualCount       int                                 `json:"manual_count"`
	ManualMetricScore map[string]float64                  `json:"manual_metric_scores,omitempty"`
}

type agentTraceSummary struct {
	StepCount          int     `json:"step_count"`
	InvalidStepCount   int     `json:"invalid_step_count"`
	InvalidSearchRatio float64 `json:"invalid_search_ratio"`
}

type scoreAgentTraceSummary struct {
	Captured *agentTraceSummary `json:"captured,omitempty"`
	Replay   *agentTraceSummary `json:"replay,omitempty"`
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

type manualAggregate struct {
	AnnotationCount int                `json:"annotation_count"`
	MetricScores    map[string]float64 `json:"metric_scores,omitempty"`
}

type metricAgg struct {
	count int
	total float64
}

var focusMetrics = []string{
	"retrieval_precision_at_k",
	"grounded_answer",
	"table_cell_accuracy",
	"agent_step_efficiency",
	"agent_invalid_search_ratio",
}

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
		Results           []eval.EvaluationResult   `json:"results"`
		Summary           []eval.ResultSummary      `json:"summary"`
		ReplayID          string                    `json:"replay_run_id,omitempty"`
		AgentTraceSummary *scoreAgentTraceSummary   `json:"agent_trace_summary,omitempty"`
	}{
		Results:           results,
		Summary:           eval.SummarizeResults(results),
		AgentTraceSummary: buildScoreAgentTraceSummary(stored, replay),
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
		SampleID           string                    `json:"sample_id"`
		Replay             eval.ReplayRun            `json:"replay"`
		Warnings           []tracebridge.ExportWarning `json:"warnings,omitempty"`
		Results            []eval.EvaluationResult   `json:"results"`
		Summary            []eval.ResultSummary      `json:"summary"`
		AgentTraceSummary  *scoreAgentTraceSummary   `json:"agent_trace_summary,omitempty"`
	}{
		SampleID:          stored.SampleID,
		Replay:            replay,
		Warnings:          stored.Warnings,
		Results:           results,
		Summary:           eval.SummarizeResults(results),
		AgentTraceSummary: buildScoreAgentTraceSummary(stored, &replay),
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
	replayRuns := map[string]*eval.ReplayRun{}
	for _, sampleID := range sampleIDs {
		run, runErr := repo.GetLatestReplayRun(sampleID)
		if runErr != nil {
			return runErr
		}
		replayRuns[sampleID] = run
	}
	annotations, err := repo.ListManualAnnotations(sampleIDs)
	if err != nil {
		return err
	}

	payload := buildComparisonPayload(sampleIDs, samples, replayRuns, results, annotations, focusMetrics)
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func buildComparisonPayload(
	sampleIDs []string,
	samples map[string]eval.StoredSample,
	replayRuns map[string]*eval.ReplayRun,
	results []eval.EvaluationResult,
	annotations []eval.ManualAnnotation,
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
			SampleID:          sampleID,
			Question:          stored.Sample.Question,
			OriginalQuery:     stored.Sample.OriginalQuery,
			RewrittenQuery:    stored.Sample.RewrittenQuery,
			ChunkCount:        len(stored.Sample.Chunks),
			AgentTrace:        summarizeAgentTrace(stored.AgentSteps, replayRuns[sampleID]),
			Scores:            map[string]map[string]metricOutcome{},
			ManualMetricScore: map[string]float64{},
		}
		bySample = append(bySample, item)
		byID[sampleID] = &bySample[len(bySample)-1]
	}

	aggMap := map[string]*metricAgg{}
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
			aggMap[key] = &metricAgg{}
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

	manual := buildManualAggregate(byID, annotations)

	return comparisonPayload{
		SampleIDs:       sampleIDs,
		FocusMetrics:    metrics,
		BySample:        bySample,
		Aggregate:       aggregate,
		ManualAggregate: manual,
	}
}

func summarizeAgentTrace(capturedSteps []appdto.AgentStep, replayRun *eval.ReplayRun) *agentTraceSummary {
	steps := capturedSteps
	if replayRun != nil && len(replayRun.AgentSteps) > 0 {
		steps = replayRun.AgentSteps
	}
	if len(steps) == 0 {
		return nil
	}

	invalid := 0
	for _, step := range steps {
		if step.RetrievedCount == 0 {
			invalid++
		}
	}

	return &agentTraceSummary{
		StepCount:          len(steps),
		InvalidStepCount:   invalid,
		InvalidSearchRatio: safeAverage(float64(invalid), len(steps)),
	}
}

func buildScoreAgentTraceSummary(stored eval.StoredSample, replayRun *eval.ReplayRun) *scoreAgentTraceSummary {
	captured := summarizeAgentTrace(stored.AgentSteps, nil)
	replay := summarizeAgentTrace(nil, replayRun)
	if captured == nil && replay == nil {
		return nil
	}
	return &scoreAgentTraceSummary{
		Captured: captured,
		Replay:   replay,
	}
}

func buildManualAggregate(byID map[string]*sampleComparison, annotations []eval.ManualAnnotation) manualAggregate {
	sampleMetricAgg := map[string]map[string]*metricAgg{}
	globalMetricAgg := map[string]*metricAgg{}
	annotationCount := 0

	for _, annotation := range annotations {
		sample, ok := byID[annotation.SampleID]
		if !ok {
			continue
		}
		annotationCount++
		sample.ManualCount++
		if _, ok := sampleMetricAgg[annotation.SampleID]; !ok {
			sampleMetricAgg[annotation.SampleID] = map[string]*metricAgg{}
		}

		addMetric(sampleMetricAgg[annotation.SampleID], "retrieval_relevance", annotation.RetrievalRelevance)
		addMetric(sampleMetricAgg[annotation.SampleID], "grounded_answer", annotation.GroundedAnswer)
		addMetric(sampleMetricAgg[annotation.SampleID], "citation_correctness", annotation.CitationCorrectness)
		addMetric(sampleMetricAgg[annotation.SampleID], "abstention_quality", annotation.AbstentionQuality)

		addMetric(globalMetricAgg, "retrieval_relevance", annotation.RetrievalRelevance)
		addMetric(globalMetricAgg, "grounded_answer", annotation.GroundedAnswer)
		addMetric(globalMetricAgg, "citation_correctness", annotation.CitationCorrectness)
		addMetric(globalMetricAgg, "abstention_quality", annotation.AbstentionQuality)
	}

	for sampleID, metricAgg := range sampleMetricAgg {
		sample := byID[sampleID]
		for metric, entry := range metricAgg {
			sample.ManualMetricScore[metric] = safeAverage(entry.total, entry.count)
		}
	}

	manualScores := map[string]float64{}
	for metric, entry := range globalMetricAgg {
		manualScores[metric] = safeAverage(entry.total, entry.count)
	}

	return manualAggregate{
		AnnotationCount: annotationCount,
		MetricScores:    manualScores,
	}
}

func addMetric(container map[string]*metricAgg, metric string, value float64) {
	entry, ok := container[metric]
	if !ok {
		entry = &metricAgg{}
		container[metric] = entry
	}
	entry.total += value
	entry.count++
}

func safeAverage(total float64, count int) float64 {
	if count == 0 {
		return 0
	}
	return total / float64(count)
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

func annotateSample(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf("annotate-sample requires a sample id")
	}
	sampleID := strings.TrimSpace(args[0])

	flagSet := flag.NewFlagSet("annotate-sample", flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	var reviewer string
	var retrieval float64
	var grounded float64
	var citation float64
	var abstention float64
	var notes string
	flagSet.StringVar(&reviewer, "reviewer", "", "reviewer name")
	flagSet.Float64Var(&retrieval, "retrieval", -1, "retrieval relevance score [0,1]")
	flagSet.Float64Var(&grounded, "grounded", -1, "grounded answer score [0,1]")
	flagSet.Float64Var(&citation, "citation", -1, "citation correctness score [0,1]")
	flagSet.Float64Var(&abstention, "abstention", -1, "abstention quality score [0,1]")
	flagSet.StringVar(&notes, "notes", "", "annotation notes")
	if err := flagSet.Parse(args[1:]); err != nil {
		return err
	}

	reviewer = strings.TrimSpace(reviewer)
	if reviewer == "" {
		return fmt.Errorf("annotate-sample requires --reviewer")
	}
	if err := validateScore("retrieval", retrieval); err != nil {
		return err
	}
	if err := validateScore("grounded", grounded); err != nil {
		return err
	}
	if err := validateScore("citation", citation); err != nil {
		return err
	}
	if err := validateScore("abstention", abstention); err != nil {
		return err
	}

	db, err := openEvalDB()
	if err != nil {
		return err
	}
	repo := eval.NewRepository(db)
	if _, err := repo.GetSample(sampleID); err != nil {
		return fmt.Errorf("sample %s not found: %w", sampleID, err)
	}

	annotation, err := repo.SaveManualAnnotation(eval.ManualAnnotation{
		SampleID:            sampleID,
		Reviewer:            reviewer,
		RetrievalRelevance:  retrieval,
		GroundedAnswer:      grounded,
		CitationCorrectness: citation,
		AbstentionQuality:   abstention,
		Notes:               strings.TrimSpace(notes),
	})
	if err != nil {
		return err
	}

	payload := struct {
		Annotation eval.ManualAnnotation `json:"annotation"`
	}{
		Annotation: annotation,
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func validateScore(name string, score float64) error {
	if score < 0 || score > 1 {
		return fmt.Errorf("--%s must be within [0,1]", name)
	}
	return nil
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
