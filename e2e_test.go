// SPDX-FileCopyrightText: 2026 Matthias Wende
// SPDX-License-Identifier: GPL-3.0-only

// This file runs the cert-manager DNS01 provider conformance test suite.
//
// Prerequisites:
//   - Set HETZNER_DNS_API_TOKEN to a valid Hetzner Cloud API token
//   - Set TEST_ZONE_NAME to a zone accessible by that token (e.g. "neue-grafik.de.")
//   - The test writes testdata/hetzner/secret.yaml at runtime; it does not need
//     to be committed to the repository.
//   - Run: go test -v -count=1 -tags=e2e ./...

//go:build e2e

package main

import (
	"log/slog"
	"os"
	"path/filepath"
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

	manifestPath := prepareConformanceManifestDir(t)

	fixture := acmetest.NewFixture(solver,
		acmetest.SetResolvedZone(zone),
		acmetest.SetAllowAmbientCredentials(false),
		acmetest.SetManifestPath(manifestPath),
		// cert-manager's conformance suite uses example.com as the DNSName by
		// default. The important values for DNS01 behavior here are ResolvedZone
		// and the derived ResolvedFQDN under that zone.
	)

	fixture.RunConformance(t)
}

func prepareConformanceManifestDir(t *testing.T) string {
	t.Helper()

	tempDir := t.TempDir()
	srcDir := filepath.Join("testdata", "hetzner")
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatalf("read manifest dir: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := filepath.Ext(entry.Name())
		switch ext {
		case ".json", ".yaml", ".yml":
		default:
			continue
		}

		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(tempDir, entry.Name())

		data, err := os.ReadFile(srcPath)
		if err != nil {
			t.Fatalf("read manifest file %s: %v", srcPath, err)
		}
		if err := os.WriteFile(dstPath, data, 0o600); err != nil {
			t.Fatalf("write manifest file %s: %v", dstPath, err)
		}
	}

	return tempDir
}
