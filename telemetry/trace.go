package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Tracer returns an OpenTelemetry tracer for the specified package or module name.
func Tracer(name string) trace.Tracer {
	if name == "" {
		name = "unknown-tracer"
	}
	return otel.Tracer(name)
}

// Start starts a new child span from the active context.
// Supply a non-empty tracer name and span name.
// This function handles a nil context safely by using context.Background.
func Start(ctx context.Context, tracerName, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	return Tracer(tracerName).Start(ctx, spanName, opts...)
}

// RecordError records an error event into the provided span.
// It sets the span status to Error.
// The function returns immediately if the span or the error is nil.
func RecordError(span trace.Span, err error) {
	if span == nil || err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// TraceID returns the active trace ID string from the context.
// It returns an empty string if the context is nil or lacks a trace ID.
func TraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.HasTraceID() {
		return spanCtx.TraceID().String()
	}
	return ""
}

// SpanID returns the active span ID string from the context.
// It returns an empty string if the context is nil or lacks a span ID.
func SpanID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.HasSpanID() {
		return spanCtx.SpanID().String()
	}
	return ""
}
