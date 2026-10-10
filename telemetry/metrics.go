package telemetry

import (
	"errors"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// MetricManager provides a wrapper around OpenTelemetry Meter.
type MetricManager struct {
	metric.Meter
}

// NewMetricManager creates a new MetricManager.
func NewMetricManager(instrumentationName string) *MetricManager {
	if instrumentationName == "" {
		instrumentationName = "unknown-meter"
	}
	return &MetricManager{
		Meter: otel.Meter(instrumentationName),
	}
}

// RegisterCounter creates a new integer counter.
func (m *MetricManager) RegisterCounter(name, description string, opts ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if m == nil || m.Meter == nil {
		return nil, errors.New("metric manager is uninitialized")
	}
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("metric name cannot be empty")
	}
	allOpts := make([]metric.Int64CounterOption, 0, len(opts)+1)
	if description != "" {
		allOpts = append(allOpts, metric.WithDescription(description))
	}
	allOpts = append(allOpts, opts...)
	c, err := m.Int64Counter(name, allOpts...)
	if err != nil {
		return nil, fmt.Errorf("registering counter %q: %w", name, err)
	}
	return c, nil
}

// RegisterUpDownCounter creates a new integer up-down counter.
func (m *MetricManager) RegisterUpDownCounter(name, description string, opts ...metric.Int64UpDownCounterOption) (metric.Int64UpDownCounter, error) {
	if m == nil || m.Meter == nil {
		return nil, errors.New("metric manager is uninitialized")
	}
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("metric name cannot be empty")
	}
	allOpts := make([]metric.Int64UpDownCounterOption, 0, len(opts)+1)
	if description != "" {
		allOpts = append(allOpts, metric.WithDescription(description))
	}
	allOpts = append(allOpts, opts...)
	c, err := m.Int64UpDownCounter(name, allOpts...)
	if err != nil {
		return nil, fmt.Errorf("registering up-down counter %q: %w", name, err)
	}
	return c, nil
}

// RegisterHistogram creates a new float64 histogram.
func (m *MetricManager) RegisterHistogram(name, description, unit string, opts ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	if m == nil || m.Meter == nil {
		return nil, errors.New("metric manager is uninitialized")
	}
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("metric name cannot be empty")
	}
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
