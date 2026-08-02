package telemetry

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// MetricManager provides a wrapper around OpenTelemetry Meter.
type MetricManager struct {
	Meter metric.Meter
}

// NewMetricManager creates a new MetricManager.
func NewMetricManager(instrumentationName string) *MetricManager {
	return &MetricManager{
		Meter: otel.Meter(instrumentationName),
	}
}

// RegisterCounter creates a new integer counter.
func (m *MetricManager) RegisterCounter(name, description string) (metric.Int64Counter, error) {
	return m.Meter.Int64Counter(name, metric.WithDescription(description))
}

// RegisterUpDownCounter creates a new integer up-down counter.
func (m *MetricManager) RegisterUpDownCounter(name, description string) (metric.Int64UpDownCounter, error) {
	return m.Meter.Int64UpDownCounter(name, metric.WithDescription(description))
}

// RegisterHistogram creates a new float64 histogram.
func (m *MetricManager) RegisterHistogram(name, description, unit string) (metric.Float64Histogram, error) {
	return m.Meter.Float64Histogram(name, metric.WithDescription(description), metric.WithUnit(unit))
}

// MeasureFunc registers a callback to record an observable gauge.
func (m *MetricManager) MeasureFunc(name, description string, callback metric.Int64Callback) (metric.Int64ObservableGauge, error) {
	return m.Meter.Int64ObservableGauge(name, metric.WithDescription(description), metric.WithInt64Callback(callback))
}

// GetMeter returns the underlying OpenTelemetry Meter.
func (m *MetricManager) GetMeter() metric.Meter {
	return m.Meter
}

