package ingest

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	BlockTypeText  = "text"
	BlockTypeTable = "table"
)

var multiSpaceRe = regexp.MustCompile(`\s{2,}`)

type ContentBlock struct {
	Type    string
	Content string
	PageNo  int
	TableID string
}

func BuildBlocks(content string) []ContentBlock {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	paragraphs := splitParagraphUnits(content)
	blocks := make([]ContentBlock, 0, len(paragraphs))
	tableCount := 0
	for _, paragraph := range paragraphs {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}

		if looksLikeTable(paragraph) {
			tableCount++
			blocks = append(blocks, ContentBlock{
				Type:    BlockTypeTable,
				Content: paragraph,
				PageNo:  0,
				TableID: fmt.Sprintf("table_%d", tableCount),
			})
			continue
		}

		blocks = append(blocks, ContentBlock{
			Type:    BlockTypeText,
			Content: paragraph,
			PageNo:  0,
		})
	}

	return blocks
}

func splitParagraphUnits(content string) []string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	units := make([]string, 0, len(lines))
	buffer := make([]string, 0, 8)

	flush := func() {
		if len(buffer) == 0 {
			return
		}
		units = append(units, strings.Join(buffer, "\n"))
		buffer = buffer[:0]
	}

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		buffer = append(buffer, strings.TrimRight(line, " \t"))
	}
	flush()

	return units
}

func looksLikeTable(paragraph string) bool {
	lines := strings.Split(paragraph, "\n")
	if len(lines) < 2 {
		return false
	}

	total := 0
	valid := 0
	byCols := map[int]int{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		total++
		cols := splitColumns(line)
		if len(cols) < 2 {
			continue
		}
		valid++
		byCols[len(cols)]++
	}

	if total < 2 || valid < 2 {
		return false
	}
	if float64(valid)/float64(total) < 0.6 {
		return false
	}

	maxCount := 0
	for _, count := range byCols {
		if count > maxCount {
			maxCount = count
		}
	}
	// At least two rows should share the same column count.
	return maxCount >= 2
}

func splitColumns(line string) []string {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	if strings.Contains(line, "|") {
		raw := strings.Split(line, "|")
		cols := make([]string, 0, len(raw))
		for _, item := range raw {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			cols = append(cols, item)
		}
		if len(cols) >= 2 {
			return cols
		}
	}

	if strings.Contains(line, "\t") {
		raw := strings.Split(line, "\t")
		cols := make([]string, 0, len(raw))
		for _, item := range raw {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			cols = append(cols, item)
		}
		if len(cols) >= 2 {
			return cols
		}
	}

	raw := multiSpaceRe.Split(line, -1)
	cols := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		cols = append(cols, item)
	}
	if len(cols) >= 2 {
		return cols
	}

	return nil
}
