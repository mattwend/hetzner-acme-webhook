// SPDX-FileCopyrightText: 2026 Matthias Wende
// SPDX-License-Identifier: GPL-3.0-only

package webhook

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

type solverConfig struct {
	Zone string `json:"zone,omitempty"`
}

func ZoneFromEnv() (string, error) {
	zone := strings.TrimSpace(os.Getenv("HETZNER_DNS_ZONE"))
	if zone == "" {
		return "", errors.New("missing HETZNER_DNS_ZONE")
	}
	return zone, nil
}

func decodeSolverConfig(raw []byte) (solverConfig, error) {
	cfg := solverConfig{}
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return solverConfig{}, fmt.Errorf("decode config: %w", err)
	}
	return cfg, nil
}

func explicitZoneFromConfig(raw []byte) (string, error) {
	cfg, err := decodeSolverConfig(raw)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(cfg.Zone), nil
}

