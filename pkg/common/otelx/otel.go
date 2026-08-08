package otelx

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

var (
	initOnce     sync.Once
	initErr      error
	initShutdown func(context.Context) error
)

func InitTracerProvider(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	if !traceEnabled() {
		return nil, nil
	}

	initOnce.Do(func() {
		endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
		if endpoint == "" {
			endpoint = "127.0.0.1:4317"
		}

		exp, err := otlptracegrpc.New(
			ctx,
			otlptracegrpc.WithEndpoint(endpoint),
			otlptracegrpc.WithInsecure(),
		)
		if err != nil {
			initErr = err
			return
		}

		res, err := resource.New(
			ctx,
			resource.WithAttributes(semconv.ServiceName(serviceName)),
		)
		if err != nil {
			initErr = err
			return
		}

		tp := sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(traceSampleRatio()))),
			sdktrace.WithBatcher(exp),
		)

		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))
		initShutdown = tp.Shutdown
	})

	return initShutdown, initErr
}

func Enabled() bool {
	return traceEnabled()
}

func SampleRatio() float64 {
	return traceSampleRatio()
}

func traceEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("OPENIM_TRACE_ENABLED")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// traceSampleRatio resolves sampling from:
//  1. OPENIM_TRACE_SAMPLE_RATIO
//  2. OTEL_TRACES_SAMPLER_ARG
//  3. OPENIM_ENV=production|prod -> 0.1, otherwise 1.0 (dev-friendly)
func traceSampleRatio() float64 {
	if raw := strings.TrimSpace(os.Getenv("OPENIM_TRACE_SAMPLE_RATIO")); raw != "" {
		return clampRatio(parseRatio(raw, 1))
	}
	if raw := strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER_ARG")); raw != "" {
		return clampRatio(parseRatio(raw, 1))
	}
	env := strings.TrimSpace(strings.ToLower(os.Getenv("OPENIM_ENV")))
	if env == "production" || env == "prod" {
		return 0.1
	}
	return 1
}

func parseRatio(raw string, fallback float64) float64 {
	ratio, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return ratio
}

func clampRatio(ratio float64) float64 {
	if ratio < 0 {
		return 0
	}
	if ratio > 1 {
		return 1
	}
	return ratio
}
