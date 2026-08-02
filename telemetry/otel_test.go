package telemetry

import (
	"context"
	"testing"
)

func TestInit_Disabled(t *testing.T) {


	shutdown, err := Init(context.Background(), WithEnabled(false))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if shutdown == nil {
		t.Fatalf("expected non-nil shutdown function")
	}

	// Should not panic or error
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("expected no error on shutdown, got %v", err)
	}
}

func TestInit_Enabled_NoEndpoint(t *testing.T) {
	// Without an endpoint, grpc might fail or retry, 
	// but we just test initialization without crashing.
	
	// Not strictly testing the connection, just the Init logic flow.
	shutdown, err := Init(context.Background(),
		WithEnabled(true),
		WithEndpoint("localhost:4317"),
		WithServiceName("test-service"),
		WithTraceEnabled(true),
		WithMetricsEnabled(true),
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if shutdown == nil {
		t.Fatalf("expected non-nil shutdown function")
	}

	if err := shutdown(context.Background()); err != nil {
		t.Logf("shutdown err expected if no server: %v", err)
	}
}
