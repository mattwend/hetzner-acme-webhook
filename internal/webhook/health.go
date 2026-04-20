// SPDX-FileCopyrightText: 2026 Matthias Wende
// SPDX-License-Identifier: GPL-3.0-only

package webhook

import (
	"context"
	"net/http"
	"sync"
	"time"

	"k8s.io/klog/v2"
)

const (
	GroupName        = "acme.example.com"
	healthListenAddr = ":8080"
	healthCacheTTL   = 10 * time.Second
)

type healthState struct {
	client *DNSClient
	zone   string

	mu           sync.Mutex
	checkedAt    time.Time
	lastErr      error
	lastCheckDur time.Duration
}

func ServeHealth(client *DNSClient, zone string) {
	state := &healthState{client: client, zone: zone}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := state.check(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := state.check(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	if err := http.ListenAndServe(healthListenAddr, mux); err != nil {
		klog.Fatalf("health server: %v", err)
	}
}

func (s *healthState) check(parent context.Context) error {
	now := time.Now()
	s.mu.Lock()
	if !s.checkedAt.IsZero() && now.Sub(s.checkedAt) < healthCacheTTL {
		err := s.lastErr
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	start := time.Now()
	err := s.client.ping(ctx, s.zone)
	dur := time.Since(start)

	s.mu.Lock()
	s.checkedAt = time.Now()
	s.lastErr = err
	s.lastCheckDur = dur
	s.mu.Unlock()

	if err != nil {
		klog.Warningf("health check failed zone=%s duration=%s: %v", s.zone, dur, err)
	}
	return err
}
