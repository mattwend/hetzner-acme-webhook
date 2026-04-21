// SPDX-FileCopyrightText: 2026 Matthias Wende
// SPDX-License-Identifier: GPL-3.0-only

package webhook

import (
	"context"
	"time"

	acmev1 "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"k8s.io/client-go/rest"
)

const solverName = "hetzner"

type Solver struct {
	client *DNSClient
}

func NewSolver(client *DNSClient) *Solver {
	return &Solver{client: client}
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

	z, err := s.resolveZone(ctx, ch)
	if err != nil {
		return err
	}
	if z.ID == "" {
		z, err = s.client.zoneByName(ctx, z.Name)
		if err != nil {
			return err
		}
	}
	recordName, err := fqdnToRelative(ch.ResolvedFQDN, z.Name)
	if err != nil {
		return err
	}
	return s.client.presentZone(ctx, z, recordName, ch.Key)
}

func (s *Solver) CleanUp(ch *acmev1.ChallengeRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	z, err := s.resolveZone(ctx, ch)
	if err != nil {
		return err
	}
	if z.ID == "" {
		z, err = s.client.zoneByName(ctx, z.Name)
		if err != nil {
			return err
		}
	}
	recordName, err := fqdnToRelative(ch.ResolvedFQDN, z.Name)
	if err != nil {
		return err
	}
	return s.client.cleanupZone(ctx, z, recordName, ch.Key)
}

func (s *Solver) Initialize(*rest.Config, <-chan struct{}) error { return nil }
