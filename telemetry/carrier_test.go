package telemetry

import (
	"context"
	"net/http"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestCarrierHTTPAndAnyMap(t *testing.T) {
	// Setup tracer provider and propagator
	tp := sdktrace.NewTracerProvider()
	defer tp.Shutdown(context.Background())
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	ctx, span := Start(context.Background(), "test-tracer", "test-span")
	defer span.End()

	traceID := TraceID(ctx)
	if traceID == "" {
		t.Fatal("expected non-empty trace ID")
	}

	// 1. Test HTTP Carrier
	req, _ := http.NewRequest("GET", "http://localhost", nil)
	InjectHTTP(ctx, req.Header)
	if req.Header.Get("traceparent") == "" {
		t.Error("expected traceparent in HTTP header")
	}

	extractedCtx := ExtractHTTP(context.Background(), req.Header)
	if TraceID(extractedCtx) != traceID {
		t.Errorf("expected trace ID %s, got %s", traceID, TraceID(extractedCtx))
	}

	// 2. Test AnyMap Carrier (RabbitMQ amqp.Table simulation)
	table := make(map[string]any)
	table = InjectAnyMap(ctx, table)
	if table["traceparent"] == nil {
		t.Error("expected traceparent in table map")
	}

	extractedMapCtx := ExtractAnyMap(context.Background(), table)
	if TraceID(extractedMapCtx) != traceID {
		t.Errorf("expected trace ID %s, got %s", traceID, TraceID(extractedMapCtx))
	}

	// 3. Test Nil Safety
	InjectHTTP(nil, nil)
	_ = ExtractHTTP(nil, nil)
	_ = InjectAnyMap(nil, nil)
	_ = ExtractAnyMap(nil, nil)
}
