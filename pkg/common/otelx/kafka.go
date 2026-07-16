package otelx

import (
	"context"
	"fmt"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	traceParentHeaderKey = "traceparent"
	traceStateHeaderKey  = "tracestate"
)

type kafkaHeaderCarrier struct {
	headers []sarama.RecordHeader
}

func (c *kafkaHeaderCarrier) Get(key string) string {
	for i := range c.headers {
		if string(c.headers[i].Key) == key {
			return string(c.headers[i].Value)
		}
	}
	return ""
}

func (c *kafkaHeaderCarrier) Set(key, value string) {
	for i := range c.headers {
		if string(c.headers[i].Key) == key {
			c.headers[i].Value = []byte(value)
			return
		}
	}
	c.headers = append(c.headers, sarama.RecordHeader{
		Key:   []byte(key),
		Value: []byte(value),
	})
}

func (c *kafkaHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c.headers))
	for i := range c.headers {
		keys = append(keys, string(c.headers[i].Key))
	}
	return keys
}

func InjectTraceHeaders(ctx context.Context, headers []sarama.RecordHeader) []sarama.RecordHeader {
	if !Enabled() {
		return headers
	}
	carrier := &kafkaHeaderCarrier{headers: headers}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier.headers
}

func ExtractTraceContext(ctx context.Context, headers []*sarama.RecordHeader) context.Context {
	if !Enabled() || len(headers) == 0 {
		return ctx
	}
	recordHeaders := make([]sarama.RecordHeader, 0, len(headers))
	for _, header := range headers {
		if header == nil {
			continue
		}
		recordHeaders = append(recordHeaders, *header)
	}
	carrier := &kafkaHeaderCarrier{headers: recordHeaders}
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{
		traceParentHeaderKey: carrier.Get(traceParentHeaderKey),
		traceStateHeaderKey:  carrier.Get(traceStateHeaderKey),
	})
}

func StartKafkaConsumerSpan(ctx context.Context, tracerName, topic string, partition int32, offset int64) (context.Context, trace.Span) {
	if !Enabled() {
		return ctx, trace.SpanFromContext(ctx)
	}
	tracer := otel.Tracer(tracerName)
	spanName := fmt.Sprintf("kafka consume %s", topic)
	attrs := append(commonContextAttrs(ctx),
		attribute.String("messaging.system", "kafka"),
		attribute.String("messaging.destination", topic),
		attribute.Int64("messaging.kafka.partition", int64(partition)),
		attribute.Int64("messaging.kafka.offset", offset),
	)
	return tracer.Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attrs...),
	)
}

// StartKafkaBatchProcessSpan spans the real (asynchronous, batched) processing of Kafka
// messages, as opposed to the cheap enqueue step. A batcher shard groups messages by a
// single conversationID, so parentFrom (a representative message's context) is used to
// continue the upstream producer trace instead of starting an orphan root span.
func StartKafkaBatchProcessSpan(ctx, parentFrom context.Context, tracerName, key string, batchSize int) (context.Context, trace.Span) {
	if !Enabled() {
		return ctx, trace.SpanFromContext(ctx)
	}
	if sc := trace.SpanContextFromContext(parentFrom); sc.IsValid() {
		ctx = trace.ContextWithRemoteSpanContext(ctx, sc)
	}
	attrs := append(commonContextAttrs(ctx),
		attribute.String("messaging.system", "kafka"),
		attribute.String("messaging.operation", "process"),
		attribute.String("messaging.kafka.key", key),
		attribute.Int("messaging.batch.message_count", batchSize),
	)
	return otel.Tracer(tracerName).Start(ctx, "kafka batch process",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attrs...),
	)
}

func EndKafkaConsumerSpan(span trace.Span, err error) {
	EndSpan(span, err)
}
