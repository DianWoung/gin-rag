package eval

import (
	"testing"

	"github.com/dianwang-mac/go-rag/internal/appdto"
	"github.com/dianwang-mac/go-rag/internal/tracebridge"
)

func TestCitationCorrectnessSkipsWhenChunkReferenceMissing(t *testing.T) {
	stored := StoredSample{
		SampleID: "sample-1",
		Sample: tracebridge.ChatSample{
			Answer: "this answer has no explicit citation",
		},
	}

	results := ScoreChatSample(stored, nil)
	for _, result := range results {
		if result.Metric != "citation_correctness" {
			continue
		}
		if result.Status != StatusSkipped {
			t.Fatalf("Status = %q, want skipped", result.Status)
		}
		return
	}
	t.Fatal("citation_correctness result not found")
}

func TestGroundedAnswerScoresCapturedAndReplayTargetsSeparately(t *testing.T) {
	stored := StoredSample{
		SampleID: "sample-1",
		Sample: tracebridge.ChatSample{
			Question:       "what is rag retrieval generation",
			OriginalQuery:  "what is rag retrieval generation",
			RewrittenQuery: "what is rag retrieval generation",
			Answer:         "rag retrieval generation",
			Chunks: []tracebridge.RetrievedChunk{
				{Index: 0, Content: "rag retrieval generation"},
			},
		},
	}
	replay := &ReplayRun{
		ReplayRunID: "replay-1",
		Answer:      "rag retrieval",
		Status:      "completed",
	}

	results := ScoreChatSample(stored, replay)
	captured := 0
	replayed := 0
	for _, result := range results {
		if result.Metric != "grounded_answer" {
			continue
		}
		switch result.Target {
		case TargetCaptured:
			captured++
		case TargetReplay:
			replayed++
		}
	}
	if captured != 1 || replayed != 1 {
		t.Fatalf("captured=%d replayed=%d, want 1 and 1", captured, replayed)
	}
}

func TestRewriteFidelityUsesOriginalAndRewrittenQueries(t *testing.T) {
	stored := StoredSample{
		SampleID: "sample-2",
		Sample: tracebridge.ChatSample{
			Question:       "what is rag",
			OriginalQuery:  "what is rag",
			RewrittenQuery: "what is retrieval augmented generation",
		},
	}

	results := ScoreChatSample(stored, nil)
	for _, result := range results {
		if result.Metric != "rewrite_fidelity" {
			continue
		}
		if result.Status != StatusScored {
			t.Fatalf("Status = %q, want scored", result.Status)
		}
		if result.Score <= 0 || result.Score >= 1 {
			t.Fatalf("Score = %.2f, want between 0 and 1", result.Score)
		}
		return
	}
	t.Fatal("rewrite_fidelity result not found")
}

func TestRetrievalPrecisionAtKSkipsWithoutChunks(t *testing.T) {
	stored := StoredSample{
		SampleID: "sample-3",
		Sample: tracebridge.ChatSample{
			Question:       "what is rag",
			OriginalQuery:  "what is rag",
			RewrittenQuery: "what is rag",
		},
	}

	results := ScoreChatSample(stored, nil)
	for _, result := range results {
		if result.Metric != "retrieval_precision_at_k" {
			continue
		}
		if result.Status != StatusSkipped {
			t.Fatalf("Status = %q, want skipped", result.Status)
		}
		return
	}
	t.Fatal("retrieval_precision_at_k result not found")
}

func TestTableCellAccuracyScoresWhenTableLikeChunksExist(t *testing.T) {
	stored := StoredSample{
		SampleID: "sample-4",
		Sample: tracebridge.ChatSample{
			Answer: "A 10 B 12",
			Chunks: []tracebridge.RetrievedChunk{
				{Index: 0, Content: "item  price\nA     10\nB     12"},
			},
		},
	}

	results := ScoreChatSample(stored, nil)
	for _, result := range results {
		if result.Metric != "table_cell_accuracy" {
			continue
		}
		if result.Status != StatusScored {
			t.Fatalf("Status = %q, want scored", result.Status)
		}
		if result.Score <= 0 {
			t.Fatalf("Score = %.2f, want > 0", result.Score)
		}
		return
	}
	t.Fatal("table_cell_accuracy result not found")
}

func TestSummarizeResultsAggregatesByTarget(t *testing.T) {
	results := []EvaluationResult{
		{Target: TargetCaptured, Metric: "grounded_answer", Status: StatusScored, Score: 1},
		{Target: TargetCaptured, Metric: "citation_correctness", Status: StatusSkipped, Score: 0},
		{Target: TargetReplay, Metric: "grounded_answer", Status: StatusScored, Score: 0.5},
		{Target: TargetReplay, Metric: "retrieval_relevance", Status: StatusScored, Score: 0.25},
	}

	summaries := SummarizeResults(results)
	if len(summaries) != 2 {
		t.Fatalf("len(summaries)=%d, want 2", len(summaries))
	}
	if summaries[0].Target != TargetCaptured || summaries[0].AverageScore != 1 {
		t.Fatalf("captured summary = %+v", summaries[0])
	}
	if summaries[1].Target != TargetReplay || summaries[1].AverageScore != 0.375 {
		t.Fatalf("replay summary = %+v", summaries[1])
	}
}

func TestAgentMetricsScoreFromStepMarkers(t *testing.T) {
	answer := "[agent] step 1/3 query=\"a\" retrieved=2\n[agent] step 2/3 query=\"b\" retrieved=0\nfinal answer"
	stored := StoredSample{
		SampleID: "sample-agent-1",
		Sample: tracebridge.ChatSample{
			Answer: answer,
		},
	}

	results := ScoreChatSample(stored, nil)

	var efficiency EvaluationResult
	var invalidRatio EvaluationResult
	for _, result := range results {
		if result.Metric == "agent_step_efficiency" {
			efficiency = result
		}
		if result.Metric == "agent_invalid_search_ratio" {
			invalidRatio = result
		}
	}

	if efficiency.Status != StatusScored {
		t.Fatalf("efficiency status = %q, want scored", efficiency.Status)
	}
	if efficiency.Score != 0.5 {
		t.Fatalf("efficiency score = %.2f, want 0.50", efficiency.Score)
	}
	if invalidRatio.Status != StatusScored {
		t.Fatalf("invalid ratio status = %q, want scored", invalidRatio.Status)
	}
	if invalidRatio.Score != 0.5 {
		t.Fatalf("invalid ratio score = %.2f, want 0.50", invalidRatio.Score)
	}
}

func TestAgentMetricsPreferStructuredSteps(t *testing.T) {
	stored := StoredSample{
		SampleID: "sample-agent-2",
		Sample: tracebridge.ChatSample{
			Answer: "final answer only",
		},
		AgentSteps: []appdto.AgentStep{
			{Step: 1, RetrievedCount: 3},
			{Step: 2, RetrievedCount: 0},
			{Step: 3, RetrievedCount: 1},
		},
	}

	results := ScoreChatSample(stored, nil)
	var efficiency EvaluationResult
	var invalidRatio EvaluationResult
	for _, result := range results {
		if result.Metric == "agent_step_efficiency" {
			efficiency = result
		}
		if result.Metric == "agent_invalid_search_ratio" {
			invalidRatio = result
		}
	}
	if efficiency.Status != StatusScored || efficiency.Score != (1.0/3.0) {
		t.Fatalf("efficiency = %+v, want scored with 1/3", efficiency)
	}
	if invalidRatio.Status != StatusScored || invalidRatio.Score != (1.0/3.0) {
		t.Fatalf("invalid ratio = %+v, want scored with 1/3", invalidRatio)
	}
}
