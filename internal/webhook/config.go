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

func zoneFromConfig(raw []byte) (string, error) {
	cfg := solverConfig{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return "", fmt.Errorf("decode config: %w", err)
		}
	}
	zoneName := strings.TrimSpace(cfg.Zone)
	if zoneName != "" {
		return zoneName, nil
	}
	zoneName, err := ZoneFromEnv()
	if err != nil {
		return "", errors.New("missing zone in solver config and HETZNER_DNS_ZONE")
	}
	return zoneName, nil
}
