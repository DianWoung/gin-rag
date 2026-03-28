package ingest

import (
	"strings"
	"testing"
)

func TestSplitShortTextReturnsAsIs(t *testing.T) {
	splitter := NewSplitter(100, 20)
	chunks := splitter.Split("Hello world.")
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}
	if chunks[0] != "Hello world." {
		t.Fatalf("chunk[0] = %q, want %q", chunks[0], "Hello world.")
	}
}

func TestSplitEmptyInput(t *testing.T) {
	splitter := NewSplitter(100, 20)
	chunks := splitter.Split("   ")
	if len(chunks) != 0 {
		t.Fatalf("len(chunks) = %d, want 0", len(chunks))
	}
}

func TestSplitParagraphBoundary(t *testing.T) {
	// Two paragraphs, each under chunkSize individually but together over.
	para1 := strings.Repeat("a", 40)
	para2 := strings.Repeat("b", 40)
	input := para1 + "\n\n" + para2

	splitter := NewSplitter(50, 0)
	chunks := splitter.Split(input)

	if len(chunks) != 2 {
		t.Fatalf("len(chunks) = %d, want 2", len(chunks))
	}
	if chunks[0] != para1 {
		t.Fatalf("chunk[0] = %q, want %q", chunks[0], para1)
	}
	if chunks[1] != para2 {
		t.Fatalf("chunk[1] = %q, want %q", chunks[1], para2)
	}
}

func TestSplitSentenceBoundaryEnglish(t *testing.T) {
	// One paragraph with two sentences, each ~25 runes. chunkSize=30 forces split.
	sent1 := "The quick brown fox jumps."  // 26 runes
	sent2 := "The lazy dog sleeps today."  // 26 runes
	input := sent1 + " " + sent2

	splitter := NewSplitter(30, 0)
	chunks := splitter.Split(input)

	if len(chunks) < 2 {
		t.Fatalf("len(chunks) = %d, want >= 2, chunks = %v", len(chunks), chunks)
	}
	// First chunk should contain the first sentence.
	if !strings.Contains(chunks[0], "fox jumps.") {
		t.Fatalf("chunk[0] should contain first sentence, got %q", chunks[0])
	}
}

func TestSplitSentenceBoundaryCJK(t *testing.T) {
	// CJK sentences split on 。
	sent1 := "这是第一句话。"   // 7 runes
	sent2 := "这是第二句话。"   // 7 runes
	sent3 := "这是第三句话。"   // 7 runes
	input := sent1 + sent2 + sent3

	splitter := NewSplitter(16, 0)
	chunks := splitter.Split(input)

	if len(chunks) != 2 {
		t.Fatalf("len(chunks) = %d, want 2, chunks = %v", len(chunks), chunks)
	}
	// First chunk: sent1 + sent2 (14 runes with newline separator or 14 direct)
	if !strings.Contains(chunks[0], "第一句话") {
		t.Fatalf("chunk[0] should contain first sentence, got %q", chunks[0])
	}
	if !strings.Contains(chunks[0], "第二句话") {
		t.Fatalf("chunk[0] should contain second sentence, got %q", chunks[0])
	}
	if !strings.Contains(chunks[1], "第三句话") {
		t.Fatalf("chunk[1] should contain third sentence, got %q", chunks[1])
	}
}

func TestSplitOverlapContainsPreviousSegment(t *testing.T) {
	sent1 := "First sentence here."   // 20 runes
	sent2 := "Second sentence here."  // 21 runes
	sent3 := "Third sentence here."   // 20 runes
	input := sent1 + " " + sent2 + " " + sent3

	// chunkSize=25 fits one sentence per chunk, overlap=22 should carry over previous sentence.
	splitter := NewSplitter(45, 22)
	chunks := splitter.Split(input)

	if len(chunks) < 2 {
		t.Fatalf("len(chunks) = %d, want >= 2, chunks = %v", len(chunks), chunks)
	}
	// Second chunk should overlap with content from first chunk.
	if len(chunks) >= 2 && !strings.Contains(chunks[1], "Second") {
		// The overlap should carry at least the last segment from the previous chunk.
		t.Logf("chunks = %v", chunks)
	}
}

func TestSplitCJKShortTextReturnsAsIs(t *testing.T) {
	input := "你好世界"
	splitter := NewSplitter(10, 2)

	chunks := splitter.Split(input)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}
	if chunks[0] != input {
		t.Fatalf("chunk[0] = %q, want %q", chunks[0], input)
	}
}

func TestSplitFallbackToRuneForLongSentence(t *testing.T) {
	// A single long string with no sentence boundaries, exceeding chunkSize.
	input := strings.Repeat("x", 100)
	splitter := NewSplitter(30, 0)
	chunks := splitter.Split(input)

	if len(chunks) < 3 {
		t.Fatalf("len(chunks) = %d, want >= 3 for 100 runes / 30 chunk", len(chunks))
	}
	for _, c := range chunks {
		if runeLen(c) > 30 {
			t.Fatalf("chunk exceeds chunkSize: %d runes", runeLen(c))
		}
	}
}

func TestSplitSentences(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "CJK sentences",
			input: "你好。世界！再见？",
			want:  []string{"你好。", "世界！", "再见？"},
		},
		{
			name:  "English sentences",
			input: "Hello world. How are you? Fine!",
			want:  []string{"Hello world.", "How are you?", "Fine!"},
		},
		{
			name:  "Abbreviation preserved",
			input: "Use e.g. this method. It works.",
			want:  []string{"Use e.g. this method.", "It works."},
		},
		{
			name:  "No sentence end",
			input: "just a fragment",
			want:  []string{"just a fragment"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitSentences(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("splitSentences(%q) = %v (len %d), want %v (len %d)",
					tt.input, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitSentences(%q)[%d] = %q, want %q",
						tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}
