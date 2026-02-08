package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Kong/go-pdk"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// PluginLogger wraps Kong PDK log with structured fields.
type PluginLogger struct {
	kong       *pdk.PDK
	phase      string
	serviceURL string
}

// NewPluginLogger creates a logger with standard plugin context fields.
func NewPluginLogger(kong *pdk.PDK, phase, serviceURL string) *PluginLogger {
	return &PluginLogger{
		kong:       kong,
		phase:      phase,
		serviceURL: serviceURL,
	}
}

func (l *PluginLogger) formatMsg(level, msg string, kvs ...interface{}) string {
	entry := map[string]interface{}{
		"plugin":      PluginName,
		"phase":       l.phase,
		"service_url": l.serviceURL,
		"level":       level,
		"msg":         msg,
	}
	for i := 0; i+1 < len(kvs); i += 2 {
		key, ok := kvs[i].(string)
		if ok {
			entry[key] = kvs[i+1]
		}
	}
	b, _ := json.Marshal(entry)
	return string(b)
}

// Debug logs at debug level.
func (l *PluginLogger) Debug(msg string, kvs ...interface{}) {
	l.kong.Log.Debug(l.formatMsg("debug", msg, kvs...))
}

// Info logs at info level.
func (l *PluginLogger) Info(msg string, kvs ...interface{}) {
	l.kong.Log.Info(l.formatMsg("info", msg, kvs...))
}

// Warn logs at warn level.
func (l *PluginLogger) Warn(msg string, kvs ...interface{}) {
	l.kong.Log.Warn(l.formatMsg("warn", msg, kvs...))
}

// Err logs at error level.
func (l *PluginLogger) Err(msg string, kvs ...interface{}) {
	l.kong.Log.Err(l.formatMsg("error", msg, kvs...))
}

// PluginMetrics holds pre-created OTel instruments.
type PluginMetrics struct {
	SidebandDuration  metric.Float64Histogram
	SidebandTotal     metric.Int64Counter
	CircuitBreakerSt  metric.Int64Gauge
	PolicyDecisions   metric.Int64Counter
	MCPRequestsTotal  metric.Int64Counter // MCP requests by mcp_method
	MCPDeniedTotal    metric.Int64Counter // MCP denied requests by mcp_method, reason
	MCPToolCallsTotal metric.Int64Counter // MCP tool calls by tool_name
}

// InitOTel initializes OpenTelemetry trace and metric providers.
func InitOTel(ctx context.Context) (func(context.Context) error, *PluginMetrics, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(PluginName),
			semconv.ServiceVersionKey.String(Version),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create OTel resource: %w", err)
	}

	// Trace exporter
	traceExporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tracerProvider)

	// Metric exporter
	metricExporter, err := otlpmetricgrpc.New(ctx)
	if err != nil {
		tracerProvider.Shutdown(ctx)
		return nil, nil, fmt.Errorf("failed to create metric exporter: %w", err)
	}

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(meterProvider)

	// Create instruments
	meter := meterProvider.Meter(PluginName)

	sidebandDuration, _ := meter.Float64Histogram("ping_authorize_sideband_duration_ms",
		metric.WithDescription("Sideband call latency in milliseconds"))
	sidebandTotal, _ := meter.Int64Counter("ping_authorize_sideband_total",
		metric.WithDescription("Total sideband calls"))
	cbState, _ := meter.Int64Gauge("ping_authorize_circuit_breaker_state",
		metric.WithDescription("Circuit breaker state: 0=closed, 1=open"))
	policyDecisions, _ := meter.Int64Counter("ping_authorize_policy_decisions_total",
		metric.WithDescription("Policy decision counts"))

	// MCP-specific metrics
	mcpRequestsTotal, _ := meter.Int64Counter("ping_authorize_mcp_requests_total",
		metric.WithDescription("Total MCP requests by method"))
	mcpDeniedTotal, _ := meter.Int64Counter("ping_authorize_mcp_denied_total",
		metric.WithDescription("Total MCP denied requests by method and reason"))
	mcpToolCallsTotal, _ := meter.Int64Counter("ping_authorize_mcp_tool_calls_total",
		metric.WithDescription("Total MCP tool calls by tool name"))

	metrics := &PluginMetrics{
		SidebandDuration:  sidebandDuration,
		SidebandTotal:     sidebandTotal,
		CircuitBreakerSt:  cbState,
		PolicyDecisions:   policyDecisions,
		MCPRequestsTotal:  mcpRequestsTotal,
		MCPDeniedTotal:    mcpDeniedTotal,
		MCPToolCallsTotal: mcpToolCallsTotal,
	}

	shutdown := func(ctx context.Context) error {
		var errs []error
		if err := tracerProvider.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
		if err := meterProvider.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
		if len(errs) > 0 {
			return errs[0]
		}
		return nil
	}

	return shutdown, metrics, nil
}

// RedactHeaders replaces values of sensitive headers with [REDACTED].
// The secretHeaderName is always redacted regardless of the redact set.
func RedactHeaders(headers []map[string]string, redactSet map[string]bool, secretHeaderName string) []map[string]string {
	if len(headers) == 0 {
		return headers
	}

	result := make([]map[string]string, len(headers))
	secretLower := strings.ToLower(secretHeaderName)

	for i, entry := range headers {
		newEntry := make(map[string]string)
		for name, value := range entry {
			lower := strings.ToLower(name)
			if redactSet[lower] || lower == secretLower {
				newEntry[name] = "[REDACTED]"
			} else {
				newEntry[name] = value
			}
		}
		result[i] = newEntry
	}
	return result
}

// TruncateBody truncates a body string if it exceeds maxBytes.
// If maxBytes is 0, no truncation is performed.
func TruncateBody(body string, maxBytes int) string {
	if maxBytes <= 0 || len(body) <= maxBytes {
		return body
	}
	return body[:maxBytes] + fmt.Sprintf("... [truncated, %d bytes]", len(body))
}

// DebugLogPayload logs a sideband payload with redaction and truncation.
// When MCP context is present, logs mcp_method, mcp_tool_name, and traffic_type at Info level.
func DebugLogPayload(logger *PluginLogger, direction string, payload interface{}, config *Config) {
	if !config.EnableDebugLogging {
		return
	}

	// Log MCP-specific fields at Info level when MCP is detected
	if config.EnableMCP {
		logMCPContext(logger, direction, payload, config)
	}

	b, err := json.Marshal(payload)
	if err != nil {
		logger.Debug("Failed to marshal payload for debug logging", "error", err.Error())
		return
	}

	body := TruncateBody(string(b), config.DebugBodyMaxBytes)
	logger.Debug(direction, "payload", body)
}

// logMCPContext extracts and logs MCP-specific fields from sideband payloads.
func logMCPContext(logger *PluginLogger, direction string, payload interface{}, config *Config) {
	var mcpCtx *MCPContext
	var trafficType string

	switch p := payload.(type) {
	case *SidebandAccessRequest:
		mcpCtx = p.MCP
		trafficType = p.TrafficType
	case *SidebandResponsePayload:
		mcpCtx = p.MCP
		trafficType = p.TrafficType
	default:
		return
	}

	if mcpCtx == nil {
		return
	}

	kvs := []interface{}{
		"traffic_type", trafficType,
		"mcp_method", mcpCtx.Method,
	}

	if mcpCtx.ToolName != "" {
		kvs = append(kvs, "mcp_tool_name", mcpCtx.ToolName)
	}
	if mcpCtx.ResourceURI != "" {
		kvs = append(kvs, "mcp_resource_uri", mcpCtx.ResourceURI)
	}
	if mcpCtx.PromptName != "" {
		kvs = append(kvs, "mcp_prompt_name", mcpCtx.PromptName)
	}
	if mcpCtx.ToolArguments != nil {
		args := TruncateBody(string(mcpCtx.ToolArguments), config.DebugBodyMaxBytes)
		kvs = append(kvs, "mcp_tool_arguments", args)
	}

	logger.Info(direction+" [MCP]", kvs...)
}
