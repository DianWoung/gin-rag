package ingest

import "strings"

type Splitter struct {
	chunkSize    int
	chunkOverlap int
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

func (s *Splitter) Split(content string) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	runes := []rune(content)
	if len(runes) <= s.chunkSize {
		return []string{content}
	}

	step := s.chunkSize - s.chunkOverlap
	if step <= 0 {
		step = s.chunkSize
	}

	var chunks []string
	for start := 0; start < len(runes); start += step {
		end := start + s.chunkSize
		if end > len(runes) {
			end = len(runes)
		}

		chunk := strings.TrimSpace(string(runes[start:end]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}

		if end == len(runes) {
			break
		}
	}

	return chunks
}
