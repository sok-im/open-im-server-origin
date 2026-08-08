package otelx

import (
	"context"
	"fmt"

	"github.com/openimsdk/tools/mcontext"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func StartServerSpan(ctx context.Context, tracerName, spanName string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	if !Enabled() {
		return ctx, trace.SpanFromContext(ctx)
	}
	attrs = append(commonContextAttrs(ctx), attrs...)
	return otel.Tracer(tracerName).Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attrs...),
	)
}

func StartWSSpan(ctx context.Context, tracerName string, reqIdentifier int32) (context.Context, trace.Span) {
	spanName := fmt.Sprintf("ws %d", reqIdentifier)
	return StartServerSpan(ctx, tracerName, spanName,
		attribute.String("messaging.system", "websocket"),
		attribute.Int("ws.req_identifier", int(reqIdentifier)),
	)
}

func EndSpan(span trace.Span, err error) {
	if span == nil || !span.SpanContext().IsValid() {
		return
	}
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
	}
	span.End()
}

func commonContextAttrs(ctx context.Context) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 2)
	if operationID := mcontext.GetOperationID(ctx); operationID != "" {
		attrs = append(attrs, attribute.String("operation.id", operationID))
	}
	if opUserID := mcontext.GetOpUserID(ctx); opUserID != "" {
		attrs = append(attrs, attribute.String("op.user.id", opUserID))
	}
	return attrs
}
