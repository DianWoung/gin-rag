package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestEvalctlExportTraceRequiresTraceID(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run([]string{"export-trace"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run() error = nil, want missing trace id error")
	}
	if !strings.Contains(err.Error(), "requires a trace id") {
		t.Fatalf("error = %q", err)
	}
}

func TestEvalctlReplaySampleRequiresSampleID(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run([]string{"replay-sample"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run() error = nil, want missing sample id error")
	}
}

func TestEvalctlScoreSampleRequiresSampleID(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run([]string{"score-sample"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run() error = nil, want missing sample id error")
	}
}

func TestEvalctlRunTraceRequiresTraceID(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run([]string{"run-trace"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run() error = nil, want missing trace id error")
	}
	if !strings.Contains(err.Error(), "requires a trace id") {
		t.Fatalf("error = %q", err)
	}
}

func TestEvalctlCompareSamplesRequiresSampleID(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run([]string{"compare-samples"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run() error = nil, want missing sample id error")
	}
	if !strings.Contains(err.Error(), "requires at least one sample id") {
		t.Fatalf("error = %q", err)
	}
}

func TestEvalctlAnnotateSampleRequiresSampleID(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run([]string{"annotate-sample"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run() error = nil, want missing sample id error")
	}
	if !strings.Contains(err.Error(), "requires a sample id") {
		t.Fatalf("error = %q", err)
	}
}
