// SPDX-FileCopyrightText: 2026 Matthias Wende
// SPDX-License-Identifier: GPL-3.0-only

package webhook

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

func fqdnToRelative(fqdn, zone string) (string, error) {
	fqdn = normalizeDNSName(fqdn)
	zone = normalizeDNSName(zone)
	if fqdn == "" {
		return "", errors.New("empty fqdn")
	}
	if zone == "" {
		return "", errors.New("empty zone")
	}
	suffix := "." + zone
	if fqdn != zone && !strings.HasSuffix(fqdn, suffix) {
		return "", fmt.Errorf("fqdn %q is outside zone %q", fqdn, zone)
	}
	name := strings.TrimSuffix(fqdn, suffix)
	name = strings.TrimSuffix(name, ".")
	if name == "" {
		return "", fmt.Errorf("fqdn %q resolves to zone apex, expected challenge record", fqdn)
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

func mergeUnique(existing []string, value string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(existing)+1)
	for _, item := range append(existing, value) {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}
