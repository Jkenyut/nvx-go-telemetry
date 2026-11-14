package telemetry

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	// ErrUninitializedManager indicates a nil MetricManager or missing underlying Meter.
	ErrUninitializedManager = errors.New("metric manager is uninitialized")
	// ErrEmptyMetricName indicates an empty or whitespace-only metric instrument name.
	ErrEmptyMetricName = errors.New("metric name cannot be empty")
)

// MetricManager provides a wrapper around OpenTelemetry Meter with thread-safe instrument caching.
type MetricManager struct {
	metric.Meter
	mu                    sync.RWMutex
	counters              map[string]metric.Int64Counter
	floatCounters         map[string]metric.Float64Counter
	upDownCounters        map[string]metric.Int64UpDownCounter
	floatUpDownCounters   map[string]metric.Float64UpDownCounter
	histograms            map[string]metric.Float64Histogram
	intHistograms         map[string]metric.Int64Histogram
	observableGauges      map[string]metric.Int64ObservableGauge
	floatObservableGauges map[string]metric.Float64ObservableGauge
}

// NewMetricManager creates a new MetricManager backed by the global OpenTelemetry MeterProvider.
func NewMetricManager(instrumentationName string) *MetricManager {
	if instrumentationName == "" {
		instrumentationName = "unknown-meter"
	}
	return NewMetricManagerWithMeter(otel.Meter(instrumentationName))
}

// NewMetricManagerWithMeter creates a new MetricManager using the provided metric.Meter.
// Use this constructor for dependency injection and isolated testing without global state.
func NewMetricManagerWithMeter(meter metric.Meter) *MetricManager {
	return &MetricManager{
		Meter:                 meter,
		counters:              make(map[string]metric.Int64Counter),
		floatCounters:         make(map[string]metric.Float64Counter),
		upDownCounters:        make(map[string]metric.Int64UpDownCounter),
		floatUpDownCounters:   make(map[string]metric.Float64UpDownCounter),
		histograms:            make(map[string]metric.Float64Histogram),
		intHistograms:         make(map[string]metric.Int64Histogram),
		observableGauges:      make(map[string]metric.Int64ObservableGauge),
		floatObservableGauges: make(map[string]metric.Float64ObservableGauge),
	}
}

func (m *MetricManager) validate(name string) error {
	if m == nil || m.Meter == nil {
		return ErrUninitializedManager
	}
	if strings.TrimSpace(name) == "" {
		return ErrEmptyMetricName
	}
	return nil
}

func getOrRegisterInstrument[T any](
	m *MetricManager,
	name string,
	kind string,
	cache *map[string]T,
	create func() (T, error),
) (T, error) {
	var zero T
	m.mu.RLock()
	if inst, ok := (*cache)[name]; ok {
		m.mu.RUnlock()
		return inst, nil
	}
	m.mu.RUnlock()

	inst, err := create()
	if err != nil {
		return zero, fmt.Errorf("registering %s %q: %w", kind, name, err)
	}

	m.mu.Lock()
	if *cache == nil {
		*cache = make(map[string]T)
	}
	if existing, ok := (*cache)[name]; ok {
		m.mu.Unlock()
		return existing, nil
	}
	(*cache)[name] = inst
	m.mu.Unlock()

	return inst, nil
}

// RegisterCounter creates a new integer counter or returns a cached instance.
func (m *MetricManager) RegisterCounter(name, description string, opts ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if err := m.validate(name); err != nil {
		return nil, err
	}
	return getOrRegisterInstrument(m, name, "counter", &m.counters, func() (metric.Int64Counter, error) {
		allOpts := make([]metric.Int64CounterOption, 0, len(opts)+1)
		if description != "" {
			allOpts = append(allOpts, metric.WithDescription(description))
		}
		allOpts = append(allOpts, opts...)
		return m.Int64Counter(name, allOpts...)
	})
}

// RegisterFloat64Counter creates a new float64 counter or returns a cached instance.
func (m *MetricManager) RegisterFloat64Counter(name, description string, opts ...metric.Float64CounterOption) (metric.Float64Counter, error) {
	if err := m.validate(name); err != nil {
		return nil, err
	}
	return getOrRegisterInstrument(m, name, "float64 counter", &m.floatCounters, func() (metric.Float64Counter, error) {
		allOpts := make([]metric.Float64CounterOption, 0, len(opts)+1)
		if description != "" {
			allOpts = append(allOpts, metric.WithDescription(description))
		}
		allOpts = append(allOpts, opts...)
		return m.Float64Counter(name, allOpts...)
	})
}

// RegisterUpDownCounter creates a new integer up-down counter or returns a cached instance.
func (m *MetricManager) RegisterUpDownCounter(name, description string, opts ...metric.Int64UpDownCounterOption) (metric.Int64UpDownCounter, error) {
	if err := m.validate(name); err != nil {
		return nil, err
	}
	return getOrRegisterInstrument(m, name, "up-down counter", &m.upDownCounters, func() (metric.Int64UpDownCounter, error) {
		allOpts := make([]metric.Int64UpDownCounterOption, 0, len(opts)+1)
		if description != "" {
			allOpts = append(allOpts, metric.WithDescription(description))
		}
		allOpts = append(allOpts, opts...)
		return m.Int64UpDownCounter(name, allOpts...)
	})
}

// RegisterFloat64UpDownCounter creates a new float64 up-down counter or returns a cached instance.
func (m *MetricManager) RegisterFloat64UpDownCounter(name, description string, opts ...metric.Float64UpDownCounterOption) (metric.Float64UpDownCounter, error) {
	if err := m.validate(name); err != nil {
		return nil, err
	}
	return getOrRegisterInstrument(m, name, "float64 up-down counter", &m.floatUpDownCounters, func() (metric.Float64UpDownCounter, error) {
		allOpts := make([]metric.Float64UpDownCounterOption, 0, len(opts)+1)
		if description != "" {
			allOpts = append(allOpts, metric.WithDescription(description))
		}
		allOpts = append(allOpts, opts...)
		return m.Float64UpDownCounter(name, allOpts...)
	})
}

// RegisterHistogram creates a new float64 histogram or returns a cached instance.
func (m *MetricManager) RegisterHistogram(name, description, unit string, opts ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	if err := m.validate(name); err != nil {
		return nil, err
	}
	return getOrRegisterInstrument(m, name, "histogram", &m.histograms, func() (metric.Float64Histogram, error) {
		allOpts := make([]metric.Float64HistogramOption, 0, len(opts)+2)
		if description != "" {
			allOpts = append(allOpts, metric.WithDescription(description))
		}
		if unit != "" {
			allOpts = append(allOpts, metric.WithUnit(unit))
		}
		allOpts = append(allOpts, opts...)
		return m.Float64Histogram(name, allOpts...)
	})
}

// RegisterInt64Histogram creates a new int64 histogram or returns a cached instance.
func (m *MetricManager) RegisterInt64Histogram(name, description, unit string, opts ...metric.Int64HistogramOption) (metric.Int64Histogram, error) {
	if err := m.validate(name); err != nil {
		return nil, err
	}
	return getOrRegisterInstrument(m, name, "int64 histogram", &m.intHistograms, func() (metric.Int64Histogram, error) {
		allOpts := make([]metric.Int64HistogramOption, 0, len(opts)+2)
		if description != "" {
			allOpts = append(allOpts, metric.WithDescription(description))
		}
		if unit != "" {
			allOpts = append(allOpts, metric.WithUnit(unit))
		}
		allOpts = append(allOpts, opts...)
		return m.Int64Histogram(name, allOpts...)
	})
}

// MeasureFunc registers a callback to record an observable int64 gauge or returns a cached instance.
func (m *MetricManager) MeasureFunc(name, description string, callback metric.Int64Callback, opts ...metric.Int64ObservableGaugeOption) (metric.Int64ObservableGauge, error) {
	if err := m.validate(name); err != nil {
		return nil, err
	}
	return getOrRegisterInstrument(m, name, "observable gauge", &m.observableGauges, func() (metric.Int64ObservableGauge, error) {
		allOpts := make([]metric.Int64ObservableGaugeOption, 0, len(opts)+2)
		if description != "" {
			allOpts = append(allOpts, metric.WithDescription(description))
		}
		if callback != nil {
			allOpts = append(allOpts, metric.WithInt64Callback(callback))
		}
		allOpts = append(allOpts, opts...)
		return m.Int64ObservableGauge(name, allOpts...)
	})
}

// MeasureFloat64Func registers a callback to record an observable float64 gauge or returns a cached instance.
func (m *MetricManager) MeasureFloat64Func(name, description string, callback metric.Float64Callback, opts ...metric.Float64ObservableGaugeOption) (metric.Float64ObservableGauge, error) {
	if err := m.validate(name); err != nil {
		return nil, err
	}
	return getOrRegisterInstrument(m, name, "float64 observable gauge", &m.floatObservableGauges, func() (metric.Float64ObservableGauge, error) {
		allOpts := make([]metric.Float64ObservableGaugeOption, 0, len(opts)+2)
		if description != "" {
			allOpts = append(allOpts, metric.WithDescription(description))
		}
		if callback != nil {
			allOpts = append(allOpts, metric.WithFloat64Callback(callback))
		}
		allOpts = append(allOpts, opts...)
		return m.Float64ObservableGauge(name, allOpts...)
	})
}

// GetMeter returns the underlying OpenTelemetry Meter.
//
// Deprecated: Access the embedded Meter directly or use interface methods.
func (m *MetricManager) GetMeter() metric.Meter {
	if m == nil {
		return nil
	}
	return m.Meter
}
