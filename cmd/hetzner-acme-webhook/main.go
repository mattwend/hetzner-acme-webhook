// SPDX-FileCopyrightText: 2026 Matthias Wende
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd"
	"github.com/mattwend/hetzner-acme-webhook/internal/telemetry"
	"github.com/mattwend/hetzner-acme-webhook/internal/webhook"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	ctx := context.Background()
	shutdownTracing := telemetry.Init(ctx, logger)
	defer func() {
		if err := shutdownTracing(ctx); err != nil {
			logger.Error("tracing shutdown failed", slog.String("error", err.Error()))
		}
	}()

	client, err := webhook.NewDNSClient(logger)
	if err != nil {
		logger.Error("configure client failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	zone, err := webhook.ZoneFromEnv()
	if err != nil {
		logger.Info("health checks will run in local-only mode", slog.String("reason", err.Error()))
		zone = ""
	}

	go webhook.ServeHealth(logger, client, zone)

	cmd.RunWebhookServer(webhook.GroupName, webhook.NewSolver(logger, client))
}
