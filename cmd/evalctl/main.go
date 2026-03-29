package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

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
		return fmt.Errorf("usage: evalctl <export-trace <trace_id> | replay-sample <sample_id> | score-sample <sample_id> | run-trace <trace_id>>")
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
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
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
