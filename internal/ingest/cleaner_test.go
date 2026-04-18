package ingest

import (
	"strings"
	"testing"
)

func TestCleanerNormalizesCharactersAndBlankLines(t *testing.T) {
	cleaner := NewCleaner()
	raw := "\ufeffalpha\x00\r\n\r\n\r\nbeta  \r\ngamma\t \n"

	cleaned, report := cleaner.Clean(raw)

	want := "alpha\n\nbeta\ngamma"
	if cleaned != want {
		t.Fatalf("cleaned = %q, want %q", cleaned, want)
	}
	if report.RemovedBlankLines != 1 {
		t.Fatalf("RemovedBlankLines = %d, want 1", report.RemovedBlankLines)
	}
	if !report.Changed {
		t.Fatal("Changed = false, want true")
	}
}

func TestCleanerRemovesConsecutiveDuplicateShortLines(t *testing.T) {
	cleaner := NewCleaner()
	raw := "Header\nHeader\n正文内容保留。\n"

	cleaned, report := cleaner.Clean(raw)

	want := "Header\n正文内容保留。"
	if cleaned != want {
		t.Fatalf("cleaned = %q, want %q", cleaned, want)
	}
	if report.RemovedDuplicateLines != 1 {
		t.Fatalf("RemovedDuplicateLines = %d, want 1", report.RemovedDuplicateLines)
	}
}

func TestCleanerRemovesStandalonePageNumbers(t *testing.T) {
	cleaner := NewCleaner()
	raw := "前言\n\n- 3 -\n\n正文\n\nPage 4\n"

	cleaned, report := cleaner.Clean(raw)

	want := "前言\n\n正文"
	if cleaned != want {
		t.Fatalf("cleaned = %q, want %q", cleaned, want)
	}
	if report.RemovedPageNumberLines != 2 {
		t.Fatalf("RemovedPageNumberLines = %d, want 2", report.RemovedPageNumberLines)
	}
}

func TestCleanerRemovesRepeatedHeaderFooter(t *testing.T) {
	cleaner := NewCleaner()
	raw := strings.Join([]string{
		"ACME Quarterly Report",
		"正文 A",
		"",
		"1",
		"",
		"ACME Quarterly Report",
		"正文 B",
		"",
		"2",
		"",
		"ACME Quarterly Report",
		"正文 C",
	}, "\n")

	cleaned, report := cleaner.Clean(raw)

	if strings.Contains(cleaned, "ACME Quarterly Report") {
		t.Fatalf("cleaned still contains repeated header: %q", cleaned)
	}
	if report.RemovedRepeatedHeaderFooterLines != 3 {
		t.Fatalf("RemovedRepeatedHeaderFooterLines = %d, want 3", report.RemovedRepeatedHeaderFooterLines)
	}
}

func TestCleanerKeepsLegitimateRepeatedShortContent(t *testing.T) {
	cleaner := NewCleaner()
	raw := strings.Join([]string{
		"Overview",
		"Alpha details.",
		"",
		"Overview",
		"Beta details.",
	}, "\n")

	cleaned, report := cleaner.Clean(raw)

	if cleaned != raw {
		t.Fatalf("cleaned = %q, want original %q", cleaned, raw)
	}
	if report.RemovedRepeatedHeaderFooterLines != 0 {
		t.Fatalf("RemovedRepeatedHeaderFooterLines = %d, want 0", report.RemovedRepeatedHeaderFooterLines)
	}
}
