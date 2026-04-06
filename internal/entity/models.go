package entity

import "time"

type KnowledgeBase struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	Name            string    `gorm:"size:128;uniqueIndex;not null" json:"name"`
	Description     string    `gorm:"type:text" json:"description"`
	CollectionName  string    `gorm:"size:128;uniqueIndex;not null" json:"collection_name"`
	EmbeddingModel  string    `gorm:"size:128;not null" json:"embedding_model"`
	VectorDimension int       `gorm:"not null;default:0" json:"vector_dimension"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Document struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	KnowledgeBaseID uint      `gorm:"index;not null" json:"knowledge_base_id"`
	Title           string    `gorm:"size:255;not null" json:"title"`
	SourceType      string    `gorm:"size:64;not null" json:"source_type"`
	Status          string    `gorm:"size:32;not null" json:"status"`
	Content         string    `gorm:"type:longtext;not null" json:"content"`
	ErrorMessage    string    `gorm:"type:text" json:"error_message,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type DocumentChunk struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	KnowledgeBaseID uint      `gorm:"index;not null" json:"knowledge_base_id"`
	DocumentID      uint      `gorm:"index;not null" json:"document_id"`
	ChunkIndex      int       `gorm:"not null" json:"chunk_index"`
	ChunkType       string    `gorm:"size:32;not null;default:'text'" json:"chunk_type"`
	TableID         string    `gorm:"size:64;index" json:"table_id,omitempty"`
	PageNo          int       `gorm:"not null;default:0" json:"page_no"`
	Content         string    `gorm:"type:longtext;not null" json:"content"`
	TokenCount      int       `gorm:"not null;default:0" json:"token_count"`
	VectorPointID   string    `gorm:"size:64;uniqueIndex;not null" json:"vector_point_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
