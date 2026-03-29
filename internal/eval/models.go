package eval

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/dianwang-mac/go-rag/internal/tracebridge"
)

type SampleRecord struct {
	SampleID           string  `gorm:"primaryKey;size:36"`
	TraceID            string  `gorm:"uniqueIndex;size:128;not null"`
	ProjectName        string  `gorm:"size:128;not null"`
	RootSpanName       string  `gorm:"size:128;not null"`
	Question           string  `gorm:"type:longtext;not null"`
	OriginalQuery      string  `gorm:"type:longtext"`
	RewrittenQuery     string  `gorm:"type:longtext"`
	Answer             string  `gorm:"type:longtext"`
	Prompt             string  `gorm:"type:longtext;not null"`
	PromptMessagesJSON string  `gorm:"type:longtext"`
	Model              string  `gorm:"size:128"`
	Temperature        float32 `gorm:"not null;default:0"`
	KnowledgeBaseID    uint    `gorm:"not null;default:0"`
	KnowledgeBaseName  string  `gorm:"size:128"`
	CollectionName     string  `gorm:"size:128"`
	EmbeddingModel     string  `gorm:"size:128"`
	ChunksJSON         string  `gorm:"type:longtext;not null"`
	WarningsJSON       string  `gorm:"type:longtext;not null"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type ReplayRunRecord struct {
	ReplayRunID  string  `gorm:"primaryKey;size:36"`
	SampleID     string  `gorm:"index;size:36;not null"`
	Model        string  `gorm:"size:128;not null"`
	Temperature  float32 `gorm:"not null;default:0"`
	Prompt       string  `gorm:"type:longtext;not null"`
	Answer       string  `gorm:"type:longtext"`
	Status       string  `gorm:"size:32;not null"`
	ErrorMessage string  `gorm:"type:text"`
	CreatedAt    time.Time
}

type EvaluationResultRecord struct {
	EvaluationResultID string  `gorm:"primaryKey;size:36"`
	SampleID           string  `gorm:"index;size:36;not null"`
	ReplayRunID        string  `gorm:"index;size:36"`
	Target             string  `gorm:"size:32;not null"`
	Metric             string  `gorm:"size:64;not null"`
	Status             string  `gorm:"size:32;not null"`
	Score              float64 `gorm:"not null;default:0"`
	Summary            string  `gorm:"type:text"`
	CreatedAt          time.Time
}

type StoredSample struct {
	SampleID  string
	Sample    tracebridge.ChatSample
	Warnings  []tracebridge.ExportWarning
	CreatedAt time.Time
}

type ReplayRun struct {
	ReplayRunID  string
	SampleID     string
	Model        string
	Temperature  float32
	Prompt       string
	Answer       string
	Status       string
	ErrorMessage string
	CreatedAt    time.Time
}

type EvaluationResult struct {
	EvaluationResultID string
	SampleID           string
	ReplayRunID        string
	Target             string
	Metric             string
	Status             string
	Score              float64
	Summary            string
	CreatedAt          time.Time
}

func NewSampleRecord(sample tracebridge.ChatSample, warnings []tracebridge.ExportWarning) (SampleRecord, error) {
	promptMessagesJSON, err := json.Marshal(sample.PromptMessages)
	if err != nil {
		return SampleRecord{}, err
	}
	chunksJSON, err := json.Marshal(sample.Chunks)
	if err != nil {
		return SampleRecord{}, err
	}
	warningsJSON, err := json.Marshal(warnings)
	if err != nil {
		return SampleRecord{}, err
	}

	return SampleRecord{
		SampleID:           uuid.NewString(),
		TraceID:            sample.TraceID,
		ProjectName:        sample.ProjectName,
		RootSpanName:       sample.RootSpanName,
		Question:           sample.Question,
		OriginalQuery:      sample.OriginalQuery,
		RewrittenQuery:     sample.RewrittenQuery,
		Answer:             sample.Answer,
		Prompt:             sample.Prompt,
		PromptMessagesJSON: string(promptMessagesJSON),
		Model:              sample.Model,
		Temperature:        sample.Temperature,
		KnowledgeBaseID:    sample.KnowledgeBaseID,
		KnowledgeBaseName:  sample.KnowledgeBaseName,
		CollectionName:     sample.CollectionName,
		EmbeddingModel:     sample.EmbeddingModel,
		ChunksJSON:         string(chunksJSON),
		WarningsJSON:       string(warningsJSON),
	}, nil
}

func (r SampleRecord) ToStoredSample() (StoredSample, error) {
	var sample tracebridge.ChatSample
	sample = tracebridge.ChatSample{
		TraceID:           r.TraceID,
		ProjectName:       r.ProjectName,
		RootSpanName:      r.RootSpanName,
		Question:          r.Question,
		OriginalQuery:     r.OriginalQuery,
		RewrittenQuery:    r.RewrittenQuery,
		Answer:            r.Answer,
		Prompt:            r.Prompt,
		PromptMessages:    nil,
		Model:             r.Model,
		Temperature:       r.Temperature,
		KnowledgeBaseID:   r.KnowledgeBaseID,
		KnowledgeBaseName: r.KnowledgeBaseName,
		CollectionName:    r.CollectionName,
		EmbeddingModel:    r.EmbeddingModel,
	}
	if err := json.Unmarshal([]byte(r.ChunksJSON), &sample.Chunks); err != nil {
		return StoredSample{}, err
	}
	if r.PromptMessagesJSON != "" {
		if err := json.Unmarshal([]byte(r.PromptMessagesJSON), &sample.PromptMessages); err != nil {
			return StoredSample{}, err
		}
	}

	var warnings []tracebridge.ExportWarning
	if r.WarningsJSON != "" {
		if err := json.Unmarshal([]byte(r.WarningsJSON), &warnings); err != nil {
			return StoredSample{}, err
		}
	}

	return StoredSample{
		SampleID:  r.SampleID,
		Sample:    sample,
		Warnings:  warnings,
		CreatedAt: r.CreatedAt,
	}, nil
}

func NewReplayRunRecord(run ReplayRun) ReplayRunRecord {
	if run.ReplayRunID == "" {
		run.ReplayRunID = uuid.NewString()
	}

	return ReplayRunRecord{
		ReplayRunID:  run.ReplayRunID,
		SampleID:     run.SampleID,
		Model:        run.Model,
		Temperature:  run.Temperature,
		Prompt:       run.Prompt,
		Answer:       run.Answer,
		Status:       run.Status,
		ErrorMessage: run.ErrorMessage,
	}
}

func (r ReplayRunRecord) ToReplayRun() ReplayRun {
	return ReplayRun{
		ReplayRunID:  r.ReplayRunID,
		SampleID:     r.SampleID,
		Model:        r.Model,
		Temperature:  r.Temperature,
		Prompt:       r.Prompt,
		Answer:       r.Answer,
		Status:       r.Status,
		ErrorMessage: r.ErrorMessage,
		CreatedAt:    r.CreatedAt,
	}
}

func NewEvaluationResultRecord(result EvaluationResult) EvaluationResultRecord {
	if result.EvaluationResultID == "" {
		result.EvaluationResultID = uuid.NewString()
	}

	return EvaluationResultRecord{
		EvaluationResultID: result.EvaluationResultID,
		SampleID:           result.SampleID,
		ReplayRunID:        result.ReplayRunID,
		Target:             result.Target,
		Metric:             result.Metric,
		Status:             result.Status,
		Score:              result.Score,
		Summary:            result.Summary,
	}
}
