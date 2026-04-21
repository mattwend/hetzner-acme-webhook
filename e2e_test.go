// SPDX-FileCopyrightText: 2026 Matthias Wende
// SPDX-License-Identifier: GPL-3.0-only

// This file runs the cert-manager DNS01 provider conformance test suite.
//
// Prerequisites:
//   - Set HETZNER_DNS_API_TOKEN to a valid Hetzner Cloud API token
//   - Set TEST_ZONE_NAME to a zone accessible by that token (e.g. "example.com.")
//   - Place the token in testdata/hetzner/secret.yaml matching the reference
//     in testdata/hetzner/config.json
//   - Run: go test -v -count=1 -tags=e2e ./...

//go:build e2e

package main

import (
	"log/slog"
	"os"
	"testing"

	acmetest "github.com/cert-manager/cert-manager/test/acme"
	"github.com/mattwend/hetzner-acme-webhook/internal/webhook"
)

var (
	zone = os.Getenv("TEST_ZONE_NAME")
)

func TestRunsSuite(t *testing.T) {
	if zone == "" {
		t.Skip("TEST_ZONE_NAME not set, skipping conformance tests")
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	client, err := webhook.NewDNSClient(logger)
	if err != nil {
		t.Fatalf("create DNS client: %v", err)
	}

	solver := webhook.NewSolver(logger, client)

	fixture := acmetest.NewFixture(solver,
		acmetest.SetResolvedZone(zone),
		acmetest.SetAllowAmbientCredentials(false),
		acmetest.SetManifestPath("testdata/hetzner"),
	)

	fixture.RunConformance(t)
}
