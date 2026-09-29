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
            Lg["Logging<br/><code>Zerolog + TraceID</code>"]
        end

        Shutdown["4. Graceful Shutdown<br/><code>defer shutdown(ctx)</code>"]
    end

    subgraph ENGINE["nvx-go-telemetry Engine (In-Memory Buffers)"]
        direction TB
        BSP["BatchSpanProcessor<br/><i>(Queue: 8192, Batch: 1024)</i>"]
        PR["PeriodicReader<br/><i>(Interval: 30s)</i>"]
        Diode["Diode Ring Buffer<br/><i>(Non-blocking logger)</i>"]
    end

    subgraph BACKEND["Observability Backend"]
        Collector["OTel Collector / Jaeger / Prometheus<br/><i>(gRPC: 4317)</i>"]
        Stdout["Stdout / Stderr<br/><i>(Structured JSON Logs)</i>"]
    end

    %% Flow Connections
    Main -->|Configure OTel & Providers| RUNTIME
    Tr -->|Span Data| BSP
    Mt -->|Metric Data| PR
    Lg -->|Log Events| Diode

    BSP -->|3. Async gRPC Export| Collector
    PR -->|3. Async gRPC Export| Collector
    Diode -->|Output Stream| Stdout

    Shutdown -.->|Flush & Close Remaining Buffers| BSP
    Shutdown -.->|Flush & Close Remaining Buffers| PR
    Shutdown -.->|Flush & Close Remaining Buffers| Diode
```

---

## 1. Telemetry Initialization & Graceful Shutdown

This diagram illustrates how [`telemetry.Init`](../../telemetry/otel.go) configures options, sets up an asynchronous diode logger buffer, instantiates OTel gRPC exporters & providers (traces & metrics), registers global propagators, and executes LIFO graceful shutdown with timeout contexts.

```mermaid
sequenceDiagram
    autonumber
    actor App as Caller App (main)
    participant Init as telemetry.Init()
    participant Cfg as Config
    participant Diode as Diode Writer & Logger
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

    alt cfg.Logger == nil
        Init->>Diode: diode.NewWriter(1000 ring buffer, 10ms poll)
        Init->>Diode: Setup Zerolog instance (prod: JSON / dev: Console)
        Note over Diode: Append logger.Close() to shutdownFuncs
    end

    Init->>OTelGlobal: SetErrorHandler(logger callback)
    Init->>Res: resource.New(WithFromEnv, WithProcess, WithAttributes)
    Res-->>Init: Resource instance

    Init->>OTelGlobal: SetTextMapPropagator(TraceContext + Baggage)

    opt cfg.EnableTrace == true
        Init->>TraceExp: otlptracegrpc.New(ctx, Endpoint, Insecure, Headers)
        TraceExp-->>Init: traceExporter
        Init->>BSP: NewBatchSpanProcessor(traceExporter, Queue: 8192, Batch: 1024, Timeout: 5s)
        Init->>TP: NewTracerProvider(ParentBased+Ratio, Resource, BSP, SpanLimits)
        Init->>OTelGlobal: SetTracerProvider(tracerProvider)
        Note over Init: Append tracerProvider.Shutdown to shutdownFuncs
    end

    opt cfg.EnableMetrics == true
        Init->>MetricExp: otlpmetricgrpc.New(ctx, Endpoint, Insecure, Headers)
        MetricExp-->>Init: metricExporter
        Init->>PeriodicReader: NewPeriodicReader(metricExporter, Interval: 30s, Timeout: 10s)
        Init->>MP: NewMeterProvider(Resource, PeriodicReader)
        Init->>OTelGlobal: SetMeterProvider(meterProvider)
        Note over Init: Append meterProvider.Shutdown to shutdownFuncs
    end

    Init-->>App: shutdown(context.Context) func, nil

    Note over App: Application runs and handles traffic...

    App->>App: defer shutdown(context.Background())
    Note over App: Execute registered shutdown handlers in reverse order (LIFO)

    opt MeterProvider active
        App->>MP: Shutdown(5s timeout ctx)
        MP->>PeriodicReader: Shutdown & Flush pending metrics
        PeriodicReader->>MetricExp: Export remaining metrics
    end

    opt TracerProvider active
        App->>TP: Shutdown(5s timeout ctx)
        TP->>BSP: Shutdown & Flush pending spans
        BSP->>TraceExp: Export remaining batches
    end

    opt Custom Diode Logger active
        App->>Diode: Close writer ring buffer
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
    participant DiodeLog as Zerolog Logger
    participant BSP as BatchSpanProcessor

    Handler->>TraceUtil: Start(ctx, "orders", "ProcessPayment")
    TraceUtil->>OTelTracer: Tracer("orders").Start(ctx, "ProcessPayment", opts...)
    OTelTracer-->>TraceUtil: spanCtx, span
    TraceUtil-->>Handler: spanCtx, span

    Handler->>TraceUtil: TraceID(spanCtx)
    TraceUtil-->>Handler: "4bf92f3577b34da6a3ce929d0e0e4736"

    Handler->>DiodeLog: logger.Info().Str("trace_id", traceID).Msg("Processing payment...")

    alt Operation Fails
        Handler->>TraceUtil: RecordError(span, err)
        TraceUtil->>Span: RecordError(err)
        TraceUtil->>Span: SetStatus(codes.Error, err.Error())
    end

    Handler->>Span: End()
    Span->>BSP: OnEnd(spanReadOnly)
    Note over BSP: Enqueue span into ring buffer (max 8192) for batch export
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
