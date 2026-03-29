package phoenix

import "time"

type TraceEnvelope struct {
	ProjectName string      `json:"project_name"`
	TraceID     string      `json:"trace_id"`
	RootSpan    *TraceSpan  `json:"root_span,omitempty"`
	Spans       []TraceSpan `json:"spans"`
	StartTime   time.Time   `json:"start_time"`
	EndTime     time.Time   `json:"end_time"`
}

type TraceSpan struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	TraceID       string         `json:"trace_id"`
	SpanID        string         `json:"span_id"`
	ParentID      string         `json:"parent_id,omitempty"`
	SpanKind      string         `json:"span_kind,omitempty"`
	StartTime     time.Time      `json:"start_time"`
	EndTime       time.Time      `json:"end_time"`
	StatusCode    string         `json:"status_code,omitempty"`
	StatusMessage string         `json:"status_message,omitempty"`
	Attributes    map[string]any `json:"attributes,omitempty"`
	Events        []TraceEvent   `json:"events,omitempty"`
}

type TraceEvent struct {
	Name       string         `json:"name"`
	Timestamp  time.Time      `json:"timestamp"`
	Attributes map[string]any `json:"attributes,omitempty"`
}
