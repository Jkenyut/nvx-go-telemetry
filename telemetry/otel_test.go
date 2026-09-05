package telemetry

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func TestInit_Disabled(t *testing.T) {
	shutdown, err := Init(context.Background(), WithEnabled(false))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if shutdown == nil {
		t.Fatalf("expected non-nil shutdown function")
	}

	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("expected no error on shutdown, got %v", err)
	}
}

func TestInit_Validation(t *testing.T) {
	tests := []struct {
		name    string
		opts    []Option
		wantErr bool
	}{
		{
			name: "empty endpoint when enabled",
			opts: []Option{
				WithEnabled(true),
				WithEndpoint(""),
			},
			wantErr: true,
		},
		{
			name: "empty service name when enabled",
			opts: []Option{
				WithEnabled(true),
				WithServiceName("   "),
			},
			wantErr: true,
		},
		{
			name: "negative trace ratio",
			opts: []Option{
				WithEnabled(true),
				WithTraceRatio(-0.1),
			},
			wantErr: true,
		},
		{
			name: "trace ratio greater than 1",
			opts: []Option{
				WithEnabled(true),
				WithTraceRatio(1.5),
			},
			wantErr: true,
		},
		{
			name: "valid configuration",
			opts: []Option{
				WithEnabled(false),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Init(context.Background(), tt.opts...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Init() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestInit_Enabled_Shutdown_Idempotent(t *testing.T) {
	var buf bytes.Buffer
	testLogger := zerolog.New(&buf).With().Timestamp().Logger()

	shutdown, err := Init(context.Background(),
		WithEnabled(true),
		WithEndpoint("localhost:4317"),
		WithServiceName("test-idempotent-service"),
		WithEnvironment("production"),
		WithServiceVersion("v1.2.3"),
		WithInsecure(true),
		WithHeaders(map[string]string{"authorization": "secret-token"}),
		WithTraceEnabled(true),
		WithMetricsEnabled(true),
		WithTraceRatio(0.5),
		WithShutdownTimeout(200*time.Millisecond),
		WithLogger(&testLogger),
	)
	if err != nil {
		t.Fatalf("expected no init error, got %v", err)
	}
	if shutdown == nil {
		t.Fatalf("expected non-nil shutdown function")
	}

	// First shutdown call
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err1 := shutdown(ctx)

	// Second shutdown call (should be idempotent, returning same cached result without panicking)
	err2 := shutdown(ctx)
	if err1 != err2 {
		t.Fatalf("expected idempotent shutdown error %v, got %v", err1, err2)
	}
}

func TestInit_FallbackLogger_ProdAndDev(t *testing.T) {
	// Test production branch (JSON writer to stdout)
	shutdownProd, err := Init(context.Background(),
		WithEnabled(true),
		WithEndpoint("localhost:4317"),
		WithServiceName("prod-service"),
		WithEnvironment("production"),
		WithShutdownTimeout(50*time.Millisecond),
		WithTraceEnabled(false),
		WithMetricsEnabled(false),
	)
	if err != nil {
		t.Fatalf("expected no init error for prod logger, got %v", err)
	}
	if err := shutdownProd(context.Background()); err != nil {
		t.Logf("shutdown prod: %v", err)
	}

	// Test development branch (ConsoleWriter to stderr)
	shutdownDev, err := Init(context.Background(),
		WithEnabled(true),
		WithEndpoint("localhost:4317"),
		WithServiceName("dev-service"),
		WithEnvironment("development"),
		WithShutdownTimeout(50*time.Millisecond),
		WithTraceEnabled(false),
		WithMetricsEnabled(false),
	)
	if err != nil {
		t.Fatalf("expected no init error for dev logger, got %v", err)
	}
	if err := shutdownDev(context.Background()); err != nil {
		t.Logf("shutdown dev: %v", err)
	}
}

func TestConfig_Defaults_And_Options(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Enabled || cfg.Endpoint != "localhost:4317" || cfg.ServiceName != "nvx-go-telemetry" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}

	headers := map[string]string{"key": "val"}
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	opts := []Option{
		WithEnabled(false),
		WithEndpoint("collector:4317"),
		WithServiceName("custom-service"),
		WithEnvironment("staging"),
		WithServiceVersion("v2.0.0"),
		WithTraceEnabled(false),
		WithMetricsEnabled(false),
		WithInsecure(false),
		WithHeaders(headers),
		WithTraceRatio(0.25),
		WithShutdownTimeout(3 * time.Second),
		WithLogger(&logger),
	}

	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.Enabled != false ||
		cfg.Endpoint != "collector:4317" ||
		cfg.ServiceName != "custom-service" ||
		cfg.Environment != "staging" ||
		cfg.ServiceVersion != "v2.0.0" ||
		cfg.EnableTrace != false ||
		cfg.EnableMetrics != false ||
		cfg.Insecure != false ||
		cfg.TraceRatio != 0.25 ||
		cfg.ShutdownTimeout != 3*time.Second ||
		cfg.Logger != &logger {
		t.Fatalf("options not applied correctly: %+v", cfg)
	}
}

func TestConfig_Headers_CloneSafety(t *testing.T) {
	origHeaders := map[string]string{"api-key": "secret"}
	cfg := DefaultConfig()
	WithHeaders(origHeaders)(cfg)

	// Mutate original map
	origHeaders["api-key"] = "tampered"
	origHeaders["new-key"] = "leaked"

	// Cloned map in config must remain untouched
	if cfg.Headers["api-key"] != "secret" {
		t.Fatalf("expected 'secret', got %q (data race / shallow copy bug)", cfg.Headers["api-key"])
	}
	if _, ok := cfg.Headers["new-key"]; ok {
		t.Fatalf("unexpected key found in config.Headers (data race / shallow copy bug)")
	}
}

func TestConfig_Masking(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Headers = map[string]string{
		"Authorization": "Bearer super-secret-token",
		"X-API-Key":     "my-secret-api-key",
	}

	str := cfg.String()
	goStr := cfg.GoString()

	if strings.Contains(str, "super-secret-token") || strings.Contains(str, "my-secret-api-key") {
		t.Fatalf("sensitive headers leaked in Config.String(): %s", str)
	}
	if strings.Contains(goStr, "super-secret-token") || strings.Contains(goStr, "my-secret-api-key") {
		t.Fatalf("sensitive headers leaked in Config.GoString(): %s", goStr)
	}
	if !strings.Contains(str, "[REDACTED]") {
		t.Fatalf("expected '[REDACTED]' in Config.String(): %s", str)
	}

	var nilCfg *Config
	if nilCfg.String() != "<nil>" {
		t.Fatalf("expected '<nil>', got %s", nilCfg.String())
	}
}

func TestTrace_Operations(t *testing.T) {
	// 1. Tracer fallback name
	trEmpty := Tracer("")
	if trEmpty == nil {
		t.Fatalf("expected non-nil tracer")
	}
	trNamed := Tracer("my-tracer")
	if trNamed == nil {
		t.Fatalf("expected non-nil tracer")
	}

	// 2. Start span
	ctx := context.Background()
	ctx, span := Start(ctx, "test-tracer", "test-span")
	if span == nil {
		t.Fatalf("expected non-nil span")
	}
	defer span.End()

	// 3. RecordError with nil and valid error
	RecordError(nil, errors.New("span is nil")) // should not panic
	RecordError(span, nil)                      // should not panic
	RecordError(span, errors.New("test error")) // should record properly

	// 4. TraceID and SpanID
	if TraceID(nil) != "" {
		t.Fatalf("expected empty string for nil context")
	}
	if SpanID(nil) != "" {
		t.Fatalf("expected empty string for nil context")
	}

	tid := TraceID(ctx)
	sid := SpanID(ctx)
	// Without an active recording tracer provider, it returns empty or no-op string
	t.Logf("Active TraceID: %q, SpanID: %q", tid, sid)

	// Context without span
	emptyCtx := context.Background()
	if TraceID(emptyCtx) != "" {
		t.Fatalf("expected empty TraceID for empty context")
	}
	if SpanID(emptyCtx) != "" {
		t.Fatalf("expected empty SpanID for empty context")
	}
}

func TestMetrics_Operations(t *testing.T) {
	// 1. NewMetricManager with fallback name
	mm := NewMetricManager("")
	if mm == nil || mm.Meter == nil {
		t.Fatalf("expected non-nil MetricManager with valid meter")
	}

	if mm.GetMeter() == nil {
		t.Fatalf("expected non-nil Meter from GetMeter()")
	}

	// 2. RegisterCounter with extra options
	counter, err := mm.RegisterCounter("http_requests_total", "Total HTTP requests", metric.WithUnit("1"))
	if err != nil {
		t.Fatalf("failed to register counter: %v", err)
	}
	counter.Add(context.Background(), 1)

	// 3. RegisterUpDownCounter with extra options
	upDown, err := mm.RegisterUpDownCounter("active_connections", "Number of active connections", metric.WithUnit("conn"))
	if err != nil {
		t.Fatalf("failed to register up-down counter: %v", err)
	}
	upDown.Add(context.Background(), 1)
	upDown.Add(context.Background(), -1)

	// 4. RegisterHistogram with extra options
	hist, err := mm.RegisterHistogram("request_duration_seconds", "Request latency", "s", metric.WithExplicitBucketBoundaries(0.1, 0.5, 1.0))
	if err != nil {
		t.Fatalf("failed to register histogram: %v", err)
	}
	hist.Record(context.Background(), 0.25)

	// 5. MeasureFunc
	gauge, err := mm.MeasureFunc("system_memory_bytes", "Memory usage in bytes", func(_ context.Context, obs metric.Int64Observer) error {
		obs.Observe(1024, metric.WithAttributes(attribute.String("type", "heap")))
		return nil
	})
	if err != nil {
		t.Fatalf("failed to register measure func: %v", err)
	}
	if gauge == nil {
		t.Fatalf("expected non-nil observable gauge")
	}

	// 6. Nil MetricManager safety
	var nilMM *MetricManager
	if nilMM.GetMeter() != nil {
		t.Fatalf("expected nil meter from nil MetricManager")
	}
	if _, err := nilMM.RegisterCounter("c", "d"); err == nil {
		t.Fatalf("expected error when registering counter on nil MetricManager")
	}
	if _, err := nilMM.RegisterUpDownCounter("u", "d"); err == nil {
		t.Fatalf("expected error when registering up-down counter on nil MetricManager")
	}
	if _, err := nilMM.RegisterHistogram("h", "d", "u"); err == nil {
		t.Fatalf("expected error when registering histogram on nil MetricManager")
	}
	if _, err := nilMM.MeasureFunc("g", "d", nil); err == nil {
		t.Fatalf("expected error when measuring func on nil MetricManager")
	}
}
