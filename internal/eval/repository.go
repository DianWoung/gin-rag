package eval

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) SaveSample(sample StoredSample) (StoredSample, error) {
	var existing SampleRecord
	err := r.db.Where("trace_id = ?", sample.Sample.TraceID).First(&existing).Error
	switch {
	case err == nil:
		return existing.ToStoredSample()
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return StoredSample{}, fmt.Errorf("find sample by trace id: %w", err)
	}

	record, err := NewSampleRecord(sample.Sample, sample.Warnings)
	if err != nil {
		return StoredSample{}, fmt.Errorf("build sample record: %w", err)
	}
	if err := r.db.Create(&record).Error; err != nil {
		return StoredSample{}, fmt.Errorf("create sample: %w", err)
	}

	return record.ToStoredSample()
}

func (r *Repository) GetSample(sampleID string) (StoredSample, error) {
	var record SampleRecord
	if err := r.db.First(&record, "sample_id = ?", sampleID).Error; err != nil {
		return StoredSample{}, fmt.Errorf("get sample: %w", err)
	}

	return record.ToStoredSample()
}

func (r *Repository) SaveReplayRun(run ReplayRun) (ReplayRun, error) {
	record := NewReplayRunRecord(run)
	if err := r.db.Create(&record).Error; err != nil {
		return ReplayRun{}, fmt.Errorf("create replay run: %w", err)
	}

	return record.ToReplayRun(), nil
}

func (r *Repository) GetLatestReplayRun(sampleID string) (*ReplayRun, error) {
	var record ReplayRunRecord
	if err := r.db.Where("sample_id = ?", sampleID).Order("created_at desc").First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest replay run: %w", err)
	}

	run := record.ToReplayRun()
	return &run, nil
}

func (r *Repository) SaveEvaluationResults(results []EvaluationResult) error {
	if len(results) == 0 {
		return nil
	}

	records := make([]EvaluationResultRecord, 0, len(results))
	for _, result := range results {
		records = append(records, NewEvaluationResultRecord(result))
	}

	if err := r.db.Create(&records).Error; err != nil {
		return fmt.Errorf("create evaluation results: %w", err)
	}

	return nil
}

func (r *Repository) GetSamples(sampleIDs []string) (map[string]StoredSample, error) {
	sampleIDs = normalizeIDs(sampleIDs)
	if len(sampleIDs) == 0 {
		return map[string]StoredSample{}, nil
	}

	var records []SampleRecord
	if err := r.db.Where("sample_id IN ?", sampleIDs).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("get samples: %w", err)
	}

	result := make(map[string]StoredSample, len(records))
	for _, record := range records {
		stored, err := record.ToStoredSample()
		if err != nil {
			return nil, fmt.Errorf("decode sample %s: %w", record.SampleID, err)
		}
		result[record.SampleID] = stored
	}
	return result, nil
}

// GetLatestEvaluationResults returns the latest result per (sample, target, metric).
func (r *Repository) GetLatestEvaluationResults(sampleIDs []string) ([]EvaluationResult, error) {
	sampleIDs = normalizeIDs(sampleIDs)
	if len(sampleIDs) == 0 {
		return nil, nil
	}

	var records []EvaluationResultRecord
	if err := r.db.
		Where("sample_id IN ?", sampleIDs).
		Order("created_at desc").
		Find(&records).Error; err != nil {
		return nil, fmt.Errorf("get evaluation results: %w", err)
	}

	seen := make(map[string]struct{}, len(records))
	results := make([]EvaluationResult, 0, len(records))
	for _, record := range records {
		key := strings.Join([]string{record.SampleID, record.Target, record.Metric}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		results = append(results, EvaluationResult{
			EvaluationResultID: record.EvaluationResultID,
			SampleID:           record.SampleID,
			ReplayRunID:        record.ReplayRunID,
			Target:             record.Target,
			Metric:             record.Metric,
			Status:             record.Status,
			Score:              record.Score,
			Summary:            record.Summary,
			CreatedAt:          record.CreatedAt,
		})
	}
	return results, nil
}

func normalizeIDs(ids []string) []string {
	set := map[string]struct{}{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := set[id]; ok {
			continue
		}
		set[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
