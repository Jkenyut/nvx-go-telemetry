package telemetry

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
)

// Config holds the OpenTelemetry configuration.
type Config struct {
	Enabled         bool              `yaml:"enabled" default:"true"`
	Endpoint        string            `yaml:"endpoint" default:"localhost:4317"`
	Insecure        bool              `yaml:"insecure" default:"true"`
	Headers         map[string]string `yaml:"headers"` // API Keys / Auth tokens
	ServiceName     string            `yaml:"service_name" default:"nvx-go-telemetry"`
	Environment     string            `yaml:"environment"`
	ServiceVersion  string            `yaml:"service_version"`
	EnableTrace     bool              `yaml:"enable_trace" default:"true"`
	EnableMetrics   bool              `yaml:"enable_metrics" default:"true"`
	TraceRatio      float64           `yaml:"trace_ratio" default:"1.0"`
	ShutdownTimeout int64             `yaml:"shutdown_timeout" default:"5000"` // in milliseconds
	Logger          *slog.Logger      `yaml:"-"`
}

// Option defines a functional option for Config.
type Option func(*Config)

// WithEnabled sets whether telemetry is enabled.
func WithEnabled(enabled bool) Option {
	return func(c *Config) {
		c.Enabled = enabled
	}
}

// WithEndpoint sets the OTLP endpoint.
func WithEndpoint(endpoint string) Option {
	return func(c *Config) {
		c.Endpoint = endpoint
	}
}

// WithServiceName sets the service name.
func WithServiceName(name string) Option {
	return func(c *Config) {
		c.ServiceName = name
	}
}

// WithEnvironment sets the deployment environment (e.g. production, staging).
func WithEnvironment(env string) Option {
	return func(c *Config) {
		c.Environment = env
	}
}

// WithServiceVersion sets the service version.
func WithServiceVersion(version string) Option {
	return func(c *Config) {
		c.ServiceVersion = version
	}
}

// WithTraceEnabled enables or disables tracing.
func WithTraceEnabled(enabled bool) Option {
	return func(c *Config) {
		c.EnableTrace = enabled
	}
}

// WithMetricsEnabled enables or disables metrics.
func WithMetricsEnabled(enabled bool) Option {
	return func(c *Config) {
		c.EnableMetrics = enabled
	}
}

// WithInsecure sets whether the OTLP endpoint is insecure.
func WithInsecure(insecure bool) Option {
	return func(c *Config) {
		c.Insecure = insecure
	}
}

// WithHeaders sets the headers for the OTLP exporters.
// It creates a defensive copy of the headers map to prevent data races.
func WithHeaders(headers map[string]string) Option {
	return func(c *Config) {
		c.Headers = maps.Clone(headers)
	}
}

// WithTraceRatio sets the sampling ratio for tracing (0.0 to 1.0).
func WithTraceRatio(ratio float64) Option {
	return func(c *Config) {
		c.TraceRatio = ratio
	}
}

// WithShutdownTimeout sets the timeout duration in milliseconds for graceful shutdown.
func WithShutdownTimeout(timeout int64) Option {
	return func(c *Config) {
		c.ShutdownTimeout = timeout
	}
}

// WithLogger sets a custom slog logger for internal errors.
func WithLogger(logger *slog.Logger) Option {
	return func(c *Config) {
		c.Logger = logger
	}
}

// WithConfig applies settings from an existing Config struct.
// It creates defensive copies of maps to prevent data races.
func WithConfig(src *Config) Option {
	return func(c *Config) {
		if src == nil {
			return
		}
		c.Enabled = src.Enabled
		c.Endpoint = src.Endpoint
		c.Insecure = src.Insecure
		c.Headers = maps.Clone(src.Headers)
		c.ServiceName = src.ServiceName
		c.Environment = src.Environment
		c.ServiceVersion = src.ServiceVersion
		c.EnableTrace = src.EnableTrace
		c.EnableMetrics = src.EnableMetrics
		c.TraceRatio = src.TraceRatio
		c.ShutdownTimeout = src.ShutdownTimeout
		if src.Logger != nil {
			c.Logger = src.Logger
		}
	}
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		Enabled:         true,
		Endpoint:        "localhost:4317",
		Insecure:        true,
		ServiceName:     "nvx-go-telemetry",
		EnableTrace:     true,
		EnableMetrics:   true,
		TraceRatio:      1.0,
		ShutdownTimeout: 5000,
	}
}

// Validate checks whether the configuration is valid.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("telemetry config is nil")
	}
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.Endpoint) == "" {
		return errors.New("telemetry endpoint cannot be empty when enabled")
	}
	if strings.Contains(c.Endpoint, "://") {
		return fmt.Errorf("telemetry endpoint %q must not contain a URL scheme (e.g. http:// or https://)", c.Endpoint)
	}
	if strings.TrimSpace(c.ServiceName) == "" {
		return errors.New("telemetry service name cannot be empty when enabled")
	}
	if c.TraceRatio < 0.0 || c.TraceRatio > 1.0 {
		return fmt.Errorf("telemetry trace ratio must be between 0.0 and 1.0, got: %f", c.TraceRatio)
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 5000
	}
	return nil
}

// String implements fmt.Stringer with sensitive headers redacted.
func (c *Config) String() string {
	if c == nil {
		return "<nil>"
	}
	redactedHeaders := make(map[string]string, len(c.Headers))
	for k := range c.Headers {
		redactedHeaders[k] = "[REDACTED]"
	}
	return fmt.Sprintf("Config{Enabled:%t, Endpoint:%q, Insecure:%t, ServiceName:%q, Environment:%q, ServiceVersion:%q, EnableTrace:%t, EnableMetrics:%t, TraceRatio:%.2f, ShutdownTimeout:%dms, Headers:%v}",
		c.Enabled, c.Endpoint, c.Insecure, c.ServiceName, c.Environment, c.ServiceVersion, c.EnableTrace, c.EnableMetrics, c.TraceRatio, c.ShutdownTimeout, redactedHeaders)
}

// GoString implements fmt.GoStringer with sensitive headers redacted.
func (c *Config) GoString() string {
	return c.String()
}
