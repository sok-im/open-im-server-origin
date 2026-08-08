package otelx

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/openimsdk/protocol/constant"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func GinMiddleware(serviceName string) gin.HandlerFunc {
	tracer := otel.Tracer(serviceName)
	propagator := otel.GetTextMapPropagator()
	return func(c *gin.Context) {
		// Continue any upstream trace (gateway / frontend) instead of always starting a root span.
		ctx := propagator.Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))

		route := c.FullPath()
		spanName := c.Request.Method + " " + normalizeRoute(route, http.StatusOK)
		ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindServer))
		c.Request = c.Request.WithContext(ctx)

		c.Next()

		status := c.Writer.Status()
		span.SetAttributes(
			attribute.String("http.method", c.Request.Method),
			// Never emit raw URL paths (may carry userIDs / tokens) as span attributes.
			attribute.String("http.route", normalizeRoute(c.FullPath(), status)),
			attribute.Int("http.status_code", status),
		)

		if operationID, ok := c.Get(constant.OperationID); ok {
			if op, ok := operationID.(string); ok && op != "" {
				span.SetAttributes(attribute.String("operation.id", op))
			}
		}
		if opUserID, ok := c.Get(constant.OpUserID); ok {
			if uid, ok := opUserID.(string); ok && uid != "" {
				span.SetAttributes(attribute.String("op.user.id", uid))
			}
		}

		if status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, "http status "+strconv.Itoa(status))
		}
		span.End()
	}
}

// normalizeRoute keeps span cardinality bounded by using Gin's route template,
// mapping unmatched requests to stable placeholders instead of raw paths.
func normalizeRoute(fullPath string, status int) string {
	if fullPath != "" {
		return fullPath
	}
	if status == http.StatusNotFound {
		return "<404>"
	}
	return "<unmatched>"
}
