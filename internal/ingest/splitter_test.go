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
