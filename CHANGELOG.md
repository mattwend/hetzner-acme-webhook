# Changelog

## [1.0.0] - 2026-04-20

Initial stable release.

### Added
- cert-manager DNS01 webhook solver for Hetzner DNS
- token loading from mounted file or environment variable, preferring `/var/run/secrets/hetzner-dns/token`
- health and readiness endpoints with short-lived cached checks
- distroless non-root container image
- CI and container publishing workflows
- complete install manifest and example `ClusterIssuer` for deployment
- security policy and project license

### Changed
- webhook startup no longer requires `HETZNER_DNS_ZONE`; challenge handling still requires a zone from solver config or environment
- upstream-backed health checks are serialized to avoid overlapping checks

### Notes
- Configure `groupName: acme.mattwend.github.io`
- Configure `solverName: hetzner`
- Prefer mounting the Hetzner API token at `/var/run/secrets/hetzner-dns/token`
