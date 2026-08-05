package telemetry

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/diode"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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

	var shutdownFuncs []func(context.Context) error

	var logger *zerolog.Logger
	if cfg.Logger == nil {
		// Fallback if no logger injected
		// Check for environment-specific log level
		level := zerolog.DebugLevel
		isProd := cfg.Environment == "production" || cfg.Environment == "prod"
		if isProd {
			level = zerolog.InfoLevel
		}
		if envLevelStr := os.Getenv("LOG_LEVEL"); envLevelStr != "" {
			if parsedLevel, err := zerolog.ParseLevel(envLevelStr); err == nil {
				level = parsedLevel
			}
		}
		zerolog.SetGlobalLevel(level)

		var out zerolog.ConsoleWriter
		if cfg.Environment == "production" || cfg.Environment == "prod" {
			// JSON format for production
			out = zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339, NoColor: true}
		} else {
			// Pretty format for development
			out = zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339, NoColor: false}
		}

		wr := diode.NewWriter(out, 1000, 10*time.Millisecond, func(_ int) {})
		l := zerolog.New(wr).
			With().
			Timestamp().
			Str("service", cfg.ServiceName).
			Logger()

		// Caller is expensive. Only enable it in non-production environments.
		if !isProd {
			l = l.With().Caller().Logger()
		}

		logger = &l

		shutdownFuncs = append(shutdownFuncs, func(context.Context) error {
			_ = wr.Close()
			return nil
		})
	} else {
		logger = cfg.Logger
	}

	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Error().Err(err).Msg("opentelemetry internal error")
	}))

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
		resource.WithFromEnv(), // Best practice: allow injecting OTEL_RESOURCE_ATTRIBUTES from env
		resource.WithProcess(), // Best practice: include process information (PID, etc)
		resource.WithAttributes(attrs...),
	)
	if err != nil {
		return nil, err
	}

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
		if len(cfg.Headers) > 0 {
			traceOpts = append(traceOpts, otlptracegrpc.WithHeaders(cfg.Headers))
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
		if len(cfg.Headers) > 0 {
			metricOpts = append(metricOpts, otlpmetricgrpc.WithHeaders(cfg.Headers))
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
