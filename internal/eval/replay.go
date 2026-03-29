package eval

import (
	"context"
	"fmt"
	"strings"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type ChatModelFactory interface {
	NewChatModel(ctx context.Context, modelName string, temperature float32) (einomodel.BaseChatModel, error)
}

func ReplayChatSample(ctx context.Context, factory ChatModelFactory, stored StoredSample) (ReplayRun, error) {
	sample := stored.Sample
	if sample.Prompt == "" {
		return ReplayRun{}, fmt.Errorf("sample prompt is empty")
	}
	if sample.Model == "" {
		return ReplayRun{}, fmt.Errorf("sample model is empty")
	}

	model, err := factory.NewChatModel(ctx, sample.Model, sample.Temperature)
	if err != nil {
		return ReplayRun{
			SampleID:     stored.SampleID,
			Model:        sample.Model,
			Temperature:  sample.Temperature,
			Prompt:       sample.Prompt,
			Status:       "error",
			ErrorMessage: err.Error(),
		}, nil
	}

	messages := parsePromptMessages(sample.Prompt)
	resp, err := model.Generate(ctx, messages)
	if err != nil {
		return ReplayRun{
			SampleID:     stored.SampleID,
			Model:        sample.Model,
			Temperature:  sample.Temperature,
			Prompt:       sample.Prompt,
			Status:       "error",
			ErrorMessage: err.Error(),
		}, nil
	}

	return ReplayRun{
		SampleID:    stored.SampleID,
		Model:       sample.Model,
		Temperature: sample.Temperature,
		Prompt:      sample.Prompt,
		Answer:      strings.TrimSpace(resp.Content),
		Status:      "completed",
	}, nil
}

func parsePromptMessages(prompt string) []*schema.Message {
	blocks := strings.Split(strings.TrimSpace(prompt), "\n\n")
	messages := make([]*schema.Message, 0, len(blocks))
	for _, block := range blocks {
		role, content, ok := strings.Cut(block, ": ")
		if !ok {
			messages = append(messages, &schema.Message{
				Role:    schema.User,
				Content: block,
			})
			continue
		}
		messages = append(messages, &schema.Message{
			Role:    schema.RoleType(strings.ToLower(strings.TrimSpace(role))),
			Content: content,
		})
	}
	if len(messages) == 0 {
		messages = append(messages, &schema.Message{
			Role:    schema.User,
			Content: prompt,
		})
	}

	return messages
}
