package ingest

import "testing"

func TestBuildBlocksDetectsTableParagraph(t *testing.T) {
	content := "产品  价格  库存\nA    10    3\nB    12    8\n\n这是正文段落。"
	blocks := BuildBlocks(content)
	if len(blocks) != 2 {
		t.Fatalf("len(blocks)=%d, want 2", len(blocks))
	}
	if blocks[0].Type != BlockTypeTable {
		t.Fatalf("blocks[0].Type=%q, want %q", blocks[0].Type, BlockTypeTable)
	}
	if blocks[0].TableID == "" {
		t.Fatal("blocks[0].TableID is empty")
	}
	if blocks[1].Type != BlockTypeText {
		t.Fatalf("blocks[1].Type=%q, want %q", blocks[1].Type, BlockTypeText)
	}
}

func TestBuildBlocksKeepsPlainParagraphAsText(t *testing.T) {
	content := "这是一段普通文本。\n没有表格列。"
	blocks := BuildBlocks(content)
	if len(blocks) != 1 {
		t.Fatalf("len(blocks)=%d, want 1", len(blocks))
	}
	if blocks[0].Type != BlockTypeText {
		t.Fatalf("blocks[0].Type=%q, want %q", blocks[0].Type, BlockTypeText)
	}
}
