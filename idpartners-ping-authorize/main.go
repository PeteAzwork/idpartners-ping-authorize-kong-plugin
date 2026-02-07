package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Kong/go-pdk"
	"github.com/Kong/go-pdk/server"
)

const (
	PluginName = "idpartners-ping-authorize"
	Version    = "2.0.0"
	Priority   = 999
)

// New returns a new plugin configuration instance.
func New() interface{} {
	return &Config{
		// Defaults that match DESIGN.md §3.1
		ConnectionTimeoutMs:   10000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     true,
		PassthroughStatusCodes: []int{413},
		RetryBackoffMs:        500,
		CircuitBreakerEnabled: true,
		StripAcceptEncoding:   true,
		RedactHeaders:         []string{"authorization", "cookie"},
		DebugBodyMaxBytes:     8192,
	}
}

// Access is the Kong access phase handler.
func (conf *Config) Access(kong *pdk.PDK) {
	defer func() {
		if r := recover(); r != nil {
			kong.Log.Err(fmt.Sprintf("[%s] Unexpected panic in access phase: %v", PluginName, r))
			kong.Response.Exit(500, nil, nil)
		}
	}()
	executeAccess(kong, conf)
}

// Response is the Kong response phase handler.
func (conf *Config) Response(kong *pdk.PDK) {
	if conf.SkipResponsePhase {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			kong.Log.Err(fmt.Sprintf("[%s] Unexpected panic in response phase: %v", PluginName, r))
			kong.Response.Exit(500, nil, nil)
		}
	}()
	executeResponse(kong, conf)
}

func main() {
	// Optional OTel initialization
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		ctx := context.Background()
		shutdown, _, err := InitOTel(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] Failed to initialize OpenTelemetry: %v\n", PluginName, err)
		} else if shutdown != nil {
			defer shutdown(ctx)
		}
	}

	err := server.StartServer(New, Version, Priority)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] Failed to start server: %v\n", PluginName, err)
		os.Exit(1)
	}
}
