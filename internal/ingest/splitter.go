package ingest

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type Splitter struct {
	chunkSize    int
	chunkOverlap int
}

type Chunk struct {
	Content string
	Type    string
	PageNo  int
	TableID string
}

func NewSplitter(chunkSize, chunkOverlap int) *Splitter {
	if chunkSize <= 0 {
		chunkSize = 800
	}
	if chunkOverlap < 0 {
		chunkOverlap = 0
	}
	if chunkOverlap >= chunkSize {
		chunkOverlap = chunkSize / 4
	}

	return &Splitter{
		chunkSize:    chunkSize,
		chunkOverlap: chunkOverlap,
	}
}

// Split breaks content into chunks along paragraph and sentence boundaries,
// falling back to rune-level splitting only when a single sentence exceeds
// chunkSize.
func (s *Splitter) Split(content string) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	if runeLen(content) <= s.chunkSize {
		return []string{content}
	}

	segments := splitIntoSegments(content, s.chunkSize)
	return s.mergeSegments(segments)
}

// SplitBlocks chunks structured content blocks while preserving block metadata.
func (s *Splitter) SplitBlocks(blocks []ContentBlock) []Chunk {
	if len(blocks) == 0 {
		return nil
	}

	chunks := make([]Chunk, 0, len(blocks))
	for _, block := range blocks {
		blockType := strings.TrimSpace(block.Type)
		if blockType == "" {
			blockType = BlockTypeText
		}

		switch blockType {
		case BlockTypeTable:
			tableChunks := s.splitTableBlock(block)
			chunks = append(chunks, tableChunks...)
		default:
			parts := s.Split(block.Content)
			for _, part := range parts {
				chunks = append(chunks, Chunk{
					Content: strings.TrimSpace(part),
					Type:    blockType,
					PageNo:  block.PageNo,
					TableID: block.TableID,
				})
			}
		}
	}

	return compactChunks(chunks)
}

func (s *Splitter) splitTableBlock(block ContentBlock) []Chunk {
	lines := splitNonEmptyLines(block.Content)
	if len(lines) == 0 {
		return nil
	}
	if len(lines) == 1 {
		return []Chunk{{
			Content: lines[0],
			Type:    BlockTypeTable,
			PageNo:  block.PageNo,
			TableID: block.TableID,
		}}
	}

	header := lines[0]
	rows := lines[1:]
	result := make([]Chunk, 0, len(rows))

	for start := 0; start < len(rows); {
		current := []string{header}
		end := start
		for end < len(rows) {
			candidate := append(current, rows[end])
			if runeLen(strings.Join(candidate, "\n")) > s.chunkSize {
				break
			}
			current = candidate
			end++
		}
		if end == start {
			current = append(current, rows[start])
			end++
		}

		result = append(result, Chunk{
			Content: strings.Join(current, "\n"),
			Type:    BlockTypeTable,
			PageNo:  block.PageNo,
			TableID: block.TableID,
		})

		next := end
		if s.chunkOverlap > 0 && end < len(rows) {
			total := 0
			for i := end - 1; i > start; i-- {
				add := runeLen(rows[i]) + 1
				if total+add > s.chunkOverlap {
					break
				}
				total += add
				next = i
			}
		}
		if next <= start {
			next = end
		}
		start = next
	}

	return result
}

// splitIntoSegments breaks text into paragraph-level or sentence-level pieces.
// Any segment still longer than maxRunes is split by rune as a last resort.
func splitIntoSegments(text string, maxRunes int) []string {
	paragraphs := splitParagraphs(text)

	var segments []string
	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		if runeLen(para) <= maxRunes {
			segments = append(segments, para)
			continue
		}
		// Paragraph too large — split into sentences.
		sentences := splitSentences(para)
		for _, sent := range sentences {
			sent = strings.TrimSpace(sent)
			if sent == "" {
				continue
			}
			if runeLen(sent) <= maxRunes {
				segments = append(segments, sent)
				continue
			}
			// Sentence still too large — hard split by rune.
			segments = append(segments, splitByRune(sent, maxRunes)...)
		}
	}
	return segments
}

// mergeSegments greedily packs segments into chunks up to chunkSize, with
// sentence-level overlap between consecutive chunks.
func (s *Splitter) mergeSegments(segments []string) []string {
	if len(segments) == 0 {
		return nil
	}

	var chunks []string
	current := segments[0]
	overlapStart := 0 // index of the first segment in the overlap window

	for i := 1; i < len(segments); i++ {
		merged := current + "\n" + segments[i]
		if runeLen(merged) <= s.chunkSize {
			current = merged
			continue
		}

		// Flush current chunk.
		chunks = append(chunks, strings.TrimSpace(current))

		// Build overlap prefix from trailing segments of the previous chunk.
		overlapPrefix := ""
		if s.chunkOverlap > 0 {
			overlapPrefix, overlapStart = s.buildOverlap(segments, overlapStart, i)
		}

		if overlapPrefix != "" {
			current = overlapPrefix + "\n" + segments[i]
		} else {
			current = segments[i]
			overlapStart = i
		}
	}

	last := strings.TrimSpace(current)
	if last != "" {
		chunks = append(chunks, last)
	}
	return chunks
}

// buildOverlap walks backward from segEnd to collect segments that fit within
// chunkOverlap runes. Returns the overlap text and the new overlapStart index.
func (s *Splitter) buildOverlap(segments []string, _ int, segEnd int) (string, int) {
	var parts []string
	total := 0
	start := segEnd
	for j := segEnd - 1; j >= 0; j-- {
		add := runeLen(segments[j])
		if total+add > s.chunkOverlap {
			break
		}
		total += add + 1 // +1 for newline separator
		parts = append(parts, segments[j])
		start = j
	}
	// Reverse parts to restore original order.
	for left, right := 0, len(parts)-1; left < right; left, right = left+1, right-1 {
		parts[left], parts[right] = parts[right], parts[left]
	}
	return strings.Join(parts, "\n"), start
}

// splitParagraphs splits on double-newline boundaries.
func splitParagraphs(text string) []string {
	return strings.Split(text, "\n\n")
}

func splitNonEmptyLines(text string) []string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

// splitSentences splits text at sentence-ending punctuation while keeping the
// punctuation attached to the preceding sentence. Handles both CJK (。！？）
// and Western (.!?) sentence endings.
func splitSentences(text string) []string {
	var sentences []string
	var buf strings.Builder

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		buf.WriteRune(runes[i])

		if !isSentenceEnd(runes[i]) {
			continue
		}

		// CJK sentence enders are always a split point.
		if isCJKSentenceEnd(runes[i]) {
			sentences = appendNonEmpty(sentences, buf.String())
			buf.Reset()
			continue
		}

		// Western sentence ender (.!?) — only split if followed by whitespace
		// or end of text, to avoid splitting on abbreviations like "e.g.".
		next := peekRune(runes, i+1)
		if next == 0 || unicode.IsSpace(next) {
			// For periods, skip if preceded by a single lowercase letter
			// (abbreviation like "e.g.", "i.e.", "Dr.").
			if runes[i] == '.' && i >= 1 && unicode.IsLower(runes[i-1]) {
				if i < 2 || !unicode.IsLetter(runes[i-2]) {
					continue
				}
			}
			sentences = appendNonEmpty(sentences, buf.String())
			buf.Reset()
		}
	}

	if buf.Len() > 0 {
		sentences = appendNonEmpty(sentences, buf.String())
	}
	return sentences
}

func isSentenceEnd(r rune) bool {
	return isCJKSentenceEnd(r) || r == '.' || r == '!' || r == '?'
}

func isCJKSentenceEnd(r rune) bool {
	return r == '。' || r == '！' || r == '？'
}

func peekRune(runes []rune, idx int) rune {
	if idx >= len(runes) {
		return 0
	}
	return runes[idx]
}

func appendNonEmpty(ss []string, s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ss
	}
	return append(ss, s)
}

// splitByRune is the last-resort hard split by rune count.
func splitByRune(text string, size int) []string {
	runes := []rune(text)
	var parts []string
	for i := 0; i < len(runes); i += size {
		end := i + size
		if end > len(runes) {
			end = len(runes)
		}
		part := strings.TrimSpace(string(runes[i:end]))
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func runeLen(s string) int {
	return utf8.RuneCountInString(s)
}

func compactChunks(chunks []Chunk) []Chunk {
	out := make([]Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		chunk.Content = strings.TrimSpace(chunk.Content)
		if chunk.Content == "" {
			continue
		}
		if strings.TrimSpace(chunk.Type) == "" {
			chunk.Type = BlockTypeText
		}
		out = append(out, chunk)
	}
	return out
}
