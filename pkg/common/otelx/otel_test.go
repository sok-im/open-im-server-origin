package otelx

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/openimsdk/protocol/constant"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestTraceEnabled(t *testing.T) {
	t.Setenv("OPENIM_TRACE_ENABLED", "true")
	if !traceEnabled() {
		t.Fatal("expected trace to be enabled")
	}

	t.Setenv("OPENIM_TRACE_ENABLED", "false")
	if traceEnabled() {
		t.Fatal("expected trace to be disabled")
	}
}

func TestTraceSampleRatio(t *testing.T) {
	t.Setenv("OPENIM_ENV", "")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "")

	t.Setenv("OPENIM_TRACE_SAMPLE_RATIO", "0.5")
	if got := traceSampleRatio(); got != 0.5 {
		t.Fatalf("traceSampleRatio() = %v, want 0.5", got)
	}

	t.Setenv("OPENIM_TRACE_SAMPLE_RATIO", "bad")
	if got := traceSampleRatio(); got != 1 {
		t.Fatalf("traceSampleRatio() = %v, want 1", got)
	}

	t.Setenv("OPENIM_TRACE_SAMPLE_RATIO", "")
	t.Setenv("OPENIM_ENV", "production")
	if got := traceSampleRatio(); got != 0.1 {
		t.Fatalf("production default sample ratio = %v, want 0.1", got)
	}

	t.Setenv("OPENIM_ENV", "dev")
	if got := traceSampleRatio(); got != 1 {
		t.Fatalf("dev default sample ratio = %v, want 1", got)
	}
}

func TestGinMiddlewareSetsAttributes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	exporter := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(exporter)
	t.Cleanup(func() {
		_ = exporter.Shutdown(t.Context())
	})

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(constant.OperationID, "op-123")
		c.Set(constant.OpUserID, "user-456")
		c.Next()
	})
	r.Use(GinMiddleware("openim-api-test"))
	r.GET("/user/get_users_info", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/user/get_users_info", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestInitTracerProviderDisabled(t *testing.T) {
	t.Setenv("OPENIM_TRACE_ENABLED", "false")
	shutdown, err := InitTracerProvider(t.Context(), "openim-api")
	if err != nil {
		t.Fatalf("InitTracerProvider() error = %v", err)
	}
	if shutdown != nil {
		t.Fatal("expected nil shutdown when tracing disabled")
	}
}

func TestEnabled(t *testing.T) {
	t.Setenv("OPENIM_TRACE_ENABLED", os.Getenv("OPENIM_TRACE_ENABLED"))
	if Enabled() != traceEnabled() {
		t.Fatal("Enabled() should match traceEnabled()")
	}
}
