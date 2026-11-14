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

	// 3. Test Case-Insensitive Fallback & Byte Slices in AnyMapCarrier
	capitalizedTable := map[string]any{
		"Traceparent": table["traceparent"],
	}
	extractedCapCtx := ExtractAnyMap(context.Background(), capitalizedTable)
	if TraceID(extractedCapCtx) != traceID {
		t.Errorf("expected case-insensitive trace ID %s, got %s", traceID, TraceID(extractedCapCtx))
	}

	byteTable := map[string]any{
		"TRACEPARENT": []byte(table["traceparent"].(string)),
	}
	extractedByteCtx := ExtractAnyMap(context.Background(), byteTable)
	if TraceID(extractedByteCtx) != traceID {
		t.Errorf("expected byte slice trace ID %s, got %s", traceID, TraceID(extractedByteCtx))
	}

	// 4. Test Nil Safety & Edge Cases
	InjectHTTP(nil, nil)
	InjectHTTP(ctx, nil)
	_ = ExtractHTTP(nil, nil)
	_ = ExtractHTTP(ctx, nil)
	_ = InjectAnyMap(nil, nil)
	injectedNilMap := InjectAnyMap(ctx, nil)
	if injectedNilMap["traceparent"] == nil {
		t.Error("expected InjectAnyMap with nil map to allocate and populate traceparent")
	}
	_ = ExtractAnyMap(nil, nil)
	_ = ExtractAnyMap(ctx, nil)

	var nilCarrier AnyMapCarrier
	if nilCarrier.Get("traceparent") != "" {
		t.Error("expected empty string from nil AnyMapCarrier.Get")
	}
	nilCarrier.Set("traceparent", "val")
	if nilCarrier.Keys() != nil {
		t.Error("expected nil keys from nil AnyMapCarrier.Keys")
	}

	nonStringCarrier := AnyMapCarrier{
		"num":         123,
		"Traceparent": 456,
	}
	if nonStringCarrier.Get("num") != "" || nonStringCarrier.Get("traceparent") != "" {
		t.Error("expected empty string for non-string/non-byte carrier values")
	}

	keys := AnyMapCarrier(table).Keys()
	if len(keys) == 0 {
		t.Error("expected non-empty keys from AnyMapCarrier.Keys")
	}

	// 5. Test Alias Helpers (InjectHTTPHeader, ExtractHTTPHeader, InjectAMQP, ExtractAMQP)
	aliasReq, _ := http.NewRequest("GET", "http://localhost", nil)
	InjectHTTPHeader(ctx, aliasReq.Header)
	if aliasReq.Header.Get("traceparent") == "" {
		t.Error("expected traceparent via InjectHTTPHeader")
	}
	if got := TraceID(ExtractHTTPHeader(context.Background(), aliasReq.Header)); got != traceID {
		t.Errorf("expected trace ID %s via ExtractHTTPHeader, got %s", traceID, got)
	}

	amqpHeaders := InjectAMQP(ctx, nil)
	if amqpHeaders["traceparent"] == nil {
		t.Error("expected traceparent via InjectAMQP")
	}
	if got := TraceID(ExtractAMQP(context.Background(), amqpHeaders)); got != traceID {
		t.Errorf("expected trace ID %s via ExtractAMQP, got %s", traceID, got)
	}
}
