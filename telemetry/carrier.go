package telemetry

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// AnyMapCarrier adapts map[string]any to propagation.TextMapCarrier.
// Use this type to inject or extract trace contexts in messaging headers.
type AnyMapCarrier map[string]any

// Get returns the value associated with the key.
func (c AnyMapCarrier) Get(key string) string {
	if c == nil {
		return ""
	}
	val, ok := c[key]
	if !ok || val == nil {
		return ""
	}
	if str, ok := val.(string); ok {
		return str
	}
	if b, ok := val.([]byte); ok {
		return string(b)
	}
	return ""
}

// Set stores the key-value pair in the map.
func (c AnyMapCarrier) Set(key string, val string) {
	if c == nil {
		return
	}
	c[key] = val
}

// Keys returns all keys present in the map.
func (c AnyMapCarrier) Keys() []string {
	if c == nil {
		return nil
	}
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// InjectHTTP writes trace context headers to an HTTP request header.
func InjectHTTP(ctx context.Context, header http.Header) {
	if ctx == nil || header == nil {
		return
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(header))
}

// ExtractHTTP reads trace context headers from an inbound HTTP request header.
func ExtractHTTP(ctx context.Context, header http.Header) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if header == nil {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(header))
}

// InjectAnyMap writes trace context to a map[string]any carrier.
func InjectAnyMap(ctx context.Context, carrier map[string]any) map[string]any {
	if ctx == nil {
		return carrier
	}
	if carrier == nil {
		carrier = make(map[string]any)
	}
	otel.GetTextMapPropagator().Inject(ctx, AnyMapCarrier(carrier))
	return carrier
}

// ExtractAnyMap reads trace context from a map[string]any carrier.
func ExtractAnyMap(ctx context.Context, carrier map[string]any) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if carrier == nil {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, AnyMapCarrier(carrier))
}

// InjectHTTPHeader writes trace context headers to an HTTP request header.
// It is an alias for InjectHTTP.
func InjectHTTPHeader(ctx context.Context, header http.Header) {
	InjectHTTP(ctx, header)
}

// ExtractHTTPHeader reads trace context headers from an HTTP request header.
// It is an alias for ExtractHTTP.
func ExtractHTTPHeader(ctx context.Context, header http.Header) context.Context {
	return ExtractHTTP(ctx, header)
}

// InjectAMQP writes trace context to a RabbitMQ message header table.
// It is an alias for InjectAnyMap.
func InjectAMQP(ctx context.Context, carrier map[string]any) map[string]any {
	return InjectAnyMap(ctx, carrier)
}

// ExtractAMQP reads trace context from a RabbitMQ message header table.
// It is an alias for ExtractAnyMap.
func ExtractAMQP(ctx context.Context, carrier map[string]any) context.Context {
	return ExtractAnyMap(ctx, carrier)
}
