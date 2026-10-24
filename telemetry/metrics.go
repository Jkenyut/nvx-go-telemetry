package telemetry

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// MetricManager provides a wrapper around OpenTelemetry Meter with thread-safe instrument caching.
type MetricManager struct {
	metric.Meter
	mu             sync.RWMutex
	counters       map[string]metric.Int64Counter
	upDownCounters map[string]metric.Int64UpDownCounter
	histograms     map[string]metric.Float64Histogram
}

// NewMetricManager creates a new MetricManager.
func NewMetricManager(instrumentationName string) *MetricManager {
	if instrumentationName == "" {
		instrumentationName = "unknown-meter"
	}
	return &MetricManager{
		Meter:          otel.Meter(instrumentationName),
		counters:       make(map[string]metric.Int64Counter),
		upDownCounters: make(map[string]metric.Int64UpDownCounter),
		histograms:     make(map[string]metric.Float64Histogram),
	}
}

// RegisterCounter creates a new integer counter or returns a cached instance.
func (m *MetricManager) RegisterCounter(name, description string, opts ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if m == nil || m.Meter == nil {
		return nil, errors.New("metric manager is uninitialized")
	}
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("metric name cannot be empty")
	}

	m.mu.RLock()
	if c, ok := m.counters[name]; ok {
		m.mu.RUnlock()
		return c, nil
	}
	m.mu.RUnlock()

	allOpts := make([]metric.Int64CounterOption, 0, len(opts)+1)
	if description != "" {
		allOpts = append(allOpts, metric.WithDescription(description))
	}
	allOpts = append(allOpts, opts...)
	c, err := m.Int64Counter(name, allOpts...)
	if err != nil {
		return nil, fmt.Errorf("registering counter %q: %w", name, err)
	}

	m.mu.Lock()
	if m.counters == nil {
		m.counters = make(map[string]metric.Int64Counter)
	}
	if existing, ok := m.counters[name]; ok {
		m.mu.Unlock()
		return existing, nil
	}
	m.counters[name] = c
	m.mu.Unlock()

	return c, nil
}

// RegisterUpDownCounter creates a new integer up-down counter or returns a cached instance.
func (m *MetricManager) RegisterUpDownCounter(name, description string, opts ...metric.Int64UpDownCounterOption) (metric.Int64UpDownCounter, error) {
	if m == nil || m.Meter == nil {
		return nil, errors.New("metric manager is uninitialized")
	}
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("metric name cannot be empty")
	}

	m.mu.RLock()
	if c, ok := m.upDownCounters[name]; ok {
		m.mu.RUnlock()
		return c, nil
	}
	m.mu.RUnlock()

	allOpts := make([]metric.Int64UpDownCounterOption, 0, len(opts)+1)
	if description != "" {
		allOpts = append(allOpts, metric.WithDescription(description))
	}
	allOpts = append(allOpts, opts...)
	c, err := m.Int64UpDownCounter(name, allOpts...)
	if err != nil {
		return nil, fmt.Errorf("registering up-down counter %q: %w", name, err)
	}

	m.mu.Lock()
	if m.upDownCounters == nil {
		m.upDownCounters = make(map[string]metric.Int64UpDownCounter)
	}
	if existing, ok := m.upDownCounters[name]; ok {
		m.mu.Unlock()
		return existing, nil
	}
	m.upDownCounters[name] = c
	m.mu.Unlock()

	return c, nil
}

// RegisterHistogram creates a new float64 histogram or returns a cached instance.
func (m *MetricManager) RegisterHistogram(name, description, unit string, opts ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	if m == nil || m.Meter == nil {
		return nil, errors.New("metric manager is uninitialized")
	}
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("metric name cannot be empty")
	}

	m.mu.RLock()
	if h, ok := m.histograms[name]; ok {
		m.mu.RUnlock()
		return h, nil
	}
	m.mu.RUnlock()

	allOpts := make([]metric.Float64HistogramOption, 0, len(opts)+2)
	if description != "" {
		allOpts = append(allOpts, metric.WithDescription(description))
	}
	if unit != "" {
		allOpts = append(allOpts, metric.WithUnit(unit))
	}
	allOpts = append(allOpts, opts...)
	h, err := m.Float64Histogram(name, allOpts...)
	if err != nil {
		return nil, fmt.Errorf("registering histogram %q: %w", name, err)
	}

	m.mu.Lock()
	if m.histograms == nil {
		m.histograms = make(map[string]metric.Float64Histogram)
	}
	if existing, ok := m.histograms[name]; ok {
		m.mu.Unlock()
		return existing, nil
	}
	m.histograms[name] = h
	m.mu.Unlock()

	return h, nil
}

// MeasureFunc registers a callback to record an observable gauge.
func (m *MetricManager) MeasureFunc(name, description string, callback metric.Int64Callback, opts ...metric.Int64ObservableGaugeOption) (metric.Int64ObservableGauge, error) {
	if m == nil || m.Meter == nil {
		return nil, errors.New("metric manager is uninitialized")
	}
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("metric name cannot be empty")
	}
	allOpts := make([]metric.Int64ObservableGaugeOption, 0, len(opts)+2)
	if description != "" {
		allOpts = append(allOpts, metric.WithDescription(description))
	}
	if callback != nil {
		allOpts = append(allOpts, metric.WithInt64Callback(callback))
	}
	allOpts = append(allOpts, opts...)
	g, err := m.Int64ObservableGauge(name, allOpts...)
	if err != nil {
		return nil, fmt.Errorf("registering observable gauge %q: %w", name, err)
	}
	return g, nil
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
