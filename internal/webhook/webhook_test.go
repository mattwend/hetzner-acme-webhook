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

	acmev1 "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
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

func TestMatchZoneByFQDNLongestSuffixWins(t *testing.T) {
	z, err := matchZoneByFQDN("_acme-challenge.test.sub.example.com.", []zone{{ID: "1", Name: "example.com"}, {ID: "2", Name: "sub.example.com"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if z.ID != "2" || z.Name != "sub.example.com" {
		t.Fatalf("unexpected zone: %#v", z)
	}
}

func TestMatchZoneByFQDNNoMatch(t *testing.T) {
	if _, err := matchZoneByFQDN("_acme-challenge.test.other.example", []zone{{ID: "1", Name: "example.com"}}); err == nil {
		t.Fatal("expected error")
	}
}

func TestMatchZoneByFQDNNormalizesCaseAndTrailingDot(t *testing.T) {
	z, err := matchZoneByFQDN(" _acme-challenge.Test.Example.COM. ", []zone{{ID: "1", Name: "example.com."}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if z.ID != "1" || z.Name != "example.com." {
		t.Fatalf("unexpected zone: %#v", z)
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

func TestResolveZonePrefersConfigWithoutClientLookup(t *testing.T) {
	s := &Solver{}
	ch := &acmev1.ChallengeRequest{ResolvedFQDN: "_acme-challenge.test.cfg.example.", Config: &apiextensionsv1.JSON{Raw: []byte(`{"zone":"cfg.example"}`)}}
	z, err := s.resolveZone(context.Background(), ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if z.Name != "cfg.example" {
		t.Fatalf("got zone %q", z.Name)
	}
}

func TestResolveZonePrefersEnvWithoutClientLookup(t *testing.T) {
	t.Setenv("HETZNER_DNS_ZONE", "env.example")
	s := &Solver{}
	ch := &acmev1.ChallengeRequest{ResolvedFQDN: "_acme-challenge.test.env.example."}
	z, err := s.resolveZone(context.Background(), ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if z.Name != "env.example" {
		t.Fatalf("got zone %q", z.Name)
	}
}

func TestResolveZoneAutoDetect(t *testing.T) {
	t.Setenv("HETZNER_DNS_ZONE", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/zones" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"zones": []map[string]any{{"id": "zone-1", "name": "example.com"}, {"id": "zone-2", "name": "sub.example.com"}}})
	}))
	defer server.Close()

	s := &Solver{client: &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client()}}
	ch := &acmev1.ChallengeRequest{ResolvedFQDN: "_acme-challenge.test.sub.example.com."}
	z, err := s.resolveZone(context.Background(), ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if z.ID != "zone-2" || z.Name != "sub.example.com" {
		t.Fatalf("unexpected zone: %#v", z)
	}
}

func TestResolveZoneAutoDetectNoMatchingZones(t *testing.T) {
	t.Setenv("HETZNER_DNS_ZONE", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/zones" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"zones": []map[string]any{}})
	}))
	defer server.Close()

	s := &Solver{client: &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client()}}
	ch := &acmev1.ChallengeRequest{ResolvedFQDN: "_acme-challenge.test.sub.example.com."}
	if _, err := s.resolveZone(context.Background(), ch); err == nil {
		t.Fatal("expected error")
	}
}

func TestZoneFromEnvMissing(t *testing.T) {
	t.Setenv("HETZNER_DNS_ZONE", "")
	if _, err := ZoneFromEnv(); err == nil {
		t.Fatal("expected error")
	}
}

func TestListZonesFollowsPagination(t *testing.T) {
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path + "?" + r.URL.RawQuery {
		case "/zones?":
			w.Header().Set("Link", "<"+serverURL+"/zones?page=2>; rel=\"next\"")
			_ = json.NewEncoder(w).Encode(map[string]any{"zones": []map[string]any{{"id": "zone-1", "name": "example.com"}}})
		case "/zones?page=2":
			_ = json.NewEncoder(w).Encode(map[string]any{"zones": []map[string]any{{"id": "zone-2", "name": "sub.example.com"}}})
		default:
			t.Fatalf("unexpected request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	serverURL = server.URL
	defer server.Close()

	c := &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client()}
	zones, err := c.listZones(context.Background())
	if err != nil {
		t.Fatalf("listZones: %v", err)
	}
	if len(zones) != 2 {
		t.Fatalf("expected 2 zones, got %d", len(zones))
	}
	if zones[1].ID != "zone-2" || zones[1].Name != "sub.example.com" {
		t.Fatalf("unexpected second zone: %#v", zones[1])
	}
}

func TestPresentWithExplicitZoneResolvesIDBeforeMutation(t *testing.T) {
	requests := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			_ = json.NewEncoder(w).Encode(map[string]any{"zones": []map[string]any{{"id": "zone-1", "name": "example.test"}}})
		case r.Method == http.MethodGet && r.URL.Path == "/zones/zone-1/rrsets":
			_ = json.NewEncoder(w).Encode(map[string]any{"rrsets": []map[string]any{}})
		case r.Method == http.MethodPost && r.URL.Path == "/zones/zone-1/rrsets":
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	s := &Solver{client: &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client()}}
	ch := &acmev1.ChallengeRequest{ResolvedFQDN: "_acme-challenge.test.example.test.", Key: "token-value", Config: &apiextensionsv1.JSON{Raw: []byte(`{"zone":"example.test"}`)}}
	if err := s.Present(ch); err != nil {
		t.Fatalf("present: %v", err)
	}
	if len(requests) != 3 {
		t.Fatalf("expected 3 requests, got %d: %#v", len(requests), requests)
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
