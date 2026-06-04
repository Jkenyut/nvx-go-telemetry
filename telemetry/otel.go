package telemetry

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
)

// Init sets up OpenTelemetry providers based on the provided options.
// It returns a shutdown function that should be deferred to ensure data is flushed.
func Init(ctx context.Context, opts ...Option) (func(context.Context) error, error) {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	if !cfg.Enabled {
		// Observability is disabled; return a no-op shutdown function
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx,
		resource.WithFromEnv(), // Best practice: allow injecting OTEL_RESOURCE_ATTRIBUTES from env
		resource.WithProcess(), // Best practice: include process information (PID, etc)
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
		),
	)
	if err != nil {
		return nil, err
	}

	var shutdownFuncs []func(context.Context) error
	shutdown := func(ctx context.Context) error {
		var errs []error
		for i := len(shutdownFuncs) - 1; i >= 0; i-- {
			// Add a timeout to shutdown to prevent hanging
			shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := shutdownFuncs[i](shutdownCtx); err != nil {
				errs = append(errs, err)
			}
			cancel()
		}
		return errors.Join(errs...)
	}

	// Set global propagator to tracecontext and baggage (W3C standard).
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	if cfg.EnableTrace {
		traceOpts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.Endpoint)}
		if cfg.Insecure {
			traceOpts = append(traceOpts, otlptracegrpc.WithInsecure())
		}
		traceExporter, err := otlptracegrpc.New(ctx, traceOpts...)
		if err != nil {
			return shutdown, err
		}
		// Explicit batch processor limiters for safety
		bsp := sdktrace.NewBatchSpanProcessor(
			traceExporter,
			sdktrace.WithMaxQueueSize(8192),          // Production recommended queue size
			sdktrace.WithMaxExportBatchSize(1024),    // Export larger batches
			sdktrace.WithBatchTimeout(5*time.Second), // Explicit batch timeout
		)
		tracerProvider := sdktrace.NewTracerProvider(
			// Best practice: ParentBased sampler respects upstream sampling decisions
			// Combined with TraceIDRatioBased to allow dropping traffic in production
			sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.TraceRatio))),
			sdktrace.WithResource(res),
			sdktrace.WithSpanProcessor(bsp),
			// Best practice: Explicit span limits to prevent OOM
			sdktrace.WithRawSpanLimits(sdktrace.SpanLimits{
				AttributeCountLimit: 128,
				EventCountLimit:     128,
				LinkCountLimit:      128,
			}),
		)
		otel.SetTracerProvider(tracerProvider)
		shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
	}

	if cfg.EnableMetrics {
		metricOpts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(cfg.Endpoint)}
		if cfg.Insecure {
			metricOpts = append(metricOpts, otlpmetricgrpc.WithInsecure())
		}
		metricExporter, err := otlpmetricgrpc.New(ctx, metricOpts...)
		if err != nil {
			return shutdown, err
		}
		// Explicit batch processor limiters for safety
		metricReader := sdkmetric.NewPeriodicReader(
			metricExporter,
			sdkmetric.WithInterval(30*time.Second), // Optimal interval for production
			sdkmetric.WithTimeout(10*time.Second),  // Explicit export timeout
		)
		meterProvider := sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(metricReader),
		)
		otel.SetMeterProvider(meterProvider)
		shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
	}

	return shutdown, nil
}
