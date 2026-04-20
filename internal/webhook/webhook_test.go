// SPDX-FileCopyrightText: 2026 Matthias Wende
// SPDX-License-Identifier: GPL-3.0-only

package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestFQDNToRelative(t *testing.T) {
	got, err := fqdnToRelative("_acme-challenge.test.example.test.", "example.test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "_acme-challenge.test" {
		t.Fatalf("got %q", got)
	}
}

func TestFQDNToRelativeRejectsOutsideZone(t *testing.T) {
	if _, err := fqdnToRelative("_acme-challenge.test.other.test", "example.test"); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateRecordNameRejectsTraversal(t *testing.T) {
	if err := validateRecordName("../../evil"); err == nil {
		t.Fatal("expected error")
	}
}

func TestPresentAndCleanup(t *testing.T) {
	var posted map[string]any
	var deleted string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			_ = json.NewEncoder(w).Encode(map[string]any{"zones": []map[string]any{{"id": "zone-1", "name": "example.test"}}})
		case r.Method == http.MethodGet && r.URL.Path == "/zones/zone-1/rrsets":
			_ = json.NewEncoder(w).Encode(map[string]any{"rrsets": []map[string]any{}})
		case r.Method == http.MethodPost && r.URL.Path == "/zones/zone-1/rrsets":
			defer r.Body.Close()
			_ = json.NewDecoder(r.Body).Decode(&posted)
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodDelete:
			deleted = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	c := &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client()}
	ctx := context.Background()
	if err := c.present(ctx, "example.test", "_acme-challenge.test", "token-value"); err != nil {
		t.Fatalf("present: %v", err)
	}
	if posted["name"] != "_acme-challenge.test" || posted["type"] != "TXT" {
		t.Fatalf("unexpected payload: %#v", posted)
	}
	if err := c.cleanup(ctx, "example.test", "_acme-challenge.test"); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if deleted != "/zones/zone-1/rrsets/_acme-challenge.test/TXT" {
		t.Fatalf("unexpected delete path: %s", deleted)
	}
}

func TestCleanupEscapesRecordNameInPath(t *testing.T) {
	var deleted string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			_ = json.NewEncoder(w).Encode(map[string]any{"zones": []map[string]any{{"id": "zone-1", "name": "example.test"}}})
		case r.Method == http.MethodDelete:
			deleted = r.URL.EscapedPath()
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	c := &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client()}
	if err := c.cleanup(context.Background(), "example.test", "_acme-challenge.test"); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if deleted != "/zones/zone-1/rrsets/_acme-challenge.test/TXT" {
		t.Fatalf("unexpected escaped delete path: %s", deleted)
	}
}

func TestZoneNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"zones": []map[string]any{}})
	}))
	defer server.Close()
	c := &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client()}
	if _, err := c.zoneID(context.Background(), "missing.example"); err == nil {
		t.Fatal("expected error")
	}
}

func TestZoneFromConfigFallsBackToEnv(t *testing.T) {
	t.Setenv("HETZNER_DNS_ZONE", "env.example")
	zone, err := zoneFromConfig(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if zone != "env.example" {
		t.Fatalf("got %q", zone)
	}
}

func TestZoneFromConfigPrefersConfig(t *testing.T) {
	t.Setenv("HETZNER_DNS_ZONE", "env.example")
	zone, err := zoneFromConfig([]byte(`{"zone":"cfg.example"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if zone != "cfg.example" {
		t.Fatalf("got %q", zone)
	}
}

func TestZoneFromEnvMissing(t *testing.T) {
	_ = os.Unsetenv("HETZNER_DNS_ZONE")
	if _, err := ZoneFromEnv(); err == nil {
		t.Fatal("expected error")
	}
}

func TestNewDNSClientPrefersTokenFileOverEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HETZNER_DNS_API_TOKEN", "from-env")
	t.Setenv("HETZNER_DNS_API_BASE_URL", "https://example.invalid")
	oldPath := tokenFilePath
	t.Cleanup(func() { tokenFilePath = oldPath })
	tokenFilePath = dir + "/token"
	if err := os.WriteFile(tokenFilePath, []byte("from-file\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	c, err := NewDNSClient()
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if c.token != "from-file" {
		t.Fatalf("got token %q", c.token)
	}
}

func TestHealthCheckCachesResult(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"zones": []map[string]any{{"id": "zone-1", "name": "example.test"}}})
	}))
	defer server.Close()

	state := &healthState{
		client:  &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client()},
		zone:    "example.test",
		enabled: true,
	}
	if err := state.check(context.Background()); err != nil {
		t.Fatalf("first check: %v", err)
	}
	if err := state.check(context.Background()); err != nil {
		t.Fatalf("second check: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 upstream call, got %d", calls)
	}
}

func TestHealthCheckDisabledWithoutZone(t *testing.T) {
	state := &healthState{enabled: false}
	if err := state.check(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
