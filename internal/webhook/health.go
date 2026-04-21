// SPDX-FileCopyrightText: 2026 Matthias Wende
// SPDX-License-Identifier: GPL-3.0-only

package webhook

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	GroupName        = "acme.mattwend.github.io"
	healthListenAddr = ":8080"
	healthCacheTTL   = 10 * time.Second
)

type healthState struct {
	client  *DNSClient
	zone    string
	enabled bool
	logger  *slog.Logger

	mu           sync.Mutex
	checking     bool
	checkedAt    time.Time
	lastErr      error
	lastCheckDur time.Duration
}

func ServeHealth(logger *slog.Logger, client *DNSClient, zone string) {
	state := &healthState{
		client:  client,
		zone:    zone,
		enabled: strings.TrimSpace(zone) != "",
		logger:  logger,
	}
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
	logger.Info("starting health server", slog.String("addr", healthListenAddr))
	if err := http.ListenAndServe(healthListenAddr, mux); err != nil {
		logger.Error("health server failed", slog.String("error", err.Error()))
		panic(fmt.Sprintf("health server: %v", err))
	}
}

func (s *healthState) check(parent context.Context) error {
	if !s.enabled {
		return nil
	}

	for {
		now := time.Now()
		s.mu.Lock()
		if !s.checkedAt.IsZero() && now.Sub(s.checkedAt) < healthCacheTTL {
			err := s.lastErr
			s.mu.Unlock()
			return err
		}
		if !s.checking {
			s.checking = true
			s.mu.Unlock()
			break
		}
		s.mu.Unlock()
		select {
		case <-parent.Done():
			return parent.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}

	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	start := time.Now()
	err := s.client.ping(ctx, s.zone)
	dur := time.Since(start)

	s.mu.Lock()
	s.checking = false
	s.checkedAt = time.Now()
	s.lastErr = err
	s.lastCheckDur = dur
	s.mu.Unlock()

	if err != nil {
		s.logger.Warn("health check failed",
			slog.String("zone", s.zone),
			slog.Duration("duration", dur),
			slog.String("error", err.Error()),
		)
	}
	return err
}
