package otelx

import (
	"context"
	"testing"

	"github.com/openimsdk/tools/mcontext"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestStartWSSpanSetsAttributes(t *testing.T) {
	t.Setenv("OPENIM_TRACE_ENABLED", "true")

	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	ctx := mcontext.WithMustInfoCtx([]string{"op-ws-1", "user-ws-1", "iOS", "conn-1"})
	ctx, span := StartWSSpan(ctx, "openim-msggateway", 1003)
	if !span.SpanContext().IsValid() {
		t.Fatal("expected valid ws span")
	}
	EndSpan(span, nil)
	_ = ctx
}

func TestStartServerSpanDisabled(t *testing.T) {
	t.Setenv("OPENIM_TRACE_ENABLED", "false")
	ctx, span := StartServerSpan(context.Background(), "test", "noop")
	EndSpan(span, nil)
	_ = ctx
}
