package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

var initMu sync.Mutex

// Init sets up OpenTelemetry providers based on the provided options.
// It returns an idempotent shutdown function that should be deferred to ensure data is flushed.
func Init(ctx context.Context, opts ...Option) (func(context.Context) error, error) {
	initMu.Lock()
	defer initMu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}

	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating telemetry config: %w", err)
	}

	if !cfg.Enabled {
		// Observability is disabled; return a no-op shutdown function
		return func(context.Context) error { return nil }, nil
	}

	var shutdownFuncs []func(context.Context) error

	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		for i := len(shutdownFuncs) - 1; i >= 0; i-- {
			_ = shutdownFuncs[i](cleanupCtx)
		}
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	attrs := []attribute.KeyValue{
		semconv.ServiceNameKey.String(cfg.ServiceName),
	}
	if cfg.Environment != "" {
		attrs = append(attrs, semconv.DeploymentEnvironmentKey.String(cfg.Environment))
	}
	if cfg.ServiceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersionKey.String(cfg.ServiceVersion))
	}

	res, err := resource.New(ctx,
		resource.WithFromEnv(), // Allow injecting OTEL_RESOURCE_ATTRIBUTES from env
		resource.WithProcess(), // Include process information (PID, etc)
		resource.WithAttributes(attrs...),
	)
	if err != nil {
		if errors.Is(err, resource.ErrPartialResource) {
			logger.Warn("partial otel resource constructed", "error", err)
		} else {
			cleanup()
			return nil, fmt.Errorf("creating otel resource: %w", err)
		}
	}

	var (
		tracerProvider *sdktrace.TracerProvider
		meterProvider  *sdkmetric.MeterProvider
	)

	if cfg.EnableTrace {
		traceOpts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.Endpoint)}
		if cfg.Insecure {
			traceOpts = append(traceOpts, otlptracegrpc.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			traceOpts = append(traceOpts, otlptracegrpc.WithHeaders(cfg.Headers))
		}
		traceExporter, err := otlptracegrpc.New(ctx, traceOpts...)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("creating otlp trace exporter: %w", err)
		}

		// Explicit batch processor limiters for safety
		bsp := sdktrace.NewBatchSpanProcessor(
			traceExporter,
			sdktrace.WithMaxQueueSize(8192),          // Production recommended queue size
			sdktrace.WithMaxExportBatchSize(1024),    // Export larger batches
			sdktrace.WithBatchTimeout(5*time.Second), // Explicit batch timeout
		)
		tracerProvider = sdktrace.NewTracerProvider(
			// ParentBased sampler respects upstream sampling decisions
			sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.TraceRatio))),
			sdktrace.WithResource(res),
			sdktrace.WithSpanProcessor(bsp),
			// Explicit span limits to prevent OOM
			sdktrace.WithRawSpanLimits(sdktrace.SpanLimits{
				AttributeCountLimit: 128,
				EventCountLimit:     128,
				LinkCountLimit:      128,
			}),
		)
		shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
	}

	if cfg.EnableMetrics {
		metricOpts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(cfg.Endpoint)}
		if cfg.Insecure {
			metricOpts = append(metricOpts, otlpmetricgrpc.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			metricOpts = append(metricOpts, otlpmetricgrpc.WithHeaders(cfg.Headers))
		}
		metricExporter, err := otlpmetricgrpc.New(ctx, metricOpts...)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("creating otlp metric exporter: %w", err)
		}

		// Explicit batch processor limiters for safety
		metricReader := sdkmetric.NewPeriodicReader(
			metricExporter,
			sdkmetric.WithInterval(30*time.Second), // Optimal interval for production
			sdkmetric.WithTimeout(10*time.Second),  // Explicit export timeout
		)
		meterProvider = sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(metricReader),
		)
		shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
	}

	// Register global handlers only after all exporters and providers are successfully constructed
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Error("opentelemetry internal error", "error", err)
	}))

	// Set global propagator to tracecontext and baggage (W3C standard).
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	if tracerProvider != nil {
		otel.SetTracerProvider(tracerProvider)
	}
	if meterProvider != nil {
		otel.SetMeterProvider(meterProvider)
	}

	var (
		shutdownOnce sync.Once
		shutdownErr  error
	)

	shutdown := func(ctx context.Context) error {
		shutdownOnce.Do(func() {
			if ctx == nil {
				ctx = context.Background()
			}

			timeout := time.Duration(cfg.ShutdownTimeout) * time.Millisecond
			if timeout <= 0 {
				timeout = 5 * time.Second
			}

			shutdownCtx := ctx
			var cancel context.CancelFunc
			if _, hasDeadline := ctx.Deadline(); !hasDeadline {
				shutdownCtx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}

			var errs []error
			for i := len(shutdownFuncs) - 1; i >= 0; i-- {
				if err := shutdownFuncs[i](shutdownCtx); err != nil {
					errs = append(errs, err)
				}
			}
			shutdownErr = errors.Join(errs...)
		})
		return shutdownErr
	}

	return shutdown, nil
}
