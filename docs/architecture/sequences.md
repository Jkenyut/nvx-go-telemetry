# Architecture and Sequence Workflows

This document describes the software architecture and operational workflows for [`nvx-go-telemetry`](../../telemetry).

---

## 1. High-Level System Architecture

The diagram below shows the data flow between the host application, internal memory buffers, and observability collectors.

```mermaid
flowchart TD
    subgraph APP["Host Application"]
        direction TB
        Main["Initialization: telemetry.Init(ctx, opts...)"]

        subgraph RUNTIME["Runtime Operations"]
            direction LR
            Tr["Tracing: telemetry.Start()"]
            Mt["Metrics: MetricManager"]
            Lg["Logging: slog with TraceID"]
        end

        Shutdown["Graceful Shutdown: defer shutdown(ctx)"]
    end

    subgraph ENGINE["nvx-go-telemetry Engine"]
        direction TB
        BSP["BatchSpanProcessor (Queue: 8192, Batch: 1024)"]
        PR["PeriodicReader (Interval: 30s, Timeout: 10s)"]
        ErrorHandler["OTel ErrorHandler (slog logger)"]
    end

    subgraph BACKEND["Observability Infrastructure"]
        Collector["OpenTelemetry Collector (gRPC: 4317)"]
        Stdout["Standard Output (Application Logs)"]
    end

    Main --> RUNTIME
    Tr -->|"Span Records"| BSP
    Mt -->|"Metric Measurements"| PR
    Lg -->|"Structured Logs"| Stdout
    ErrorHandler -->|"Internal Errors"| Stdout

    BSP -->|"Export Spans via gRPC"| Collector
    PR -->|"Export Metrics via gRPC"| Collector

    Shutdown -.->|"Flush Spans"| BSP
    Shutdown -.->|"Flush Metrics"| PR
```

The host application configures providers during process startup. The trace helper forwards completed spans to the batch span processor. The metric manager buffers measurement points for the periodic reader. Exporters push queued records to the OpenTelemetry collector over gRPC connections.

---

## 2. Telemetry Lifecycle and Graceful Shutdown

The sequence diagram below shows provider initialization, error handling configuration, and shutdown sequencing.

```mermaid
sequenceDiagram
    autonumber
    actor App as Host Application
    participant Init as telemetry.Init()
    participant Cfg as Config
    participant Logger as slog.Logger
    participant Res as Resource Builder
    participant OTelGlobal as otel Registry
    participant TraceExp as OTLP gRPC Trace Exporter
    participant BSP as BatchSpanProcessor
    participant TP as TracerProvider
    participant MetricExp as OTLP gRPC Metric Exporter
    participant PR as PeriodicReader
    participant MP as MeterProvider

    App->>Init: Init(ctx, opts...)
    Note over Init: Acquire initMu lock
    Init->>Cfg: Create DefaultConfig and apply options
    Init->>Cfg: Validate configuration parameters

    alt Config.Enabled == false
        Init-->>App: Return no-op shutdown function and nil
    end

    Init->>Logger: Resolve structured logger
    Init->>Res: resource.New(WithFromEnv, WithProcess, WithAttributes)
    Res-->>Init: Return resource instance

    opt cfg.EnableTrace == true
        Init->>TraceExp: otlptracegrpc.New(ctx, options)
        TraceExp-->>Init: Return trace exporter
        Init->>BSP: NewBatchSpanProcessor(traceExporter, queue limits)
        Init->>TP: NewTracerProvider(Sampler, Resource, BSP, SpanLimits)
        Note over Init: Register TP.Shutdown into shutdown list
    end

    opt cfg.EnableMetrics == true
        Init->>MetricExp: otlpmetricgrpc.New(ctx, options)
        MetricExp-->>Init: Return metric exporter
        Init->>PR: NewPeriodicReader(metricExporter, interval 30s)
        Init->>MP: NewMeterProvider(Resource, PR)
        Note over Init: Register MP.Shutdown into shutdown list
    end

    Init->>OTelGlobal: SetErrorHandler(logger callback)
    Init->>OTelGlobal: SetTextMapPropagator(TraceContext and Baggage)
    Init->>OTelGlobal: SetTracerProvider(TP)
    Init->>OTelGlobal: SetMeterProvider(MP)
    Init-->>App: Return shutdown function and nil

    Note over App: Host application handles service traffic...

    App->>App: Execute deferred shutdown(ctx)
    Note over App: shutdownOnce executes cleanup handlers

    opt MeterProvider active
        App->>MP: Shutdown(shutdownCtx)
        MP->>PR: Flush pending metric measurements
        PR->>MetricExp: Export remaining metrics to collector
    end

    opt TracerProvider active
        App->>TP: Shutdown(shutdownCtx)
        TP->>BSP: Flush pending span records
        BSP->>TraceExp: Export remaining batches to collector
    end

    App-->>App: Complete graceful shutdown
```

### Operational Steps (ASD-STE100)
1. Call `telemetry.Init` with configuration options during startup.
2. The function acquires an internal mutex lock.
3. The function validates user configuration values.
4. The function returns a no-op shutdown function when disabled.
5. The resource builder collects service attributes and process identifiers.
6. The runtime creates OTLP gRPC exporters for active signals.
7. The trace provider applies sampling ratios and buffer limits.
8. The meter provider configures a periodic reader with a thirty-second harvest interval.
9. The function registers global OpenTelemetry propagators and providers.
10. The function releases the initialization mutex lock.
11. Defer the returned shutdown function in the `main` function.
12. The shutdown function flushes buffers in reverse registration order during termination.

---

## 3. Tracing Workflow and Error Recording

The sequence diagram below shows span creation, context propagation, error logging, and span termination.

```mermaid
sequenceDiagram
    autonumber
    actor Handler as HTTP Handler
    participant TraceUtil as telemetry (trace.go)
    participant OTelTracer as otel.Tracer
    participant Span as trace.Span
    participant AppLogger as slog.Logger
    participant BSP as BatchSpanProcessor

    Handler->>TraceUtil: Start(ctx, "orders", "ProcessPayment")
    TraceUtil->>OTelTracer: Tracer("orders").Start(ctx, "ProcessPayment")
    OTelTracer-->>TraceUtil: Return spanCtx and span handle
    TraceUtil-->>Handler: Return spanCtx and span handle

    Handler->>TraceUtil: TraceID(spanCtx)
    TraceUtil-->>Handler: Return 32-character trace ID string

    Handler->>AppLogger: InfoContext(spanCtx, "Payment started", "trace_id", traceID)

    alt Business operation fails
        Handler->>TraceUtil: RecordError(span, err)
        TraceUtil->>Span: RecordError(err)
        TraceUtil->>Span: SetStatus(codes.Error, err.Error())
    end

    Handler->>Span: End()
    Span->>BSP: Push span to internal queue
    Note over BSP: Buffer enqueues span for batch export
```

### Operational Steps (ASD-STE100)
1. Call `telemetry.Start` with context and operation names.
2. The function returns a child context and a span handle.
3. Read the active trace identifier with `telemetry.TraceID`.
4. Include the trace identifier in structured log messages.
5. Record operation failures with `telemetry.RecordError`.
6. The error helper marks the span status as an error.
7. Always call `span.End` in a defer statement.
8. The span moves to the batch processor for export.

---

## 4. Metrics Workflow and Instrument Caching

The sequence diagram below shows instrument registration, cache lookups, and periodic data exports.

```mermaid
sequenceDiagram
    autonumber
    actor Service as Domain Service
    participant MM as MetricManager
    participant Meter as otel.Meter
    participant PR as PeriodicReader
    participant Exporter as otlpmetricgrpc Exporter
    participant Collector as OTel Collector

    Service->>MM: NewMetricManager("orders.service")
    MM->>Meter: otel.Meter("orders.service")
    MM-->>Service: Return MetricManager pointer

    Service->>MM: RegisterCounter("orders_total", "Total processed orders")
    Note over MM: Check internal cache map with read lock

    alt Instrument not cached
        MM->>Meter: Create Int64Counter
        Meter-->>MM: Return counter handle
        Note over MM: Store counter in cache with write lock
    end
    MM-->>Service: Return cached counter handle

    Service->>MM: RegisterHistogram("payment_latency_ms", "Latency", "ms")
    Note over MM: Check internal cache map with read lock

    alt Instrument not cached
        MM->>Meter: Create Float64Histogram
        Meter-->>MM: Return histogram handle
        Note over MM: Store histogram in cache with write lock
    end
    MM-->>Service: Return cached histogram handle

    Service->>Service: counter.Add(ctx, 1)
    Service->>Service: histogram.Record(ctx, 142.5)

    loop Every 30 seconds
        PR->>Meter: Harvest aggregated metrics
        PR->>Exporter: Export metric data batch
        Exporter->>Collector: Send gRPC ExportMetricsServiceRequest
        Collector-->>Exporter: Return gRPC response confirmation
    end
```

### Operational Steps (ASD-STE100)
1. Create a `MetricManager` instance with an instrumentation name.
2. Call `RegisterCounter` to create an integer counter.
3. The manager checks internal memory cache for duplicate names.
4. The manager returns existing handles if previously registered.
5. Call `RegisterHistogram` to create a duration instrument.
6. Record runtime observations with `Add` or `Record` methods.
7. The periodic reader harvests measurements every thirty seconds.
8. The exporter sends metric batches to the OpenTelemetry collector.

---

## 5. Distributed Context Propagation Workflow

The sequence diagram below shows context extraction from HTTP and messaging carriers, followed by context injection.

```mermaid
sequenceDiagram
    autonumber
    actor Gateway as Inbound Gateway
    participant Handler as Inbound HTTP Handler
    participant Carrier as Carrier Adapter
    participant Propagator as otel TextMapPropagator
    participant Consumer as RabbitMQ Consumer
    participant Client as Outbound HTTP Client
    participant Target as Downstream Service

    Gateway->>Handler: HTTP Request with traceparent header
    Handler->>Carrier: ExtractHTTP(r.Context(), r.Header)
    Carrier->>Propagator: Extract(ctx, HeaderCarrier(header))
    Propagator-->>Carrier: Return extracted context
    Carrier-->>Handler: Return context with parent trace ID

    Consumer->>Carrier: ExtractAnyMap(ctx, amqpHeaders)
    Note over Carrier: Match keys with case-insensitive check
    Carrier->>Propagator: Extract(ctx, AnyMapCarrier(amqpHeaders))
    Propagator-->>Carrier: Return extracted context
    Carrier-->>Consumer: Return context with parent trace ID

    Handler->>Client: Prepare outbound HTTP request
    Client->>Carrier: InjectHTTP(reqCtx, req.Header)
    Carrier->>Propagator: Inject(reqCtx, HeaderCarrier(req.Header))
    Note over Propagator: Write traceparent and baggage headers

    Client->>Target: Send HTTP request with distributed trace headers
    Target-->>Client: Return HTTP response
```

### Operational Steps (ASD-STE100)
1. Read incoming request headers with `telemetry.ExtractHTTP`.
2. Extract message broker headers with `telemetry.ExtractAnyMap`.
3. The carrier matches header keys with case-insensitive comparisons.
4. Pass the extracted context to `telemetry.Start`.
5. Inject trace context into outbound requests with `telemetry.InjectHTTP`.
6. Inject trace context into message payloads with `telemetry.InjectAnyMap`.
7. Transmit network payloads with W3C traceparent headers.
