package eval

import (
	"context"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/dianwang-mac/go-rag/internal/tracebridge"
)

type fakeFactory struct {
	model einomodel.BaseChatModel
	err   error
}

func (f fakeFactory) NewChatModel(context.Context, string, float32) (einomodel.BaseChatModel, error) {
	return f.model, f.err
}

type fakeModel struct {
	answer string
}

func (m fakeModel) Generate(context.Context, []*schema.Message, ...einomodel.Option) (*schema.Message, error) {
	return &schema.Message{Role: schema.Assistant, Content: m.answer}, nil
}

func (m fakeModel) Stream(context.Context, []*schema.Message, ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, Content: m.answer}}), nil
}

func TestReplayChatSampleUsesPersistedPromptAndSettings(t *testing.T) {
	stored := StoredSample{
		SampleID: "sample-1",
		Sample: tracebridge.ChatSample{
			Prompt:      "system: use context\n\nuser: what is rag",
			Model:       "gpt-4o-mini",
			Temperature: 0.2,
		},
	}

	run, err := ReplayChatSample(context.Background(), fakeFactory{model: fakeModel{answer: "rag means retrieval augmented generation"}}, stored)
	if err != nil {
		t.Fatalf("ReplayChatSample() error = %v", err)
	}
	if run.Status != "completed" {
		t.Fatalf("Status = %q, want completed", run.Status)
	}
	if run.Answer == "" {
		t.Fatal("Answer = empty, want replay output")
	}
}
