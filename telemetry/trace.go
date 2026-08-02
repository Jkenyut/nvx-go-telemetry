package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Tracer returns the standard tracer for the application.
func Tracer(name string) trace.Tracer {
	if name == "" {
		name = "unknown-tracer"
	}
	return otel.Tracer(name)
}

// Start creates a new span from the global tracer.
// tracerName should ideally be the package name creating the span.
func Start(ctx context.Context, tracerName, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return Tracer(tracerName).Start(ctx, spanName, opts...)
}

// RecordError records an error into a span and sets its status to Error.
func RecordError(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}

// TraceID extracts the TraceID from the context, or returns an empty string if no trace exists.
func TraceID(ctx context.Context) string {
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.HasTraceID() {
		return spanCtx.TraceID().String()
	}
	return ""
}

// SpanID extracts the SpanID from the context, or returns an empty string if no span exists.
func SpanID(ctx context.Context) string {
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.HasSpanID() {
		return spanCtx.SpanID().String()
	}
	return ""
}
