package telemetry

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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
	if err := nilCfg.Validate(); !errors.Is(err, ErrNilConfig) {
		t.Fatalf("expected ErrNilConfig for nil config Validate(), got: %v", err)
	}

	// Empty endpoint
	_, err := Init(context.Background(), WithEnabled(true), WithEndpoint(""))
	if !errors.Is(err, ErrEmptyEndpoint) {
		t.Fatalf("expected ErrEmptyEndpoint, got: %v", err)
	}

	// Endpoint with scheme
	_, err = Init(context.Background(), WithEnabled(true), WithEndpoint("http://localhost:4317"))
	if !errors.Is(err, ErrEndpointWithScheme) || !strings.Contains(err.Error(), "must not contain a URL scheme") {
		t.Fatalf("expected ErrEndpointWithScheme rejection error, got: %v", err)
	}

	// Empty service name
	_, err = Init(context.Background(), WithEnabled(true), WithServiceName(""))
	if !errors.Is(err, ErrEmptyServiceName) {
		t.Fatalf("expected ErrEmptyServiceName, got: %v", err)
	}

	// Invalid trace ratio
	_, err = Init(context.Background(), WithEnabled(true), WithTraceRatio(1.5))
	if !errors.Is(err, ErrInvalidTraceRatio) {
		t.Fatalf("expected ErrInvalidTraceRatio for trace ratio > 1.0, got: %v", err)
	}
	_, err = Init(context.Background(), WithEnabled(true), WithTraceRatio(-0.1))
	if !errors.Is(err, ErrInvalidTraceRatio) {
		t.Fatalf("expected ErrInvalidTraceRatio for trace ratio < 0.0, got: %v", err)
	}

	// Nil option should be safely ignored
	shutdown, err := Init(context.Background(), nil, WithEnabled(false))
	if err != nil || shutdown == nil {
		t.Fatalf("expected nil option to be ignored safely, got err=%v", err)
	}
	_ = shutdown(context.Background())

	// Shutdown timeout default reset on zero or negative
	cfg := DefaultConfig()
	cfg.ShutdownTimeout = -1
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validate error: %v", err)
	}
	if cfg.ShutdownTimeout != 5000 {
		t.Fatalf("expected reset to 5000ms, got %v", cfg.ShutdownTimeout)
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
		WithShutdownTimeout(200),
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
		WithShutdownTimeout(5000),
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
		WithShutdownTimeout(3000),
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
		cfg.ShutdownTimeout != 3000 ||
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

	// 6. Nil MetricManager & uninitialized Meter safety
	var nilMM *MetricManager
	if nilMM.GetMeter() != nil {
		t.Fatalf("expected nil meter from nil MetricManager")
	}
	if _, err := nilMM.RegisterCounter("c", "d"); !errors.Is(err, ErrUninitializedManager) {
		t.Fatalf("expected ErrUninitializedManager, got: %v", err)
	}
	if _, err := nilMM.RegisterFloat64Counter("fc", "d"); !errors.Is(err, ErrUninitializedManager) {
		t.Fatalf("expected ErrUninitializedManager, got: %v", err)
	}
	if _, err := nilMM.RegisterUpDownCounter("u", "d"); !errors.Is(err, ErrUninitializedManager) {
		t.Fatalf("expected ErrUninitializedManager, got: %v", err)
	}
	if _, err := nilMM.RegisterFloat64UpDownCounter("fu", "d"); !errors.Is(err, ErrUninitializedManager) {
		t.Fatalf("expected ErrUninitializedManager, got: %v", err)
	}
	if _, err := nilMM.RegisterHistogram("h", "d", "u"); !errors.Is(err, ErrUninitializedManager) {
		t.Fatalf("expected ErrUninitializedManager, got: %v", err)
	}
	if _, err := nilMM.RegisterInt64Histogram("ih", "d", "u"); !errors.Is(err, ErrUninitializedManager) {
		t.Fatalf("expected ErrUninitializedManager, got: %v", err)
	}
	if _, err := nilMM.MeasureFunc("g", "d", nil); !errors.Is(err, ErrUninitializedManager) {
		t.Fatalf("expected ErrUninitializedManager, got: %v", err)
	}
	if _, err := nilMM.MeasureFloat64Func("fg", "d", nil); !errors.Is(err, ErrUninitializedManager) {
		t.Fatalf("expected ErrUninitializedManager, got: %v", err)
	}

	uninitMM := NewMetricManagerWithMeter(nil)
	if _, err := uninitMM.RegisterCounter("c", "d"); !errors.Is(err, ErrUninitializedManager) {
		t.Fatalf("expected ErrUninitializedManager for nil meter, got: %v", err)
	}

	// 7. Empty name validation
	if _, err := mm.RegisterCounter("", "empty name"); !errors.Is(err, ErrEmptyMetricName) {
		t.Fatalf("expected ErrEmptyMetricName registering counter, got: %v", err)
	}
	if _, err := mm.RegisterFloat64Counter("   ", "empty name"); !errors.Is(err, ErrEmptyMetricName) {
		t.Fatalf("expected ErrEmptyMetricName registering float64 counter, got: %v", err)
	}
	if _, err := mm.RegisterUpDownCounter("", "empty name"); !errors.Is(err, ErrEmptyMetricName) {
		t.Fatalf("expected ErrEmptyMetricName registering up-down counter, got: %v", err)
	}
	if _, err := mm.RegisterFloat64UpDownCounter("", "empty name"); !errors.Is(err, ErrEmptyMetricName) {
		t.Fatalf("expected ErrEmptyMetricName registering float64 up-down counter, got: %v", err)
	}
	if _, err := mm.RegisterHistogram("", "empty name", "s"); !errors.Is(err, ErrEmptyMetricName) {
		t.Fatalf("expected ErrEmptyMetricName registering histogram, got: %v", err)
	}
	if _, err := mm.RegisterInt64Histogram("", "empty name", "ms"); !errors.Is(err, ErrEmptyMetricName) {
		t.Fatalf("expected ErrEmptyMetricName registering int64 histogram, got: %v", err)
	}
	if _, err := mm.MeasureFunc("", "empty name", nil); !errors.Is(err, ErrEmptyMetricName) {
		t.Fatalf("expected ErrEmptyMetricName registering gauge, got: %v", err)
	}
	if _, err := mm.MeasureFloat64Func("", "empty name", nil); !errors.Is(err, ErrEmptyMetricName) {
		t.Fatalf("expected ErrEmptyMetricName registering float64 gauge, got: %v", err)
	}

	// 8. Test Instrument Caching Idempotency (including MeasureFunc)
	g1, err := mm.MeasureFunc("cached_gauge", "desc", func(_ context.Context, obs metric.Int64Observer) error {
		obs.Observe(42)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	g2, err := mm.MeasureFunc("cached_gauge", "desc", func(_ context.Context, obs metric.Int64Observer) error {
		obs.Observe(99)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g1 != g2 {
		t.Fatalf("expected cached observable gauge instance, got different pointers")
	}
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
		ShutdownTimeout: 10000,
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

func TestConfig_EnvOverrides(t *testing.T) {
	t.Setenv("OTEL_ENABLED", "false")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "otel-collector:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "false")
	t.Setenv("OTEL_SERVICE_NAME", "env-service")
	t.Setenv("OTEL_ENVIRONMENT", "staging")
	t.Setenv("OTEL_SERVICE_VERSION", "v9.9.9")
	t.Setenv("OTEL_ENABLE_TRACE", "false")
	t.Setenv("OTEL_ENABLE_METRICS", "false")
	t.Setenv("OTEL_TRACE_RATIO", "0.42")
	t.Setenv("OTEL_SHUTDOWN_TIMEOUT", "7s")

	cfg := DefaultConfig()
	if cfg.Enabled != false ||
		cfg.Endpoint != "otel-collector:4317" ||
		cfg.Insecure != false ||
		cfg.ServiceName != "env-service" ||
		cfg.Environment != "staging" ||
		cfg.ServiceVersion != "v9.9.9" ||
		cfg.EnableTrace != false ||
		cfg.EnableMetrics != false ||
		cfg.TraceRatio != 0.42 ||
		cfg.ShutdownTimeout != 7000 {
		t.Fatalf("env overrides not applied properly: %+v", cfg)
	}

	// Verify integer millisecond format and graceful ignoring of invalid env values
	t.Setenv("OTEL_SHUTDOWN_TIMEOUT", "3500")
	t.Setenv("OTEL_TRACE_RATIO", "not-a-float")
	t.Setenv("OTEL_ENABLED", "invalid-bool")
	cfg2 := DefaultConfig()
	if cfg2.ShutdownTimeout != 3500 {
		t.Fatalf("expected 3500ms shutdown timeout, got %d", cfg2.ShutdownTimeout)
	}
	if cfg2.TraceRatio != 1.0 {
		t.Fatalf("expected default trace ratio 1.0 on invalid env, got %f", cfg2.TraceRatio)
	}
	if cfg2.Enabled != true {
		t.Fatalf("expected default enabled=true on invalid env, got %v", cfg2.Enabled)
	}
}

func TestConfig_TLSAndCompression(t *testing.T) {
	origTLS := &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: "otel.example.internal",
	}

	cfg := DefaultConfig()
	WithTLSConfig(origTLS)(cfg)
	WithCompression("gzip")(cfg)

	if cfg.Insecure {
		t.Fatal("expected WithTLSConfig to disable Insecure")
	}
	if cfg.TLSConfig == nil || cfg.TLSConfig.ServerName != "otel.example.internal" {
		t.Fatalf("unexpected TLSConfig: %+v", cfg.TLSConfig)
	}
	if cfg.Compression != "gzip" {
		t.Fatalf("expected compression gzip, got %q", cfg.Compression)
	}

	// Verify defensive clone of TLSConfig
	origTLS.ServerName = "tampered.internal"
	if cfg.TLSConfig.ServerName != "otel.example.internal" {
		t.Fatalf("expected cloned TLSConfig, got %q", cfg.TLSConfig.ServerName)
	}

	// Verify WithConfig clones TLSConfig and copies Compression
	copied := DefaultConfig()
	WithConfig(cfg)(copied)
	if copied.TLSConfig == nil || copied.TLSConfig == cfg.TLSConfig || copied.Compression != "gzip" {
		t.Fatalf("expected WithConfig to clone TLSConfig and copy Compression")
	}

	// Verify WithTLSConfig(nil) clears TLSConfig
	WithTLSConfig(nil)(cfg)
	if cfg.TLSConfig != nil {
		t.Fatal("expected nil TLSConfig after WithTLSConfig(nil)")
	}

	// Verify Init succeeds with custom TLSConfig + gzip compression
	shutdown1, err := Init(context.Background(),
		WithEndpoint("localhost:4317"),
		WithServiceName("tls-service"),
		WithEnvironment("production"),
		WithServiceVersion("v1.0.0"),
		WithTLSConfig(&tls.Config{MinVersion: tls.VersionTLS13}),
		WithCompression("gzip"),
		WithShutdownTimeout(100),
	)
	if err != nil {
		t.Fatalf("expected Init with custom TLSConfig to succeed, got: %v", err)
	}
	_ = shutdown1(context.Background())

	// Verify Init succeeds with system root TLS (Insecure=false, TLSConfig=nil) and compression="none"
	shutdown2, err := Init(context.Background(),
		WithEndpoint("localhost:4317"),
		WithServiceName("system-tls-service"),
		WithInsecure(false),
		WithCompression("none"),
		WithShutdownTimeout(100),
	)
	if err != nil {
		t.Fatalf("expected Init with system root TLS to succeed, got: %v", err)
	}
	_ = shutdown2(context.Background())
}

func TestInit_ReinitializationShutdown(t *testing.T) {
	// First Init intentionally without calling shutdown yet
	s1, err := Init(context.Background(),
		WithServiceName("reinit-svc-1"),
		WithTraceEnabled(true),
		WithMetricsEnabled(true),
		WithShutdownTimeout(100),
	)
	if err != nil {
		t.Fatalf("first Init failed: %v", err)
	}

	// Second Init should cleanly shut down previous providers before registering new ones
	s2, err := Init(context.Background(),
		WithServiceName("reinit-svc-2"),
		WithTraceEnabled(false),
		WithMetricsEnabled(false),
		WithShutdownTimeout(100),
	)
	if err != nil {
		t.Fatalf("second Init failed: %v", err)
	}

	// Calling s1 afterwards must remain safe and idempotent
	_ = s1(context.Background())
	if err := s2(context.Background()); err != nil {
		t.Fatalf("second shutdown failed: %v", err)
	}
}

func TestMetrics_EndToEnd_WithManualReader(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer func() { _ = mp.Shutdown(context.Background()) }()

	mm := NewMetricManagerWithMeter(mp.Meter("e2e-meter"))

	ctx := context.Background()

	ic, err := mm.RegisterCounter("e2e_int_counter", "int counter")
	if err != nil {
		t.Fatalf("RegisterCounter failed: %v", err)
	}
	ic.Add(ctx, 3)

	fc1, err := mm.RegisterFloat64Counter("e2e_float_counter", "float counter")
	if err != nil {
		t.Fatalf("RegisterFloat64Counter failed: %v", err)
	}
	fc2, err := mm.RegisterFloat64Counter("e2e_float_counter", "float counter")
	if err != nil || fc1 != fc2 {
		t.Fatalf("expected cached Float64Counter, err=%v", err)
	}
	fc1.Add(ctx, 1.5)

	iu, err := mm.RegisterUpDownCounter("e2e_int_updown", "int updown")
	if err != nil {
		t.Fatalf("RegisterUpDownCounter failed: %v", err)
	}
	iu.Add(ctx, 2)

	fu1, err := mm.RegisterFloat64UpDownCounter("e2e_float_updown", "float updown")
	if err != nil {
		t.Fatalf("RegisterFloat64UpDownCounter failed: %v", err)
	}
	fu2, err := mm.RegisterFloat64UpDownCounter("e2e_float_updown", "float updown")
	if err != nil || fu1 != fu2 {
		t.Fatalf("expected cached Float64UpDownCounter, err=%v", err)
	}
	fu1.Add(ctx, -0.5)

	fh, err := mm.RegisterHistogram("e2e_float_hist", "float hist", "s")
	if err != nil {
		t.Fatalf("RegisterHistogram failed: %v", err)
	}
	fh.Record(ctx, 0.25)

	ih1, err := mm.RegisterInt64Histogram("e2e_int_hist", "int hist", "ms")
	if err != nil {
		t.Fatalf("RegisterInt64Histogram failed: %v", err)
	}
	ih2, err := mm.RegisterInt64Histogram("e2e_int_hist", "int hist", "ms")
	if err != nil || ih1 != ih2 {
		t.Fatalf("expected cached Int64Histogram, err=%v", err)
	}
	ih1.Record(ctx, 45)

	if _, err := mm.MeasureFunc("e2e_int_gauge", "int gauge", func(_ context.Context, obs metric.Int64Observer) error {
		obs.Observe(10)
		return nil
	}); err != nil {
		t.Fatalf("MeasureFunc failed: %v", err)
	}

	fg1, err := mm.MeasureFloat64Func("e2e_float_gauge", "float gauge", func(_ context.Context, obs metric.Float64Observer) error {
		obs.Observe(3.14)
		return nil
	})
	if err != nil {
		t.Fatalf("MeasureFloat64Func failed: %v", err)
	}
	fg2, err := mm.MeasureFloat64Func("e2e_float_gauge", "float gauge", nil)
	if err != nil || fg1 != fg2 {
		t.Fatalf("expected cached Float64ObservableGauge, err=%v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("ManualReader.Collect failed: %v", err)
	}
	if len(rm.ScopeMetrics) == 0 {
		t.Fatal("expected collected ScopeMetrics")
	}

	collected := make(map[string]bool)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			collected[m.Name] = true
		}
	}

	expected := []string{
		"e2e_int_counter",
		"e2e_float_counter",
		"e2e_int_updown",
		"e2e_float_updown",
		"e2e_float_hist",
		"e2e_int_hist",
		"e2e_int_gauge",
		"e2e_float_gauge",
	}
	for _, name := range expected {
		if !collected[name] {
			t.Errorf("expected metric %q to be collected", name)
		}
	}
}

func TestDefaultSpanLimits_PreservesAttributeValues(t *testing.T) {
	limits := defaultSpanLimits()
	if limits.AttributeValueLengthLimit != defaultAttributeValueLengthLimit {
		t.Fatalf("AttributeValueLengthLimit = %d, want %d", limits.AttributeValueLengthLimit, defaultAttributeValueLengthLimit)
	}
	if limits.AttributeCountLimit <= 0 || limits.AttributePerEventCountLimit <= 0 {
		t.Fatalf("expected positive attribute count limits, got %+v", limits)
	}

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(recorder),
		sdktrace.WithRawSpanLimits(limits),
	)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	_, span := tp.Tracer("test-tracer").Start(context.Background(), "DB SELECT")
	span.SetAttributes(
		attribute.String("db.system", "postgresql"),
		attribute.String("db.operation", "SELECT"),
		attribute.String("db.statement", "SELECT 1"),
	)
	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}

	got := make(map[string]string, len(spans[0].Attributes()))
	for _, attr := range spans[0].Attributes() {
		got[string(attr.Key)] = attr.Value.AsString()
	}

	if got["db.system"] != "postgresql" {
		t.Errorf("db.system = %q, want %q", got["db.system"], "postgresql")
	}
	if got["db.operation"] != "SELECT" {
		t.Errorf("db.operation = %q, want %q", got["db.operation"], "SELECT")
	}
	if got["db.statement"] != "SELECT 1" {
		t.Errorf("db.statement = %q, want %q", got["db.statement"], "SELECT 1")
	}
}
