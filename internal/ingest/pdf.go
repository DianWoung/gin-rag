package ingest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ledongthuc/pdf"
)

type PDFTextExtractor interface {
	Extract(ctx context.Context, content []byte) (string, error)
}

type PDFExtractor struct{}

func NewPDFExtractor() *PDFExtractor {
	return &PDFExtractor{}
}

func (e *PDFExtractor) Extract(_ context.Context, content []byte) (string, error) {
	if len(content) == 0 {
		return "", fmt.Errorf("pdf content is empty")
	}

	reader, err := pdf.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return "", err
	}

	textReader, err := reader.GetPlainText()
	if err != nil {
		return "", err
	}

	raw, err := io.ReadAll(textReader)
	if err != nil {
		return "", err
	}

	return normalizePDFText(string(raw)), nil
}

func normalizePDFText(content string) string {
	content = strings.ReplaceAll(content, "\x00", "")
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	lines := strings.Split(content, "\n")
	normalized := make([]string, 0, len(lines))
	previousBlank := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if previousBlank {
				continue
			}
			previousBlank = true
			normalized = append(normalized, "")
			continue
		}

		previousBlank = false
		normalized = append(normalized, trimmed)
	}

	return strings.TrimSpace(strings.Join(normalized, "\n"))
}
