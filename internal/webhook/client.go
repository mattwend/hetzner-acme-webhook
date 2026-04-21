// SPDX-FileCopyrightText: 2026 Matthias Wende
// SPDX-License-Identifier: GPL-3.0-only

package webhook

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog/v2"
)

const (
	defaultAPIBase     = "https://api.hetzner.cloud/v1"
	presentTTL         = 60
	maxAPIResponseSize = 1 << 20
)

var tokenFilePath = "/var/run/secrets/hetzner-dns/token"

type DNSClient struct {
	baseURL    string
	httpClient *http.Client
	token      string
}

type zonesResponse struct {
	Zones []zone `json:"zones"`
}

type zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type rrsetsResponse struct {
	RRSets []rrset `json:"rrsets"`
}

type rrset struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	TTL     int      `json:"ttl,omitempty"`
	Records []string `json:"records,omitempty"`
}

type httpError struct {
	StatusCode int
	Body       string
}

func (e *httpError) Error() string { return fmt.Sprintf("http %d: %s", e.StatusCode, e.Body) }

func NewDNSClient() (*DNSClient, error) {
	var token string
	data, err := os.ReadFile(tokenFilePath)
	if err == nil {
		token = strings.TrimSpace(string(data))
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read token file: %w", err)
	}
	if token == "" {
		token = strings.TrimSpace(os.Getenv("HETZNER_DNS_API_TOKEN"))
	}
	if token == "" {
		return nil, errors.New("missing HETZNER_DNS_API_TOKEN and token file")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("HETZNER_DNS_API_BASE_URL")), "/")
	if baseURL == "" {
		baseURL = defaultAPIBase
	}
	return &DNSClient{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			},
		},
	}, nil
}

func (c *DNSClient) ping(ctx context.Context, zoneName string) error {
	_, err := c.zoneID(ctx, zoneName)
	return err
}

func normalizeDNSName(value string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
}

func parseNextPage(link string) string {
	if strings.TrimSpace(link) == "" {
		return ""
	}
	for _, part := range strings.Split(link, ",") {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		start := strings.Index(part, "<")
		end := strings.Index(part, ">")
		if start < 0 || end <= start+1 {
			return ""
		}
		u, err := url.Parse(part[start+1 : end])
		if err != nil {
			return ""
		}
		return u.RequestURI()
	}
	return ""
}

func (c *DNSClient) getJSONResponse(ctx context.Context, path string, out any) (*http.Response, error) {
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAPIResponseSize)).Decode(out); err != nil {
		resp.Body.Close()
		return nil, err
	}
	return resp, nil
}

func (c *DNSClient) listZones(ctx context.Context) ([]zone, error) {
	path := "/zones"
	all := make([]zone, 0)
	for {
		var res zonesResponse
		resp, err := c.getJSONResponse(ctx, path, &res)
		if err != nil {
			return nil, err
		}
		all = append(all, res.Zones...)
		next := parseNextPage(resp.Header.Get("Link"))
		resp.Body.Close()
		if next == "" {
			return all, nil
		}
		path = next
	}
}

func matchZoneByFQDN(fqdn string, zones []zone) (zone, error) {
	normalizedFQDN := normalizeDNSName(fqdn)
	if normalizedFQDN == "" {
		return zone{}, errors.New("empty fqdn")
	}

	candidates := append([]zone(nil), zones...)
	sort.Slice(candidates, func(i, j int) bool {
		return len(normalizeDNSName(candidates[i].Name)) > len(normalizeDNSName(candidates[j].Name))
	})
	for _, z := range candidates {
		normalizedZone := normalizeDNSName(z.Name)
		if normalizedZone == "" {
			continue
		}
		if normalizedFQDN == normalizedZone || strings.HasSuffix(normalizedFQDN, "."+normalizedZone) {
			return z, nil
		}
	}
	return zone{}, fmt.Errorf("no matching zone found for fqdn %q", strings.TrimSpace(fqdn))
}

func (c *DNSClient) detectZone(ctx context.Context, fqdn string) (zone, error) {
	zones, err := c.listZones(ctx)
	if err != nil {
		return zone{}, err
	}
	return matchZoneByFQDN(fqdn, zones)
}

func (c *DNSClient) zoneByName(ctx context.Context, zoneName string) (zone, error) {
	zones, err := c.listZones(ctx)
	if err != nil {
		return zone{}, err
	}
	normalizedZoneName := normalizeDNSName(zoneName)
	for _, z := range zones {
		if normalizeDNSName(z.Name) == normalizedZoneName {
			return z, nil
		}
	}
	return zone{}, fmt.Errorf("zone %s not found", zoneName)
}

func (c *DNSClient) zoneID(ctx context.Context, zoneName string) (string, error) {
	z, err := c.zoneByName(ctx, zoneName)
	if err != nil {
		return "", err
	}
	return z.ID, nil
}

func (c *DNSClient) listRRsets(ctx context.Context, zoneID string) ([]rrset, error) {
	var res rrsetsResponse
	if err := c.getJSON(ctx, "/zones/"+zoneID+"/rrsets", &res); err != nil {
		return nil, err
	}
	return res.RRSets, nil
}

func (c *DNSClient) presentZone(ctx context.Context, z zone, recordName, value string) error {
	if err := validateRecordName(recordName); err != nil {
		return err
	}
	zoneID := z.ID
	if zoneID == "" {
		var err error
		zoneID, err = c.zoneID(ctx, z.Name)
		if err != nil {
			return err
		}
	}
	zoneName := strings.TrimSpace(z.Name)
	if zoneName == "" {
		zoneName = z.ID
	}

	rrsets, err := c.listRRsets(ctx, zoneID)
	if err != nil {
		return err
	}
	values := []string{value}
	for _, rr := range rrsets {
		if rr.Name == recordName && rr.Type == "TXT" {
			values = mergeUnique(rr.Records, value)
			break
		}
	}
	body := map[string]any{"name": recordName, "type": "TXT", "ttl": presentTTL, "records": values}
	klog.Infof("present TXT record zone=%s name=%s values=%d", zoneName, recordName, len(values))
	return c.postJSON(ctx, "/zones/"+zoneID+"/rrsets", body)
}

func (c *DNSClient) present(ctx context.Context, zoneName, recordName, value string) error {
	z, err := c.zoneByName(ctx, zoneName)
	if err != nil {
		return err
	}
	return c.presentZone(ctx, z, recordName, value)
}

func (c *DNSClient) cleanupZone(ctx context.Context, z zone, recordName string) error {
	if err := validateRecordName(recordName); err != nil {
		return err
	}
	zoneID := z.ID
	if zoneID == "" {
		var err error
		zoneID, err = c.zoneID(ctx, z.Name)
		if err != nil {
			return err
		}
	}
	zoneName := strings.TrimSpace(z.Name)
	if zoneName == "" {
		zoneName = z.ID
	}
	klog.Infof("cleanup TXT record zone=%s name=%s", zoneName, recordName)
	return c.delete(ctx, "/zones/"+zoneID+"/rrsets/"+url.PathEscape(recordName)+"/TXT")
}

func (c *DNSClient) cleanup(ctx context.Context, zoneName, recordName string) error {
	z, err := c.zoneByName(ctx, zoneName)
	if err != nil {
		return err
	}
	return c.cleanupZone(ctx, z, recordName)
}

func (c *DNSClient) getJSON(ctx context.Context, path string, out any) error {
	resp, err := c.getJSONResponse(ctx, path, out)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *DNSClient) postJSON(ctx context.Context, path string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, http.MethodPost, path, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *DNSClient) delete(ctx context.Context, path string) error {
	resp, err := c.do(ctx, http.MethodDelete, path, nil)
	if err != nil {
		var httpErr *httpError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
			return nil
		}
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *DNSClient) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	requestURL := c.baseURL + path
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		requestURL = path
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		bodyText := string(data)
		if page := resp.Header.Get("X-Page"); page != "" {
			bodyText += " page=" + strconv.Quote(page)
		}
		return nil, &httpError{StatusCode: resp.StatusCode, Body: bodyText}
	}
	return resp, nil
}
