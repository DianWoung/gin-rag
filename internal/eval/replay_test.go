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
	answer           string
	receivedMessages []*schema.Message
}

func (m *fakeModel) Generate(_ context.Context, messages []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	m.receivedMessages = append([]*schema.Message(nil), messages...)
	return &schema.Message{Role: schema.Assistant, Content: m.answer}, nil
}

func (m *fakeModel) Stream(context.Context, []*schema.Message, ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
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

	run, err := ReplayChatSample(context.Background(), fakeFactory{model: &fakeModel{answer: "rag means retrieval augmented generation"}}, stored)
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

func TestReplayChatSamplePrefersStructuredPromptMessages(t *testing.T) {
	model := &fakeModel{answer: "rag means retrieval augmented generation"}
	stored := StoredSample{
		SampleID: "sample-2",
		Sample: tracebridge.ChatSample{
			Prompt: "system: You are a grounded RAG assistant.\n\nRetrieved context:\n[1] eval chunk\n\nuser: what is rag",
			PromptMessages: []tracebridge.PromptMessage{
				{Role: "system", Content: "You are a grounded RAG assistant.\n\nRetrieved context:\n[1] eval chunk"},
				{Role: "user", Content: "what is rag"},
			},
			Model:       "deepseek-chat",
			Temperature: 0.2,
		},
	}

	run, err := ReplayChatSample(context.Background(), fakeFactory{model: model}, stored)
	if err != nil {
		t.Fatalf("ReplayChatSample() error = %v", err)
	}
	if run.Status != "completed" {
		t.Fatalf("Status = %q, want completed", run.Status)
	}
	if len(model.receivedMessages) != 2 {
		t.Fatalf("received message count = %d, want 2", len(model.receivedMessages))
	}
	if model.receivedMessages[0].Role != schema.System || model.receivedMessages[0].Content != "You are a grounded RAG assistant.\n\nRetrieved context:\n[1] eval chunk" {
		t.Fatalf("receivedMessages[0] = %+v", model.receivedMessages[0])
	}
	if model.receivedMessages[1].Role != schema.User || model.receivedMessages[1].Content != "what is rag" {
		t.Fatalf("receivedMessages[1] = %+v", model.receivedMessages[1])
	}
}
