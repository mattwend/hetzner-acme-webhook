// SPDX-FileCopyrightText: 2026 Matthias Wende
// SPDX-License-Identifier: GPL-3.0-only

// Package telemetry configures OpenTelemetry tracing with OTLP export.
//
// Tracing is enabled when OTEL_EXPORTER_OTLP_ENDPOINT (or
// OTEL_EXPORTER_OTLP_TRACES_ENDPOINT) is set. When the variable is absent
// or empty, Init returns a no-op shutdown function and tracing calls become
// zero-cost no-ops via the default global TracerProvider.
package telemetry

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const serviceName = "hetzner-acme-webhook"

// Init sets up the global TracerProvider with OTLP/gRPC export when
// OTEL_EXPORTER_OTLP_ENDPOINT is configured. Returns a shutdown function
// that must be called on program exit.
func Init(ctx context.Context, logger *slog.Logger) (shutdown func(context.Context) error) {
	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	tracesEndpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"))

	if endpoint == "" && tracesEndpoint == "" {
		logger.Info("OTLP tracing disabled (OTEL_EXPORTER_OTLP_ENDPOINT not set)")
		return func(context.Context) error { return nil }
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		logger.Warn("failed to create OTel resource, using default", slog.String("error", err.Error()))
		res = resource.Default()
	}

	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		logger.Error("failed to create OTLP exporter, tracing disabled", slog.String("error", err.Error()))
		return func(context.Context) error { return nil }
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(5*time.Second),
		),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	target := endpoint
	if tracesEndpoint != "" {
		target = tracesEndpoint
	}
	logger.Info("OTLP tracing enabled", slog.String("endpoint", target))

	return tp.Shutdown
}
