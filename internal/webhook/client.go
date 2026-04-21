// SPDX-FileCopyrightText: 2026 Matthias Wende
// SPDX-License-Identifier: GPL-3.0-only

package webhook

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultAPIBase     = "https://api.hetzner.cloud/v1"
	presentTTL         = 60
	maxAPIResponseSize = 1 << 20
	tracerName         = "github.com/mattwend/hetzner-acme-webhook"
)

var (
	tokenFilePath = "/var/run/secrets/hetzner-dns/token"
	tracer        = otel.Tracer(tracerName)
)

type DNSClient struct {
	baseURL      string
	httpClient   *http.Client
	token        string
	pollInterval time.Duration
	logger       *slog.Logger
}

type zonesResponse struct {
	Zones []zone `json:"zones"`
}

type zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type actionResponse struct {
	Action actionStatus `json:"action"`
}

type actionStatus struct {
	ID     int64      `json:"id"`
	Status string     `json:"status"`
	Error  *actionErr `json:"error"`
}

type actionErr struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type httpError struct {
	StatusCode int
	Body       string
}

func (e *httpError) Error() string { return fmt.Sprintf("http %d: %s", e.StatusCode, e.Body) }

func NewDNSClient(logger *slog.Logger) (*DNSClient, error) {
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
	if logger == nil {
		logger = slog.Default()
	}
	return &DNSClient{
		baseURL:      baseURL,
		token:        token,
		pollInterval: time.Second,
		logger:       logger,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			},
		},
	}, nil
}

func (c *DNSClient) ping(ctx context.Context, zoneName string) error {
	ctx, span := tracer.Start(ctx, "dns.ping", trace.WithAttributes(
		attribute.String("dns.zone", zoneName),
	))
	defer span.End()

	_, err := c.zoneID(ctx, zoneName)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
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
	ctx, span := tracer.Start(ctx, "dns.list_zones")
	defer span.End()

	path := "/zones"
	all := make([]zone, 0)
	for {
		var res zonesResponse
		resp, err := c.getJSONResponse(ctx, path, &res)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, stabilizeHTTPError(err)
		}
		all = append(all, res.Zones...)
		next := parseNextPage(resp.Header.Get("Link"))
		resp.Body.Close()
		if next == "" {
			span.SetAttributes(attribute.Int("dns.zone_count", len(all)))
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

func formatTXTRecord(value string) string {
	escaped := strings.ReplaceAll(value, `"`, `\"`)
	if escaped == "" {
		return `""`
	}

	chunks := make([]string, 0, (len(escaped)/255)+1)
	var current bytes.Buffer
	for _, r := range escaped {
		runeSize := utf8.RuneLen(r)
		if current.Len() > 0 && current.Len()+runeSize > 255 {
			chunks = append(chunks, `"`+current.String()+`"`)
			current.Reset()
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		chunks = append(chunks, `"`+current.String()+`"`)
	}
	return strings.Join(chunks, " ")
}

func (c *DNSClient) presentZone(ctx context.Context, z zone, recordName, key string) error {
	ctx, span := tracer.Start(ctx, "dns.present", trace.WithAttributes(
		attribute.String("dns.zone", z.Name),
		attribute.String("dns.record_name", recordName),
	))
	defer span.End()

	if err := validateRecordName(recordName); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	zoneID := z.ID
	if zoneID == "" {
		var err error
		zoneID, err = c.zoneID(ctx, z.Name)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	zoneName := strings.TrimSpace(z.Name)
	if zoneName == "" {
		zoneName = z.ID
	}
	formatted := formatTXTRecord(key)
	body := map[string]any{"records": []map[string]string{{"value": formatted}}, "ttl": presentTTL}

	c.logger.InfoContext(ctx, "presenting TXT record",
		slog.String("zone", zoneName),
		slog.String("record", recordName),
	)
	if err := c.postAction(ctx, "/zones/"+zoneID+"/rrsets/"+url.PathEscape(recordName)+"/TXT/actions/add_records", body); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

func (c *DNSClient) cleanupZone(ctx context.Context, z zone, recordName, key string) error {
	ctx, span := tracer.Start(ctx, "dns.cleanup", trace.WithAttributes(
		attribute.String("dns.zone", z.Name),
		attribute.String("dns.record_name", recordName),
	))
	defer span.End()

	if err := validateRecordName(recordName); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	zoneID := z.ID
	if zoneID == "" {
		var err error
		zoneID, err = c.zoneID(ctx, z.Name)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	zoneName := strings.TrimSpace(z.Name)
	if zoneName == "" {
		zoneName = z.ID
	}
	formatted := formatTXTRecord(key)
	body := map[string]any{"records": []map[string]string{{"value": formatted}}}

	c.logger.InfoContext(ctx, "cleaning up TXT record",
		slog.String("zone", zoneName),
		slog.String("record", recordName),
	)
	if err := c.postAction(ctx, "/zones/"+zoneID+"/rrsets/"+url.PathEscape(recordName)+"/TXT/actions/remove_records", body); err != nil {
		if IsNotFound(err) {
			c.logger.InfoContext(ctx, "record already deleted",
				slog.String("zone", zoneName),
				slog.String("record", recordName),
			)
			return nil
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
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

func formatActionError(action actionStatus) error {
	if action.Error == nil {
		return fmt.Errorf("action %d failed with status %q", action.ID, action.Status)
	}
	return fmt.Errorf("action %d failed: %s: %s", action.ID, action.Error.Code, action.Error.Message)
}

func (c *DNSClient) postAction(ctx context.Context, path string, body any) error {
	ctx, span := tracer.Start(ctx, "dns.post_action", trace.WithAttributes(
		attribute.String("dns.api_path", path),
	))
	defer span.End()

	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	var res actionResponse
	resp, err := c.do(ctx, http.MethodPost, path, bytes.NewReader(payload))
	if err != nil {
		err = stabilizeHTTPError(err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAPIResponseSize)).Decode(&res); err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)

	span.SetAttributes(attribute.Int64("dns.action_id", res.Action.ID))

	switch res.Action.Status {
	case "success":
		return nil
	case "error":
		err := formatActionError(res.Action)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	default:
		return c.pollAction(ctx, res.Action.ID)
	}
}

func (c *DNSClient) pollAction(ctx context.Context, actionID int64) error {
	ctx, span := tracer.Start(ctx, "dns.poll_action", trace.WithAttributes(
		attribute.Int64("dns.action_id", actionID),
	))
	defer span.End()

	pollInterval := c.pollInterval
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	for {
		var res actionResponse
		if err := c.getJSON(ctx, "/actions/"+strconv.FormatInt(actionID, 10), &res); err != nil {
			err = stabilizeHTTPError(err)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		switch res.Action.Status {
		case "success":
			return nil
		case "error":
			err := formatActionError(res.Action)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
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
