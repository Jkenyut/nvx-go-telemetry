# nvx-go-telemetry

OpenTelemetry management library for Go applications.

## Overview
This library provides OpenTelemetry Traces, Metrics, and Context Propagation.
It configures OTLP gRPC exporters with production-ready buffer limits.

## Installation
Install the package with the Go command:

```bash
go get github.com/Jkenyut/nvx-go-telemetry
```

## Quick Start
Initialize telemetry at service startup. Always defer the shutdown function call.

```go
package main

import (
	"context"
	"log"

	"github.com/Jkenyut/nvx-go-telemetry/telemetry"
)

func main() {
	ctx := context.Background()

	shutdown, err := telemetry.Init(ctx,
		telemetry.WithEnabled(true),
		telemetry.WithServiceName("my-service"),
		telemetry.WithEnvironment("production"),
		telemetry.WithServiceVersion("v1.0.0"),
		telemetry.WithEndpoint("localhost:4317"),
		telemetry.WithInsecure(true),
	)
	if err != nil {
		log.Fatalf("failed to initialize telemetry: %v", err)
	}
	defer shutdown(context.Background())

	// Start a trace span
	ctx, span := telemetry.Start(ctx, "main", "process.task")
	defer span.End()

	// Extract active trace ID for structured logs
	traceID := telemetry.TraceID(ctx)
	log.Printf("Processing job with trace_id=%s", traceID)
}
```

## Trace Context Propagation

### HTTP Propagation
Use these functions to inject or extract trace headers in HTTP requests:

```go
// Inject trace context into an outbound HTTP request header
telemetry.InjectHTTP(ctx, req.Header)

// Extract trace context from an inbound HTTP request header
ctx = telemetry.ExtractHTTP(r.Context(), r.Header)
```

### Message Broker Propagation (RabbitMQ / Kafka)
Use `InjectAnyMap` and `ExtractAnyMap` with `map[string]any` headers:

```go
// In publisher: inject trace context into message headers
msg.Headers = telemetry.InjectAnyMap(ctx, msg.Headers)

// In consumer: extract trace context before you start the span
ctx = telemetry.ExtractAnyMap(ctx, msg.Headers)
```

## Configuration Options

| Option | Description | Default |
| :--- | :--- | :--- |
| `WithEnabled(bool)` | Enable or disable telemetry | `true` |
| `WithServiceName(string)` | Set resource service name | `nvx-go-telemetry` |
| `WithEnvironment(string)` | Set deployment environment | `""` |
| `WithServiceVersion(string)` | Set semantic version | `""` |
| `WithEndpoint(string)` | Set OTLP gRPC endpoint | `"localhost:4317"` |
| `WithInsecure(bool)` | Disable TLS encryption | `true` |
| `WithTraceRatio(float64)` | Sampling ratio between 0.0 and 1.0 | `1.0` |
| `WithShutdownTimeout(Duration)` | Timeout limit for flushing data | `5s` |

## License
Apache License 2.0. See [LICENSE](LICENSE) for details.
