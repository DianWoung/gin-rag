package ingest

import (
	"regexp"
	"strings"
)

const (
	maxShortNoiseLineRunes = 80
	minRepeatedNoiseHits   = 3
)

var (
	pageNumberLineRe = regexp.MustCompile(`^(?:-?\s*)?(?:page\s+\d+|第\s*\d+\s*页|\d+)(?:\s*-?)?$`)
)

type CleanReport struct {
	RemovedBlankLines                int  `json:"removed_blank_lines"`
	RemovedDuplicateLines            int  `json:"removed_duplicate_lines"`
	RemovedPageNumberLines           int  `json:"removed_page_number_lines"`
	RemovedRepeatedHeaderFooterLines int  `json:"removed_repeated_header_footer_lines"`
	Changed                          bool `json:"changed"`
}

type Cleaner struct{}

func NewCleaner() *Cleaner {
	return &Cleaner{}
}

func (c *Cleaner) Clean(raw string) (string, CleanReport) {
	if raw == "" {
		return "", CleanReport{}
	}

	normalized := normalizeCleaningInput(raw)
	lines := strings.Split(normalized, "\n")
	lines, pageRemoved := removePageNumberLines(lines)
	lines, duplicateRemoved := removeConsecutiveDuplicateShortLines(lines)
	lines, repeatedRemoved := removeRepeatedHeaderFooterLines(lines)
	lines, blankRemoved := collapseBlankLines(lines)

	cleaned := strings.TrimSpace(strings.Join(lines, "\n"))
	report := CleanReport{
		RemovedBlankLines:                blankRemoved,
		RemovedDuplicateLines:            duplicateRemoved,
		RemovedPageNumberLines:           pageRemoved,
		RemovedRepeatedHeaderFooterLines: repeatedRemoved,
	}
	report.Changed = cleaned != strings.TrimSpace(normalized)

	return cleaned, report
}

func normalizeCleaningInput(raw string) string {
	raw = strings.TrimPrefix(raw, "\ufeff")
	raw = strings.ReplaceAll(raw, "\x00", "")
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")

	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}

	return strings.Join(lines, "\n")
}

func removePageNumberLines(lines []string) ([]string, int) {
	out := make([]string, 0, len(lines))
	removed := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && pageNumberLineRe.MatchString(strings.ToLower(trimmed)) {
			removed++
			continue
		}
		out = append(out, line)
	}
	return out, removed
}

func removeConsecutiveDuplicateShortLines(lines []string) ([]string, int) {
	out := make([]string, 0, len(lines))
	removed := 0
	var lastNormalized string
	for _, line := range lines {
		normalized := normalizeNoiseLine(line)
		if normalized != "" && normalized == lastNormalized && isShortNoiseCandidate(normalized) {
			removed++
			continue
		}
		out = append(out, line)
		lastNormalized = normalized
	}
	return out, removed
}

func removeRepeatedHeaderFooterLines(lines []string) ([]string, int) {
	counts := map[string]int{}
	for _, line := range lines {
		key := normalizeNoiseLine(line)
		if key == "" || !isLikelyHeaderFooterCandidate(key) {
			continue
		}
		counts[key]++
	}

	repeated := map[string]struct{}{}
	for key, count := range counts {
		if count >= minRepeatedNoiseHits {
			repeated[key] = struct{}{}
		}
	}

	if len(repeated) == 0 {
		return lines, 0
	}

	out := make([]string, 0, len(lines))
	removed := 0
	for _, line := range lines {
		key := normalizeNoiseLine(line)
		if _, ok := repeated[key]; ok {
			removed++
			continue
		}
		out = append(out, line)
	}
	return out, removed
}

func collapseBlankLines(lines []string) ([]string, int) {
	out := make([]string, 0, len(lines))
	removed := 0
	previousBlank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if previousBlank {
				removed++
				continue
			}
			previousBlank = true
			out = append(out, "")
			continue
		}
		previousBlank = false
		out = append(out, strings.TrimSpace(line))
	}
	return out, removed
}

func normalizeNoiseLine(line string) string {
	return strings.TrimSpace(strings.ToLower(line))
}

func isShortNoiseCandidate(line string) bool {
	return line != "" && runeLen(line) <= maxShortNoiseLineRunes
}

func isLikelyHeaderFooterCandidate(line string) bool {
	if !isShortNoiseCandidate(line) {
		return false
	}
	words := strings.Fields(line)
	if len(words) > 8 {
		return false
	}
	if strings.Contains(line, "details") {
		return false
	}
	return true
}
