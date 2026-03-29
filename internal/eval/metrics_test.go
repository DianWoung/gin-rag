package eval

import (
	"testing"

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
			Answer: "rag retrieval generation",
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
