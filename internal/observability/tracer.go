package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type Shutdown func(context.Context) error

func NewProvider(ctx context.Context, cfg Config) (*sdktrace.TracerProvider, Shutdown, error) {
	setEventBodyLimit(cfg.EventBodyLimit)

	if !cfg.Enabled {
		provider := sdktrace.NewTracerProvider()
		otel.SetTracerProvider(provider)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))

		return provider, provider.Shutdown, nil
	}
	if cfg.Endpoint == "" {
		return nil, nil, fmt.Errorf("enabled tracing requires a non-empty endpoint")
	}
	if cfg.ProjectName == "" {
		return nil, nil, fmt.Errorf("enabled tracing requires a non-empty project name")
	}

	options := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(cfg.Endpoint)}
	if len(cfg.Headers) > 0 {
		options = append(options, otlptracehttp.WithHeaders(cfg.Headers))
	}

	exporter, err := otlptracehttp.New(ctx, options...)
	if err != nil {
		return nil, nil, fmt.Errorf("create otlp trace exporter: %w", err)
	}

	res, err := resource.New(
		ctx,
		resource.WithAttributes(
			attribute.String("service.name", ServiceName),
			attribute.String(ProjectNameAttribute, cfg.ProjectName),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("build tracer resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return provider, provider.Shutdown, nil
}
