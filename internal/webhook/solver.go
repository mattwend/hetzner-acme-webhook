// SPDX-FileCopyrightText: 2026 Matthias Wende
// SPDX-License-Identifier: GPL-3.0-only

package webhook

import (
	"context"
	"log/slog"
	"time"

	acmev1 "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/client-go/rest"
)

const solverName = "hetzner"

type Solver struct {
	client *DNSClient
	logger *slog.Logger
}

func NewSolver(logger *slog.Logger, client *DNSClient) *Solver {
	if logger == nil {
		logger = slog.Default()
	}
	return &Solver{client: client, logger: logger}
}

func (s *Solver) Name() string { return solverName }

func (s *Solver) resolveZone(ctx context.Context, ch *acmev1.ChallengeRequest) (zone, error) {
	var raw []byte
	if ch.Config != nil {
		raw = ch.Config.Raw
	}
	zoneName, err := explicitZoneFromConfig(raw)
	if err != nil {
		return zone{}, err
	}
	if zoneName != "" {
		return zone{Name: zoneName}, nil
	}
	zoneName, err = ZoneFromEnv()
	if err == nil {
		return zone{Name: zoneName}, nil
	}
	return s.client.detectZone(ctx, ch.ResolvedFQDN)
}

func (s *Solver) Present(ch *acmev1.ChallengeRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ctx, span := tracer.Start(ctx, "solver.present", trace.WithAttributes(
		attribute.String("dns.name", ch.DNSName),
		attribute.String("dns.resolved_fqdn", ch.ResolvedFQDN),
	))
	defer span.End()

	logger := s.logger.With(
		slog.String("dns_name", ch.DNSName),
		slog.String("resolved_fqdn", ch.ResolvedFQDN),
	)

	z, err := s.resolveZone(ctx, ch)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		logger.ErrorContext(ctx, "zone resolution failed", slog.String("error", err.Error()))
		return err
	}
	if z.ID == "" {
		z, err = s.client.zoneByName(ctx, z.Name)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	span.SetAttributes(attribute.String("dns.zone", z.Name))

	recordName, err := fqdnToRelative(ch.ResolvedFQDN, z.Name)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	logger.InfoContext(ctx, "presenting challenge",
		slog.String("zone", z.Name),
		slog.String("record", recordName),
	)
	return s.client.presentZone(ctx, z, recordName, ch.Key)
}

func (s *Solver) CleanUp(ch *acmev1.ChallengeRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ctx, span := tracer.Start(ctx, "solver.cleanup", trace.WithAttributes(
		attribute.String("dns.name", ch.DNSName),
		attribute.String("dns.resolved_fqdn", ch.ResolvedFQDN),
	))
	defer span.End()

	logger := s.logger.With(
		slog.String("dns_name", ch.DNSName),
		slog.String("resolved_fqdn", ch.ResolvedFQDN),
	)

	z, err := s.resolveZone(ctx, ch)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		logger.ErrorContext(ctx, "zone resolution failed", slog.String("error", err.Error()))
		return err
	}
	if z.ID == "" {
		z, err = s.client.zoneByName(ctx, z.Name)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	span.SetAttributes(attribute.String("dns.zone", z.Name))

	recordName, err := fqdnToRelative(ch.ResolvedFQDN, z.Name)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	logger.InfoContext(ctx, "cleaning up challenge",
		slog.String("zone", z.Name),
		slog.String("record", recordName),
	)
	return s.client.cleanupZone(ctx, z, recordName, ch.Key)
}

func (s *Solver) Initialize(*rest.Config, <-chan struct{}) error { return nil }
