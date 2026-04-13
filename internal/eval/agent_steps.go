package eval

import (
	"regexp"
	"strings"

	"github.com/dianwang-mac/go-rag/internal/appdto"
)

var agentStepPattern = regexp.MustCompile(`(?i)\[agent\]\s*step\s+(\d+)/(\d+)\s+query=(?:"([^"]*)"|([^\n]+?))\s+retrieved=(\d+)`)

func extractAgentSteps(text string) []appdto.AgentStep {
	matches := agentStepPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}

	steps := make([]appdto.AgentStep, 0, len(matches))
	for _, match := range matches {
		if len(match) < 6 {
			continue
		}
		query := strings.TrimSpace(match[3])
		if query == "" {
			query = strings.TrimSpace(match[4])
		}
		steps = append(steps, appdto.AgentStep{
			Step:           parsePositiveInt(match[1]),
			Query:          query,
			RetrievedCount: parsePositiveInt(match[5]),
			Action:         "search",
		})
	}

	return steps
}

func agentStepStats(steps []appdto.AgentStep) (totalSteps int, invalidSteps int) {
	if len(steps) == 0 {
		return 0, 0
	}
	totalSteps = len(steps)
	for _, step := range steps {
		if step.RetrievedCount == 0 {
			invalidSteps++
		}
	}
	return totalSteps, invalidSteps
}
