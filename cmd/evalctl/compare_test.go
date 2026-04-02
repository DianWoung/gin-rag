package main

import (
	"testing"

	"github.com/dianwang-mac/go-rag/internal/eval"
	"github.com/dianwang-mac/go-rag/internal/tracebridge"
)

func TestParseSampleIDsSupportsCommaSeparatedAndDeduped(t *testing.T) {
	got := parseSampleIDs([]string{"a,b", "b", " c ", "", "d,e"})
	want := []string{"a", "b", "c", "d", "e"}
	if len(got) != len(want) {
		t.Fatalf("len(got)=%d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildComparisonPayloadAggregatesFocusMetrics(t *testing.T) {
	samples := map[string]eval.StoredSample{
		"s1": {
			SampleID: "s1",
			Sample: tracebridge.ChatSample{
				Question:       "q1",
				OriginalQuery:  "oq1",
				RewrittenQuery: "rq1",
				Chunks: []tracebridge.RetrievedChunk{
					{Index: 0, Content: "c1"},
				},
			},
		},
		"s2": {
			SampleID: "s2",
			Sample: tracebridge.ChatSample{
				Question:       "q2",
				OriginalQuery:  "oq2",
				RewrittenQuery: "rq2",
				Chunks: []tracebridge.RetrievedChunk{
					{Index: 0, Content: "c1"},
					{Index: 1, Content: "c2"},
				},
			},
		},
	}
	results := []eval.EvaluationResult{
		{SampleID: "s1", Target: eval.TargetCaptured, Metric: "retrieval_precision_at_k", Status: eval.StatusScored, Score: 1},
		{SampleID: "s1", Target: eval.TargetCaptured, Metric: "grounded_answer", Status: eval.StatusScored, Score: 0.8},
		{SampleID: "s2", Target: eval.TargetCaptured, Metric: "retrieval_precision_at_k", Status: eval.StatusScored, Score: 0.6},
		{SampleID: "s2", Target: eval.TargetCaptured, Metric: "grounded_answer", Status: eval.StatusSkipped, Score: 0},
	}

	payload := buildComparisonPayload(
		[]string{"s1", "s2"},
		samples,
		results,
		nil,
		[]string{"retrieval_precision_at_k", "grounded_answer"},
	)

	if len(payload.BySample) != 2 {
		t.Fatalf("len(BySample)=%d, want 2", len(payload.BySample))
	}
	if payload.BySample[0].ChunkCount != 1 || payload.BySample[1].ChunkCount != 2 {
		t.Fatalf("chunk counts = %d,%d", payload.BySample[0].ChunkCount, payload.BySample[1].ChunkCount)
	}

	var foundPrecision bool
	var foundGrounded bool
	for _, agg := range payload.Aggregate {
		if agg.Target != eval.TargetCaptured {
			continue
		}
		if agg.Metric == "retrieval_precision_at_k" {
			foundPrecision = true
			if agg.Count != 2 {
				t.Fatalf("precision count=%d, want 2", agg.Count)
			}
			if agg.AverageScore != 0.8 {
				t.Fatalf("precision average=%.2f, want 0.8", agg.AverageScore)
			}
		}
		if agg.Metric == "grounded_answer" {
			foundGrounded = true
			if agg.Count != 1 {
				t.Fatalf("grounded count=%d, want 1", agg.Count)
			}
			if agg.AverageScore != 0.8 {
				t.Fatalf("grounded average=%.2f, want 0.8", agg.AverageScore)
			}
		}
	}
	if !foundPrecision || !foundGrounded {
		t.Fatalf("aggregate missing expected metrics: %+v", payload.Aggregate)
	}
	if payload.ManualAggregate.AnnotationCount != 0 {
		t.Fatalf("ManualAggregate.AnnotationCount=%d, want 0", payload.ManualAggregate.AnnotationCount)
	}
}

func TestBuildComparisonPayloadAggregatesManualAnnotations(t *testing.T) {
	samples := map[string]eval.StoredSample{
		"s1": {SampleID: "s1", Sample: tracebridge.ChatSample{Question: "q1"}},
		"s2": {SampleID: "s2", Sample: tracebridge.ChatSample{Question: "q2"}},
	}
	annotations := []eval.ManualAnnotation{
		{SampleID: "s1", RetrievalRelevance: 1, GroundedAnswer: 0.8, CitationCorrectness: 1, AbstentionQuality: 1},
		{SampleID: "s1", RetrievalRelevance: 0.6, GroundedAnswer: 0.6, CitationCorrectness: 0.8, AbstentionQuality: 1},
		{SampleID: "s2", RetrievalRelevance: 0.4, GroundedAnswer: 0.2, CitationCorrectness: 0.5, AbstentionQuality: 0.9},
	}

	payload := buildComparisonPayload(
		[]string{"s1", "s2"},
		samples,
		nil,
		annotations,
		[]string{"retrieval_precision_at_k", "grounded_answer"},
	)

	if payload.ManualAggregate.AnnotationCount != 3 {
		t.Fatalf("AnnotationCount=%d, want 3", payload.ManualAggregate.AnnotationCount)
	}
	if payload.BySample[0].ManualCount != 2 {
		t.Fatalf("s1 ManualCount=%d, want 2", payload.BySample[0].ManualCount)
	}
	if payload.BySample[1].ManualCount != 1 {
		t.Fatalf("s2 ManualCount=%d, want 1", payload.BySample[1].ManualCount)
	}
	if payload.ManualAggregate.MetricScores["retrieval_relevance"] != (1+0.6+0.4)/3 {
		t.Fatalf("retrieval_relevance average=%f", payload.ManualAggregate.MetricScores["retrieval_relevance"])
	}
}
