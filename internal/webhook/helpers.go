// SPDX-FileCopyrightText: 2026 Matthias Wende
// SPDX-License-Identifier: GPL-3.0-only

package webhook

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/net/idna"
)

func toASCII(value string) (string, error) {
	return idna.Lookup.ToASCII(value)
}

func fqdnToRelative(fqdn, zone string) (string, error) {
	fqdn = normalizeDNSName(fqdn)
	zone = normalizeDNSName(zone)
	if fqdn == "" {
		return "", errors.New("empty fqdn")
	}
	if zone == "" {
		return "", errors.New("empty zone")
	}
	labels := strings.Split(fqdn, ".")
	for i, label := range labels {
		if label == "" {
			continue
		}
		if strings.HasPrefix(label, "_") {
			rest, err := toASCII(strings.TrimPrefix(label, "_"))
			if err != nil {
				return "", err
			}
			labels[i] = "_" + rest
			continue
		}
		asciiLabel, err := toASCII(label)
		if err != nil {
			return "", err
		}
		labels[i] = asciiLabel
	}
	asciiFQDN := strings.Join(labels, ".")
	asciiZone, err := toASCII(zone)
	if err != nil {
		return "", err
	}
	suffix := "." + asciiZone
	if asciiFQDN != asciiZone && !strings.HasSuffix(asciiFQDN, suffix) {
		return "", fmt.Errorf("fqdn %q is outside zone %q", asciiFQDN, asciiZone)
	}
	name := strings.TrimSuffix(asciiFQDN, suffix)
	name = strings.TrimSuffix(name, ".")
	if name == "" {
		return "", fmt.Errorf("fqdn %q resolves to zone apex, expected challenge record", asciiFQDN)
	}
	if err := validateRecordName(name); err != nil {
		return "", err
	}
	return name, nil
}

func validateRecordName(name string) error {
	if name == "" {
		return errors.New("empty record name")
	}
	if strings.ContainsAny(name, `/\\`) || strings.Contains(name, "..") {
		return fmt.Errorf("invalid record name %q", name)
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			continue
		}
		return fmt.Errorf("invalid record name %q", name)
	}
	return nil
}
