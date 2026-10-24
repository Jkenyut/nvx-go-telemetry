package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestInit_Disabled(t *testing.T) {
	shutdown, err := Init(context.Background(), WithEnabled(false))
	if err != nil {
		t.Fatalf("expected no error when disabled, got %v", err)
	}
	if shutdown == nil {
		t.Fatalf("expected non-nil shutdown function when disabled")
	}

	// Calling shutdown on disabled should not fail
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("expected no error on shutdown when disabled, got %v", err)
	}
}

func TestInit_InvalidConfig(t *testing.T) {
	// Nil config validation
	var nilCfg *Config
	if err := nilCfg.Validate(); err == nil {
		t.Fatalf("expected error for nil config Validate()")
	}

	// Empty endpoint
	_, err := Init(context.Background(), WithEnabled(true), WithEndpoint(""))
	if err == nil {
		t.Fatalf("expected error for empty endpoint")
	}

	// Endpoint with scheme
	_, err = Init(context.Background(), WithEnabled(true), WithEndpoint("http://localhost:4317"))
	if err == nil || !strings.Contains(err.Error(), "must not contain a URL scheme") {
		t.Fatalf("expected URL scheme rejection error, got: %v", err)
	}

	// Empty service name
	_, err = Init(context.Background(), WithEnabled(true), WithServiceName(""))
	if err == nil {
		t.Fatalf("expected error for empty service name")
	}

	// Invalid trace ratio
	_, err = Init(context.Background(), WithEnabled(true), WithTraceRatio(1.5))
	if err == nil {
		t.Fatalf("expected error for trace ratio > 1.0")
	}
	_, err = Init(context.Background(), WithEnabled(true), WithTraceRatio(-0.1))
	if err == nil {
		t.Fatalf("expected error for trace ratio < 0.0")
	}

	// Shutdown timeout default reset on zero or negative
	cfg := DefaultConfig()
	cfg.ShutdownTimeout = -1 * time.Second
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validate error: %v", err)
	}
	if cfg.ShutdownTimeout != 5*time.Second {
		t.Fatalf("expected reset to 5s, got %v", cfg.ShutdownTimeout)
	}
}

func TestInit_Idempotency(t *testing.T) {
	testLogger := slog.Default()

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
		WithLogger(testLogger),
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

	// Third shutdown call with nil context (must be nil-safe and idempotent)
	err3 := shutdown(nil)
	if err1 != err3 {
		t.Fatalf("expected nil-context shutdown to return idempotent error %v, got %v", err1, err3)
	}
}

func TestInit_FallbackLogger(t *testing.T) {
	// Call Init with nil context to ensure fallback to context.Background()
	shutdown, err := Init(nil,
		WithEnabled(true),
		WithEndpoint("localhost:4317"),
		WithServiceName("prod-service"),
		WithEnvironment("production"),
		WithShutdownTimeout(50*time.Millisecond),
		WithTraceEnabled(false),
		WithMetricsEnabled(false),
	)
	if err != nil {
		t.Fatalf("expected no init error for fallback logger, got %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Logf("shutdown: %v", err)
	}
}

func TestConfig_Defaults_And_Options(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Enabled || cfg.Endpoint != "localhost:4317" || cfg.ServiceName != "nvx-go-telemetry" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}

	headers := map[string]string{"key": "val"}
	logger := slog.Default()

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
		WithLogger(logger),
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
		cfg.Logger != logger {
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

	// Safe to call Start with nil context
	nilCtx, nilSpan := Start(nil, "nil-ctx-tracer", "nil-span")
	if nilCtx == nil || nilSpan == nil {
		t.Fatalf("expected valid context and span when Start called with nil context")
	}
	nilSpan.End()

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

	// 5. Test active recording TracerProvider to verify non-empty TraceID and SpanID
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	activeTracer := tp.Tracer("active-tracer")
	activeCtx, activeSpan := activeTracer.Start(context.Background(), "sample-span")
	defer activeSpan.End()

	activeTID := TraceID(activeCtx)
	activeSID := SpanID(activeCtx)
	if len(activeTID) != 32 {
		t.Fatalf("expected 32-hex trace ID, got %q", activeTID)
	}
	if len(activeSID) != 16 {
		t.Fatalf("expected 16-hex span ID, got %q", activeSID)
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

	// 7. Empty name validation
	if _, err := mm.RegisterCounter("", "empty name"); err == nil {
		t.Fatalf("expected error registering counter with empty name")
	}
	if _, err := mm.RegisterUpDownCounter("", "empty name"); err == nil {
		t.Fatalf("expected error registering up-down counter with empty name")
	}
	if _, err := mm.RegisterHistogram("", "empty name", "s"); err == nil {
		t.Fatalf("expected error registering histogram with empty name")
	}
	if _, err := mm.MeasureFunc("", "empty name", nil); err == nil {
		t.Fatalf("expected error registering gauge with empty name")
	}

	// 8. Test Instrument Caching Idempotency
	c1, err := mm.RegisterCounter("cached_counter", "desc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c2, err := mm.RegisterCounter("cached_counter", "desc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c1 != c2 {
		t.Fatalf("expected cached counter instance, got different pointers")
	}

	u1, err := mm.RegisterUpDownCounter("cached_updown", "desc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u2, err := mm.RegisterUpDownCounter("cached_updown", "desc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u1 != u2 {
		t.Fatalf("expected cached up-down counter instance, got different pointers")
	}

	h1, err := mm.RegisterHistogram("cached_hist", "desc", "ms")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	h2, err := mm.RegisterHistogram("cached_hist", "desc", "ms")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("expected cached histogram instance, got different pointers")
	}
}

func TestConfig_WithConfig(t *testing.T) {
	src := &Config{
		Enabled:         true,
		Endpoint:        "remote-otel:4317",
		Insecure:        false,
		Headers:         map[string]string{"auth": "bearer-token"},
		ServiceName:     "custom-app",
		Environment:     "production",
		ServiceVersion:  "1.0.0",
		EnableTrace:     true,
		EnableMetrics:   true,
		TraceRatio:      0.75,
		ShutdownTimeout: 10 * time.Second,
	}

	cfg := DefaultConfig()
	WithConfig(src)(cfg)

	if cfg.Endpoint != "remote-otel:4317" || cfg.Insecure != false || cfg.ServiceName != "custom-app" || cfg.TraceRatio != 0.75 {
		t.Fatalf("WithConfig did not apply expected fields: %+v", cfg)
	}
	// Check defensive copy
	src.Headers["auth"] = "mutated"
	if cfg.Headers["auth"] != "bearer-token" {
		t.Fatalf("expected cloned headers, got %q", cfg.Headers["auth"])
	}

	// Nil src should not panic
	WithConfig(nil)(cfg)
}

func TestInit_Concurrent(t *testing.T) {
	const goroutines = 8
	errCh := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			shutdown, err := Init(context.Background(),
				WithEnabled(true),
				WithServiceName("concurrent-test"),
				WithTraceEnabled(false),
				WithMetricsEnabled(false),
			)
			if err != nil {
				errCh <- err
				return
			}
			_ = shutdown(context.Background())
			errCh <- nil
		}(i)
	}

	for i := 0; i < goroutines; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("concurrent Init failed: %v", err)
		}
	}
}
