package store

import (
	"fmt"

	"github.com/dianwang-mac/go-rag/internal/entity"
	"github.com/dianwang-mac/go-rag/internal/eval"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func OpenMySQL(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	if err := db.AutoMigrate(
		&entity.KnowledgeBase{},
		&entity.Document{},
		&entity.DocumentChunk{},
		&eval.SampleRecord{},
		&eval.ReplayRunRecord{},
		&eval.EvaluationResultRecord{},
		&eval.ManualAnnotationRecord{},
	); err != nil {
		return nil, fmt.Errorf("auto migrate: %w", err)
	}

	return db, nil
}
