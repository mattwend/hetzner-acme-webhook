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

func (s *Solver) Present(ch *acmev1.ChallengeRequest) error {
	zoneName, err := zoneFromConfig(ch.Config.Raw)
	if err != nil {
		return err
	}
	recordName, err := fqdnToRelative(ch.ResolvedFQDN, zoneName)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.client.present(ctx, zoneName, recordName, ch.Key)
}

func (s *Solver) CleanUp(ch *acmev1.ChallengeRequest) error {
	zoneName, err := zoneFromConfig(ch.Config.Raw)
	if err != nil {
		return err
	}
	recordName, err := fqdnToRelative(ch.ResolvedFQDN, zoneName)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.client.cleanup(ctx, zoneName, recordName)
}

func (s *Solver) Initialize(*rest.Config, <-chan struct{}) error { return nil }
