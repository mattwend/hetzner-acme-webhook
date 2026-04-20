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

func (c *DNSClient) zoneID(ctx context.Context, zoneName string) (string, error) {
	var res zonesResponse
	if err := c.getJSON(ctx, "/zones", &res); err != nil {
		return "", err
	}
	for _, z := range res.Zones {
		if strings.EqualFold(strings.TrimSuffix(z.Name, "."), strings.TrimSuffix(zoneName, ".")) {
			return z.ID, nil
		}
	}
	return "", fmt.Errorf("zone %s not found", zoneName)
}

func (c *DNSClient) listRRsets(ctx context.Context, zoneID string) ([]rrset, error) {
	var res rrsetsResponse
	if err := c.getJSON(ctx, "/zones/"+zoneID+"/rrsets", &res); err != nil {
		return nil, err
	}
	return res.RRSets, nil
}

func (c *DNSClient) present(ctx context.Context, zoneName, recordName, value string) error {
	if err := validateRecordName(recordName); err != nil {
		return err
	}
	zoneID, err := c.zoneID(ctx, zoneName)
	if err != nil {
		return err
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

func (c *DNSClient) cleanup(ctx context.Context, zoneName, recordName string) error {
	if err := validateRecordName(recordName); err != nil {
		return err
	}
	zoneID, err := c.zoneID(ctx, zoneName)
	if err != nil {
		return err
	}
	klog.Infof("cleanup TXT record zone=%s name=%s", zoneName, recordName)
	return c.delete(ctx, "/zones/"+zoneID+"/rrsets/"+url.PathEscape(recordName)+"/TXT")
}

func (c *DNSClient) getJSON(ctx context.Context, path string, out any) error {
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(io.LimitReader(resp.Body, maxAPIResponseSize)).Decode(out)
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
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
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
		return nil, &httpError{StatusCode: resp.StatusCode, Body: string(data)}
	}
	return resp, nil
}
