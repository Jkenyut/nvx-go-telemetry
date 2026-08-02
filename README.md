# nvx-go-telemetry

[![Go Reference](https://pkg.go.dev/badge/github.com/nvx-go/nvx-go-telemetry.svg)](https://pkg.go.dev/github.com/nvx-go/nvx-go-telemetry)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

`nvx-go-telemetry` is a streamlined OpenTelemetry management library for Go applications. It simplifies the initialization and configuration of OTLP Traces and Metrics exporters, adhering to best practices out of the box.

## ✨ Features

- **OpenTelemetry Native**: Built directly on top of `go.opentelemetry.io`.
- **Zero-Boilerplate Setup**: Initialize tracing and metrics with a single function call.
- **Configurable**: Environment-driven or code-driven configuration options.
- **Graceful Shutdown**: Built-in context handling and timeout for flushing telemetry data.
- **Production Ready**: Uses standard `TraceContext` and `Baggage` propagators, optimal export batch limits, and built-in error handling via `zerolog`.

## 📦 Installation

```bash
go get github.com/nvx-go/nvx-go-telemetry
```

## 🚀 Quick Start

Here is a complete example of initializing the telemetry provider and gracefully shutting it down:

```go
package main

import (
	"context"
	"log"

	"github.com/nvx-go/nvx-go-telemetry/telemetry"
	"go.opentelemetry.io/otel"
)

func main() {
	ctx := context.Background()

	// Initialize OpenTelemetry
	shutdown, err := telemetry.Init(ctx,
		telemetry.WithEnabled(true),
		telemetry.WithServiceName("my-awesome-service"),
		telemetry.WithEndpoint("localhost:4317"),
		telemetry.WithInsecure(true),
	)
	if err != nil {
		log.Fatalf("failed to initialize telemetry: %v", err)
	}
	// Ensure all data is flushed upon exit
	defer func() {
		if err := shutdown(context.Background()); err != nil {
			log.Printf("failed to gracefully shutdown telemetry: %v", err)
		}
	}()

	// Now you can use global providers from go.opentelemetry.io/otel
	tracer := otel.Tracer("my-tracer")
	_, span := tracer.Start(ctx, "main.operation")
	defer span.End()
	
	// App logic here...
}
```

## ⚙️ Configuration Options

The library is highly configurable using functional options during initialization:

| Option | Description | Default |
| :--- | :--- | :--- |
| `WithEnabled(bool)` | Master switch to enable/disable telemetry | `true` |
| `WithServiceName(string)` | Sets the `service.name` resource attribute | `nvx-go-app` |
| `WithEndpoint(string)` | Sets the OTLP gRPC endpoint | `localhost:4317` |
| `WithInsecure(bool)` | Disables TLS for the OTLP connection | `true` |
| `WithHeaders(map[string]string)`| Sets custom headers (e.g., API Keys) for exporters | `nil` |
| `WithTraceEnabled(bool)` | Specifically enable/disable tracing | `true` |
| `WithMetricsEnabled(bool)` | Specifically enable/disable metrics | `true` |
| `WithTraceRatio(float64)` | Sampling ratio for tracing (0.0 to 1.0) | `1.0` |
| `WithLogger(*zerolog.Logger)` | Sets a custom logger for OTEL internal errors | *Console Writer* |

## 🤝 Best Practices Included

- **Resource Attributes**: Automatically reads `OTEL_RESOURCE_ATTRIBUTES` from the environment and attaches process information.
- **Propagation**: Globally registers `TraceContext` and `Baggage` propagators.
- **Batching & Limits**: Sets explicit limits to prevent OOM errors and manages safe batching configurations.

## 📄 License

This project is licensed under the **Apache License 2.0**. See the [LICENSE](LICENSE) file for details.
