package telemetry

// Config holds the OpenTelemetry configuration.
type Config struct {
	Enabled       bool    `yaml:"enabled" default:"true"`
	Endpoint      string  `yaml:"endpoint" default:"localhost:4317"`
	Insecure      bool    `yaml:"insecure" default:"true"`
	ServiceName   string  `yaml:"service_name" default:"nvx-go-driver"`
	EnableTrace   bool    `yaml:"enable_trace" default:"true"`
	EnableMetrics bool    `yaml:"enable_metrics" default:"true"`
	TraceRatio    float64 `yaml:"trace_ratio" default:"1.0"`
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

// WithTraceRatio sets the sampling ratio for tracing (0.0 to 1.0).
func WithTraceRatio(ratio float64) Option {
	return func(c *Config) {
		c.TraceRatio = ratio
	}
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		Enabled:       true,
		Endpoint:      "localhost:4317",
		Insecure:      true,
		ServiceName:   "nvx-go-app",
		EnableTrace:   true,
		EnableMetrics: true,
		TraceRatio:    1.0,
	}
}
