# Architecture & Telemetry Flow Diagrams

This document outlines the end-to-end architecture and detailed sequence diagrams for the lifecycle, tracing, metrics, and context propagation in [`nvx-go-telemetry`](../../telemetry).

---

## High-Level Architecture & Data Flow

Visual overview of how your application interacts with `nvx-go-telemetry`, internal in-memory buffers, and observability backends:

```mermaid
flowchart TD
    subgraph APP["Your Application (Host Service)"]
        direction TB
        Main["1. Initialization<br/><code>telemetry.Init(ctx, opts...)</code>"]
        
        subgraph RUNTIME["2. Runtime Execution"]
            direction LR
            Tr["Tracing<br/><code>telemetry.Start()</code>"]
            Mt["Metrics<br/><code>MetricManager</code>"]
            Lg["Logging<br/><code>slog + TraceID</code>"]
        end

        Shutdown["4. Graceful Shutdown<br/><code>defer shutdown(ctx)</code>"]
    end

    subgraph ENGINE["nvx-go-telemetry Engine (In-Memory Buffers)"]
        direction TB
        BSP["BatchSpanProcessor<br/><i>(Queue: 8192, Batch: 1024)</i>"]
        PR["PeriodicReader<br/><i>(Interval: 30s, Timeout: 10s)</i>"]
        ErrorHandler["OTel ErrorHandler<br/><i>(Logs via slog.Logger)</i>"]
    end

    subgraph BACKEND["Observability Backend"]
        Collector["OTel Collector / Jaeger / Prometheus<br/><i>(gRPC: 4317)</i>"]
        Stdout["Stdout / Stderr<br/><i>(Application / Error Logs)</i>"]
    end

    %% Flow Connections
    Main -->|Configure OTel & Providers| RUNTIME
    Tr -->|Span Data| BSP
    Mt -->|Metric Data| PR
    Lg -->|Contextual Logs| Stdout
    ErrorHandler -->|Internal Errors| Stdout

    BSP -->|3. Async gRPC Export| Collector
    PR -->|3. Async gRPC Export| Collector

    Shutdown -.->|Flush & Close Pending Spans| BSP
    Shutdown -.->|Flush & Close Pending Metrics| PR
```

---

## 1. Telemetry Initialization & Graceful Shutdown

This diagram illustrates how [`telemetry.Init`](../../telemetry/otel.go) configures options, sets up the OpenTelemetry error handler using `slog`, instantiates OTel gRPC exporters & providers (traces & metrics), registers global propagators, and executes LIFO graceful shutdown with timeout contexts.

```mermaid
sequenceDiagram
    autonumber
    actor App as Caller App (main)
    participant Init as telemetry.Init()
    participant Cfg as Config
    participant Logger as slog.Logger
    participant Res as Resource Builder
    participant OTelGlobal as otel (Global Registry)
    participant TraceExp as OTLP gRPC Trace Exporter
    participant BSP as BatchSpanProcessor
    participant TP as TracerProvider (SDK)
    participant MetricExp as OTLP gRPC Metric Exporter
    participant PeriodicReader as PeriodicReader (30s)
    participant MP as MeterProvider (SDK)

    App->>Init: Init(ctx, opts...)
    Init->>Cfg: DefaultConfig() + apply functional options

    alt Config.Enabled == false
        Init-->>App: no-op shutdown func, nil
    end

    Init->>Logger: Resolve cfg.Logger (defaults to slog.Default())
    Init->>Res: resource.New(WithFromEnv, WithProcess, WithAttributes)
    Res-->>Init: Resource instance (warns on ErrPartialResource)

    opt cfg.EnableTrace == true
        Init->>TraceExp: otlptracegrpc.New(ctx, Endpoint, Insecure, Headers)
        TraceExp-->>Init: traceExporter
        Init->>BSP: NewBatchSpanProcessor(traceExporter, Queue: 8192, Batch: 1024, Timeout: 5s)
        Init->>TP: NewTracerProvider(ParentBased+Ratio, Resource, BSP, SpanLimits)
        Note over Init: Append tracerProvider.Shutdown to shutdownFuncs
    end

    opt cfg.EnableMetrics == true
        Init->>MetricExp: otlpmetricgrpc.New(ctx, Endpoint, Insecure, Headers)
        MetricExp-->>Init: metricExporter
        Init->>PeriodicReader: NewPeriodicReader(metricExporter, Interval: 30s, Timeout: 10s)
        Init->>MP: NewMeterProvider(Resource, PeriodicReader)
        Note over Init: Append meterProvider.Shutdown to shutdownFuncs
    end

    Note over Init,OTelGlobal: Atomic Registration (only when all providers succeed)
    Init->>OTelGlobal: SetErrorHandler(logger callback)
    Init->>OTelGlobal: SetTextMapPropagator(TraceContext + Baggage)
    opt TracerProvider created
        Init->>OTelGlobal: SetTracerProvider(tracerProvider)
    end
    opt MeterProvider created
        Init->>OTelGlobal: SetMeterProvider(meterProvider)
    end

    Init-->>App: shutdown(context.Context) func, nil

    Note over App: Application runs and handles traffic...

    App->>App: defer shutdown(context.Background())
    Note over App: Execute registered shutdown handlers in reverse order (LIFO)

    opt MeterProvider active
        App->>MP: Shutdown(shutdownCtx with Timeout)
        MP->>PeriodicReader: Shutdown & Flush pending metrics
        PeriodicReader->>MetricExp: Export remaining metrics
    end

    opt TracerProvider active
        App->>TP: Shutdown(shutdownCtx with Timeout)
        TP->>BSP: Shutdown & Flush pending spans
        BSP->>TraceExp: Export remaining batches
    end

    App-->>App: Graceful shutdown completed (errors.Join)
```

---

## 2. Tracing Flow & Error Capture

This diagram details the lifecycle of creating a span via [`telemetry.Start`](../../telemetry/trace.go), extracting Trace IDs for structured logging correlation, recording errors using [`telemetry.RecordError`](../../telemetry/trace.go), and dispatching data to `BatchSpanProcessor`.

```mermaid
sequenceDiagram
    autonumber
    actor Handler as Business Logic / Handler
    participant TraceUtil as telemetry (trace.go)
    participant OTelTracer as otel.Tracer
    participant Span as trace.Span
    participant AppLogger as slog.Logger
    participant BSP as BatchSpanProcessor

    Handler->>TraceUtil: Start(ctx, "orders", "ProcessPayment")
    TraceUtil->>OTelTracer: Tracer("orders").Start(ctx, "ProcessPayment", opts...)
    OTelTracer-->>TraceUtil: spanCtx, span
    TraceUtil-->>Handler: spanCtx, span

    Handler->>TraceUtil: TraceID(spanCtx)
    TraceUtil-->>Handler: "4bf92f3577b34da6a3ce929d0e0e4736"

    Handler->>AppLogger: logger.InfoContext(spanCtx, "Processing payment...", "trace_id", traceID)

    alt Operation Fails
        Handler->>TraceUtil: RecordError(span, err)
        TraceUtil->>Span: RecordError(err)
        TraceUtil->>Span: SetStatus(codes.Error, err.Error())
    end

    Handler->>Span: End()
    Span->>BSP: OnEnd(spanReadOnly)
    Note over BSP: Enqueue span into queue (max 8192) for batch export
```

---

## 3. Metrics Lifecycle & Periodic Harvesting

This diagram demonstrates metric instruments registration using [`MetricManager`](../../telemetry/metrics.go) and the periodic harvesting mechanism powered by OpenTelemetry SDK `PeriodicReader`.

```mermaid
sequenceDiagram
    autonumber
    actor Service as Service Component
    participant MM as MetricManager
    participant Meter as otel.Meter
    participant PeriodicReader as sdkmetric.PeriodicReader (Ticker: 30s)
    participant Exporter as otlpmetricgrpc Exporter
    participant Collector as OpenTelemetry Collector

    Service->>MM: NewMetricManager("service.orders")
    MM->>Meter: otel.Meter("service.orders")
    MM-->>Service: *MetricManager

    Service->>MM: RegisterCounter("orders_total", "Total processed orders")
    MM->>Meter: Int64Counter("orders_total", opts...)
    Meter-->>Service: ordersCounter (Int64Counter)

    Service->>MM: RegisterHistogram("payment_latency_ms", "Payment duration", "ms")
    MM->>Meter: Float64Histogram("payment_latency_ms", opts...)
    Meter-->>Service: latencyHist (Float64Histogram)

    Note over Service: Runtime operations execute
    Service->>Service: ordersCounter.Add(ctx, 1, attrs...)
    Service->>Service: latencyHist.Record(ctx, 124.5, attrs...)

    loop Every 30 Seconds
        PeriodicReader->>Meter: Collect & Aggregate metrics
        PeriodicReader->>Exporter: Export(ctx, metricDataBatch)
        Exporter->>Collector: gRPC ExportMetricsServiceRequest
        Collector-->>Exporter: gRPC ExportMetricsServiceResponse (OK)
    end
```

---

## 4. Distributed Context Propagation (W3C TraceContext)

This diagram shows how TraceContext and Baggage headers are extracted from an incoming HTTP request and injected into downstream network calls to preserve distributed end-to-end trace correlation.

```mermaid
sequenceDiagram
    autonumber
    actor Upstream as Upstream Client / Gateway
    participant Handler as Inbound HTTP Handler
    participant Propagator as otel.GetTextMapPropagator()
    participant TraceHelper as telemetry.Start()
    participant Client as Outbound HTTP Client
    participant Downstream as Downstream Service

    Upstream->>Handler: HTTP Request with Headers:<br/>traceparent: 00-4bf92f35...-00f067aa...-01<br/>baggage: userId=123
    Handler->>Propagator: Extract(r.Context(), propagation.HeaderCarrier(r.Header))
    Propagator-->>Handler: extractedCtx (contains remote parent SpanContext)

    Handler->>TraceHelper: Start(extractedCtx, "service-b", "HandleOrder")
    TraceHelper-->>Handler: serviceBCtx, span (child span linked to upstream trace)

    Note over Handler: Handler prepares outbound request to Downstream Service

    Handler->>Client: Create new HTTP Request (serviceBCtx)
    Client->>Propagator: Inject(serviceBCtx, propagation.HeaderCarrier(req.Header))
    Note over Propagator: Injects new traceparent (new Span ID, same Trace ID)

    Client->>Downstream: HTTP Request (traceparent, baggage)
    Downstream-->>Client: HTTP 200 OK
    Client-->>Handler: Response received

    Handler->>TraceHelper: span.End()
```
