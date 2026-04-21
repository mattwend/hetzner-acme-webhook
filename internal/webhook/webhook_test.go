// SPDX-FileCopyrightText: 2026 Matthias Wende
// SPDX-License-Identifier: GPL-3.0-only

package webhook

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	acmev1 "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

func TestFQDNToRelative(t *testing.T) {
	got, err := fqdnToRelative("_acme-challenge.test.example.test.", "example.test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "_acme-challenge.test" {
		t.Fatalf("got %q", got)
	}
}

func TestFQDNToRelativeIDN(t *testing.T) {
	got, err := fqdnToRelative("_acme-challenge.münchen.de.", "münchen.de")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "_acme-challenge" {
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

func TestValidateRecordNameRejectsURLUnsafeChars(t *testing.T) {
	for _, name := range []string{"foo bar", "foo%20bar", "foo?bar", "foo#bar"} {
		if err := validateRecordName(name); err == nil {
			t.Fatalf("expected error for %q", name)
		}
	}
}

func TestPresentAndCleanup(t *testing.T) {
	var presentBody map[string]any
	var cleanupBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			_ = json.NewEncoder(w).Encode(map[string]any{"zones": []map[string]any{{"id": "zone-1", "name": "example.test"}}})
		case r.Method == http.MethodPost && r.URL.Path == "/zones/zone-1/rrsets/_acme-challenge.test/TXT/actions/add_records":
			defer r.Body.Close()
			_ = json.NewDecoder(r.Body).Decode(&presentBody)
			_ = json.NewEncoder(w).Encode(map[string]any{"action": map[string]any{"id": 1, "status": "success"}})
		case r.Method == http.MethodPost && r.URL.Path == "/zones/zone-1/rrsets/_acme-challenge.test/TXT/actions/remove_records":
			defer r.Body.Close()
			_ = json.NewDecoder(r.Body).Decode(&cleanupBody)
			_ = json.NewEncoder(w).Encode(map[string]any{"action": map[string]any{"id": 2, "status": "success"}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	c := &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client(), logger: testLogger}
	ctx := context.Background()
	z := zone{Name: "example.test"}
	if err := c.presentZone(ctx, z, "_acme-challenge.test", "token-value"); err != nil {
		t.Fatalf("present: %v", err)
	}
	if got := presentBody["ttl"]; got != float64(60) {
		t.Fatalf("unexpected ttl: %#v", got)
	}
	presentRecords, ok := presentBody["records"].([]any)
	if !ok || len(presentRecords) != 1 {
		t.Fatalf("unexpected present payload: %#v", presentBody)
	}
	presentRecord, ok := presentRecords[0].(map[string]any)
	if !ok || presentRecord["value"] != `"token-value"` {
		t.Fatalf("unexpected present payload: %#v", presentBody)
	}
	if err := c.cleanupZone(ctx, z, "_acme-challenge.test", "token-value"); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	cleanupRecords, ok := cleanupBody["records"].([]any)
	if !ok || len(cleanupRecords) != 1 {
		t.Fatalf("unexpected cleanup payload: %#v", cleanupBody)
	}
	cleanupRecord, ok := cleanupRecords[0].(map[string]any)
	if !ok || cleanupRecord["value"] != `"token-value"` {
		t.Fatalf("unexpected cleanup payload: %#v", cleanupBody)
	}
}

func TestCleanupUsesCorrectPath(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			_ = json.NewEncoder(w).Encode(map[string]any{"zones": []map[string]any{{"id": "zone-1", "name": "example.test"}}})
		case r.Method == http.MethodPost:
			path = r.URL.EscapedPath()
			_ = json.NewEncoder(w).Encode(map[string]any{"action": map[string]any{"id": 2, "status": "success"}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	c := &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client(), logger: testLogger}
	if err := c.cleanupZone(context.Background(), zone{Name: "example.test"}, "_acme-challenge.test", "token-value"); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if path != "/zones/zone-1/rrsets/_acme-challenge.test/TXT/actions/remove_records" {
		t.Fatalf("unexpected escaped path: %s", path)
	}
}

func TestFormatTXTRecord(t *testing.T) {
	if got := formatTXTRecord("hello"); got != `"hello"` {
		t.Fatalf("got %q", got)
	}
	if got := formatTXTRecord(`a"b`); got != `"a\"b"` {
		t.Fatalf("got %q", got)
	}
	exact := strings.Repeat("a", 255)
	if got := formatTXTRecord(exact); got != `"`+exact+`"` {
		t.Fatalf("unexpected exact 255-byte output length=%d", len(got))
	}
	over := strings.Repeat("a", 256)
	expected := `"` + strings.Repeat("a", 255) + `" "a"`
	if got := formatTXTRecord(over); got != expected {
		t.Fatalf("got %q", got)
	}
}

func TestPollActionSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/actions/1" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"action": map[string]any{"id": 1, "status": "success"}})
	}))
	defer server.Close()

	c := &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client(), pollInterval: time.Millisecond, logger: testLogger}
	if err := c.pollAction(context.Background(), 1); err != nil {
		t.Fatalf("pollAction: %v", err)
	}
}

func TestPollActionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/actions/1" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"action": map[string]any{"id": 1, "status": "error", "error": map[string]any{"code": "zone_error", "message": "fail"}}})
	}))
	defer server.Close()

	c := &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client(), pollInterval: time.Millisecond, logger: testLogger}
	err := c.pollAction(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "fail") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPollActionWaitsForCompletion(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/actions/1" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		calls++
		status := "running"
		if calls == 2 {
			status = "success"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"action": map[string]any{"id": 1, "status": status}})
	}))
	defer server.Close()

	c := &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client(), pollInterval: time.Millisecond, logger: testLogger}
	if err := c.pollAction(context.Background(), 1); err != nil {
		t.Fatalf("pollAction: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

func TestCleanup404IsNotError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			_ = json.NewEncoder(w).Encode(map[string]any{"zones": []map[string]any{{"id": "zone-1", "name": "example.test"}}})
		case r.Method == http.MethodPost && r.URL.Path == "/zones/zone-1/rrsets/_acme-challenge.test/TXT/actions/remove_records":
			http.Error(w, "missing", http.StatusNotFound)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	c := &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client(), logger: testLogger}
	if err := c.cleanupZone(context.Background(), zone{Name: "example.test"}, "_acme-challenge.test", "token-value"); err != nil {
		t.Fatalf("cleanupZone: %v", err)
	}
}

func TestPresentDuplicateTXTValueIsNotError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			_ = json.NewEncoder(w).Encode(map[string]any{"zones": []map[string]any{{"id": "zone-1", "name": "example.test"}}})
		case r.Method == http.MethodPost && r.URL.Path == "/zones/zone-1/rrsets/_acme-challenge.test/TXT/actions/add_records":
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "invalid_input", "message": "duplicate value '\"token-value\"'"}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	c := &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client(), logger: testLogger}
	if err := c.presentZone(context.Background(), zone{Name: "example.test"}, "_acme-challenge.test", "token-value"); err != nil {
		t.Fatalf("presentZone: %v", err)
	}
}

func TestPresentNonDuplicate422IsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			_ = json.NewEncoder(w).Encode(map[string]any{"zones": []map[string]any{{"id": "zone-1", "name": "example.test"}}})
		case r.Method == http.MethodPost && r.URL.Path == "/zones/zone-1/rrsets/_acme-challenge.test/TXT/actions/add_records":
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "invalid_input", "message": "some other validation error"}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	c := &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client(), logger: testLogger}
	if err := c.presentZone(context.Background(), zone{Name: "example.test"}, "_acme-challenge.test", "token-value"); err == nil {
		t.Fatal("expected error")
	}
}

func TestToASCII(t *testing.T) {
	got, err := toASCII("example.com")
	if err != nil || got != "example.com" {
		t.Fatalf("got %q err=%v", got, err)
	}
	got, err = toASCII("münchen.de")
	if err != nil || got != "xn--mnchen-3ya.de" {
		t.Fatalf("got %q err=%v", got, err)
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
	c := &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client(), logger: testLogger}
	if _, err := c.zoneID(context.Background(), "missing.example"); err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveZonePrefersConfigWithoutClientLookup(t *testing.T) {
	s := &Solver{logger: testLogger}
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
	s := &Solver{logger: testLogger}
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

	s := &Solver{client: &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client(), logger: testLogger}, logger: testLogger}
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

	s := &Solver{client: &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client(), logger: testLogger}, logger: testLogger}
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
			_ = json.NewEncoder(w).Encode(map[string]any{"zones": []map[string]any{{"id": 2, "name": "sub.example.com"}}})
		default:
			t.Fatalf("unexpected request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	serverURL = server.URL
	defer server.Close()

	c := &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client(), logger: testLogger}
	zones, err := c.listZones(context.Background())
	if err != nil {
		t.Fatalf("listZones: %v", err)
	}
	if len(zones) != 2 {
		t.Fatalf("expected 2 zones, got %d", len(zones))
	}
	if zones[0].ID != "zone-1" || zones[0].Name != "example.com" {
		t.Fatalf("unexpected first zone: %#v", zones[0])
	}
	if zones[1].ID != "2" || zones[1].Name != "sub.example.com" {
		t.Fatalf("unexpected second zone: %#v", zones[1])
	}
}

func TestZoneUnmarshalAcceptsNumericID(t *testing.T) {
	var z zone
	if err := json.Unmarshal([]byte(`{"id":123,"name":"example.com"}`), &z); err != nil {
		t.Fatalf("unmarshal zone: %v", err)
	}
	if z.ID != "123" || z.Name != "example.com" {
		t.Fatalf("unexpected zone: %#v", z)
	}
}

func TestPresentWithExplicitZoneResolvesIDBeforeMutation(t *testing.T) {
	requests := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			_ = json.NewEncoder(w).Encode(map[string]any{"zones": []map[string]any{{"id": "zone-1", "name": "example.test"}}})
		case r.Method == http.MethodPost && r.URL.Path == "/zones/zone-1/rrsets/_acme-challenge.test/TXT/actions/add_records":
			_ = json.NewEncoder(w).Encode(map[string]any{"action": map[string]any{"id": 1, "status": "success"}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	s := &Solver{client: &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client(), logger: testLogger}, logger: testLogger}
	ch := &acmev1.ChallengeRequest{ResolvedFQDN: "_acme-challenge.test.example.test.", Key: "token-value", Config: &apiextensionsv1.JSON{Raw: []byte(`{"zone":"example.test"}`)}}
	if err := s.Present(ch); err != nil {
		t.Fatalf("present: %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("expected 2 requests, got %d: %#v", len(requests), requests)
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

	c, err := NewDNSClient(testLogger)
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
		client:  &DNSClient{baseURL: server.URL, token: "x", httpClient: server.Client(), logger: testLogger},
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
