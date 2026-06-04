package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Tracer returns the standard tracer for the application.
// We use the ServiceName from config, but fallback to "nvx-go-telemetry" if needed.
func Tracer(name string) trace.Tracer {
	if name == "" {
		name = "nvx-go-telemetry"
	}
	return otel.Tracer(name)
}

// Start creates a new span from the global tracer.
func Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	// Usually, the tracer name is the package name creating the span,
	// but for simplicity in a helper, we use a generic name.
	return Tracer("").Start(ctx, spanName, opts...)
}

// RecordError records an error into a span and sets its status to Error.
func RecordError(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}
