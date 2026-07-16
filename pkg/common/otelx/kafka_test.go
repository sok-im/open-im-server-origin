package otelx

import (
	"context"
	"testing"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestInjectAndExtractTraceHeaders(t *testing.T) {
	t.Setenv("OPENIM_TRACE_ENABLED", "true")

	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	ctx, span := otel.Tracer("test").Start(context.Background(), "produce")
	defer span.End()

	headers := InjectTraceHeaders(ctx, nil)
	if len(headers) == 0 {
		t.Fatal("expected trace headers to be injected")
	}

	gotParent := ""
	for _, header := range headers {
		if string(header.Key) == traceParentHeaderKey {
			gotParent = string(header.Value)
		}
	}
	if gotParent == "" {
		t.Fatal("expected traceparent header")
	}

	extracted := ExtractTraceContext(context.Background(), toPtrHeaders(headers))
	if !trace.SpanFromContext(extracted).SpanContext().IsValid() {
		t.Fatal("expected valid span context after extraction")
	}
}

func toPtrHeaders(headers []sarama.RecordHeader) []*sarama.RecordHeader {
	out := make([]*sarama.RecordHeader, 0, len(headers))
	for i := range headers {
		out = append(out, &headers[i])
	}
	return out
}
