# Changelog

## [Unreleased]

## [1.1.0] - 2026-04-21

### Added
- automatic Hetzner DNS zone detection from the challenge FQDN, including longest-suffix matching for overlapping zones such as `sub.example.com` and `example.com`
- structured JSON logging via `log/slog`
- optional OpenTelemetry tracing export via OTLP/gRPC
- cert-manager DNS01 provider conformance e2e test suite, plus a local `scripts/run-conformance.sh` helper
- an additional `ClusterIssuer` example for deployments that prefer an explicit `zone` in solver configuration

### Changed
- DNS TXT record presentation and cleanup now use Hetzner DNS action endpoints
- default deployment examples and manifests now assume zone auto-detection, with a separate explicit-zone example for pinned-zone setups
- README quick start and deployment documentation were reorganized and expanded
- CI and container publishing workflows were streamlined
- the build now targets Go 1.26

### Security
- documentation now calls out the broader token blast radius when auto-detection is enabled

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
