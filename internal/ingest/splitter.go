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

	if len(content) <= s.chunkSize {
		return []string{content}
	}

	step := s.chunkSize - s.chunkOverlap
	if step <= 0 {
		step = s.chunkSize
	}

	var chunks []string
	for start := 0; start < len(content); start += step {
		end := start + s.chunkSize
		if end > len(content) {
			end = len(content)
		}

		chunk := strings.TrimSpace(content[start:end])
		if chunk != "" {
			chunks = append(chunks, chunk)
		}

		if end == len(content) {
			break
		}
	}

	return chunks
}
