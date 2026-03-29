package observability

import (
	"context"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var eventBodyLimit atomic.Int64

func init() {
	eventBodyLimit.Store(DefaultEventBodyLimit)
}

func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	options := make([]trace.SpanStartOption, 0, 1)
	if len(attrs) > 0 {
		options = append(options, trace.WithAttributes(attrs...))
	}

	return otel.Tracer(InstrumentationName).Start(ctx, name, options...)
}

func RecordError(span trace.Span, err error) {
	if err == nil {
		return
	}

	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

func TextAttribute(key, value string) attribute.KeyValue {
	return attribute.String(key, truncateText(value))
}

func TextListAttribute(key string, values []string) attribute.KeyValue {
	return TextAttribute(key, strings.Join(values, "\n---\n"))
}

func setEventBodyLimit(limit int) {
	if limit <= 0 {
		limit = DefaultEventBodyLimit
	}
	eventBodyLimit.Store(int64(limit))
}

func truncateText(value string) string {
	limit := int(eventBodyLimit.Load())
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}

	runes := []rune(value)
	return string(runes[:limit]) + "...(truncated)"
}
