package ingest

import "testing"

func TestSplitTextProducesOverlappingChunks(t *testing.T) {
	splitter := NewSplitter(10, 2)

	chunks := splitter.Split("abcdefghij1234567890XYZ")
	if len(chunks) != 3 {
		t.Fatalf("len(chunks) = %d, want 3", len(chunks))
	}

	if chunks[0] != "abcdefghij" {
		t.Fatalf("chunk[0] = %q, want %q", chunks[0], "abcdefghij")
	}
	if chunks[1] != "ij12345678" {
		t.Fatalf("chunk[1] = %q, want %q", chunks[1], "ij12345678")
	}
	if chunks[2] != "7890XYZ" {
		t.Fatalf("chunk[2] = %q, want %q", chunks[2], "7890XYZ")
	}
}

func TestSplitTextIgnoresEmptyInput(t *testing.T) {
	splitter := NewSplitter(100, 20)

	chunks := splitter.Split("   ")
	if len(chunks) != 0 {
		t.Fatalf("len(chunks) = %d, want 0", len(chunks))
	}
}

func TestSplitCJKTextByRune(t *testing.T) {
	// 15 CJK characters, chunkSize=10 runes, overlap=2 → step=8
	// chunk[0]: runes[0:10]  = "零一二三四五六七八九"
	// chunk[1]: runes[8:15]  = "八九十壹贰叁肆"
	input := "零一二三四五六七八九十壹贰叁肆"
	splitter := NewSplitter(10, 2)

	chunks := splitter.Split(input)
	if len(chunks) != 2 {
		t.Fatalf("len(chunks) = %d, want 2", len(chunks))
	}

	if chunks[0] != "零一二三四五六七八九" {
		t.Fatalf("chunk[0] = %q, want %q", chunks[0], "零一二三四五六七八九")
	}
	if chunks[1] != "八九十壹贰叁肆" {
		t.Fatalf("chunk[1] = %q, want %q", chunks[1], "八九十壹贰叁肆")
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
