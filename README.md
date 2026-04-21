# hetzner-acme-webhook

Minimal [cert-manager](https://cert-manager.io/) DNS01 webhook for Hetzner DNS.

> **Note:** If you want the officially maintained option, see [`hetzner/cert-manager-webhook-hetzner`](https://github.com/hetzner/cert-manager-webhook-hetzner). This project is intentionally small and opinionated — see [how it differs](#how-this-differs-from-the-maintained-hetzner-webhook).

## Quick start

**Prerequisites:** cert-manager v1.0+ with cainjector enabled, installed in the `cert-manager` namespace.

**1. Add your API token** — edit the Secret in [`deploy/manifests.yaml`](deploy/manifests.yaml), replacing `REPLACE_WITH_HETZNER_DNS_API_TOKEN` with your [Hetzner DNS API token](https://docs.hetzner.com/cloud/api/getting-started/generating-api-token).

**2. Apply the manifest:**

```bash
kubectl apply -f deploy/manifests.yaml
```

**3. Create a ClusterIssuer** — pick an example, set your `email` (and `zone` if needed), then apply:

```bash
# Auto-detect zone from the challenge FQDN (simplest):
kubectl apply -f deploy/clusterissuer-example.yaml

# Or pin to a specific zone:
kubectl apply -f deploy/clusterissuer-example-explicit-zone.yaml
```

**4. Request a certificate** following the cert-manager [usage docs](https://cert-manager.io/docs/usage/).

The manifest handles Deployment, Service, TLS (self-signed CA), RBAC, and `APIService` registration — nothing else to configure.

## Configuration

### API token

The webhook reads the token from `/var/run/secrets/hetzner-dns/token` (mounted from a Secret in the install manifest). It falls back to the `HETZNER_DNS_API_TOKEN` environment variable when the file is absent or empty.

### Environment variables

| Variable | Default | Description |
|---|---|---|
| `HETZNER_DNS_API_TOKEN` | — | Fallback token (only if token file is absent) |
| `HETZNER_DNS_API_BASE_URL` | `https://api.hetzner.cloud/v1` | API base URL |
| `HETZNER_DNS_ZONE` | — | Default zone; also enables upstream-backed health checks |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | OTLP/gRPC endpoint for tracing |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | — | Traces-only OTLP/gRPC endpoint (overrides the above) |

### Per-issuer solver config

Set via the `config` field on the Issuer/ClusterIssuer:

```yaml
solvers:
  - dns01:
      webhook:
        groupName: acme.mattwend.github.io
        solverName: hetzner
        config:
          zone: example.com   # optional — overrides env/auto-detection
```

### Zone resolution order

1. **Per-issuer** — `config.zone` on the solver
2. **Cluster-wide** — `HETZNER_DNS_ZONE` environment variable
3. **Auto-detection** — longest suffix match against zones returned by `GET /zones`

Auto-detection example: `_acme-challenge.test.sub.example.com` matches `sub.example.com` before `example.com`.

## Webhook identity

| Property | Value |
|---|---|
| Group / Solver | `acme.mattwend.github.io` / `hetzner` |
| Image | `ghcr.io/mattwend/hetzner-acme-webhook` |
| Ports | HTTPS `:443` → `:8443`, health `:8080` (`/healthz`, `/readyz`) |
| Observability | Structured JSON (`slog`) + OpenTelemetry OTLP/gRPC (opt-in) |

## What the manifest includes

[`deploy/manifests.yaml`](deploy/manifests.yaml) creates:

- **Secret** with the Hetzner DNS API token
- **ServiceAccount + RBAC** (least-privilege)
- **Self-signed CA + TLS Certificate** via cert-manager; CA bundle injected by cainjector
- **Deployment** — single replica, restrictive security context (`readOnlyRootFilesystem`, `RuntimeDefault` seccomp, no privilege escalation)
- **Service + APIService** for Kubernetes API aggregation

**Optional tuning:** scale replicas / add affinity for HA, set `HETZNER_DNS_ZONE` for upstream health checks, adjust resource requests/limits.

### Customizing the ClusterIssuer examples

Before applying, update: `metadata.name`, `spec.acme.email`, `spec.acme.privateKeySecretRef.name`, and `config.zone` (explicit-zone variant only).

## Troubleshooting

**Certificate stuck pending:**

```bash
kubectl describe certificate <name>
kubectl describe order <name>
kubectl describe challenge <name>
kubectl logs -n cert-manager deploy/hetzner-acme-webhook
```

**Webhook pod not ready:**
Check that cert-manager and cainjector are running. If `HETZNER_DNS_ZONE` is set, an invalid zone or token will keep the pod unready.

**"no matching zone found":**
Ensure the API token has access to the zone containing the domain, and that the zone exists in the Hetzner DNS console.

## How this differs from the maintained Hetzner webhook

This project exists because I wanted a webhook that is easy to read, audit, and adapt for a small single-tenant setup.

| | This webhook | [`hetzner/cert-manager-webhook-hetzner`](https://github.com/hetzner/cert-manager-webhook-hetzner) |
|---|---|---|
| **Audience** | Single-tenant, self-hosted | General purpose, officially maintained |
| **Zone detection** | Auto-detect, env var, or per-issuer | Relies on cert-manager's `ResolvedZone` |
| **Token management** | Single token (file or env) | Per-issuer via Secret references |
| **API client** | Raw HTTP — no SDK | `hcloud-go` SDK |
| **Observability** | `slog` + OTLP tracing | `slog` + Prometheus metrics |
| **Packaging** | Static `kubectl apply` manifest | Helm chart |
| **Codebase** | ~800 lines of Go | ~450 lines + SDK |

**Choose the official webhook** for the safer default, broader upstream support, or multi-tenant needs. **Choose this one** for a smaller, self-contained codebase with automatic zone detection and OTLP tracing.

## Development

```bash
go test ./...
go vet ./...
podman build -t ghcr.io/mattwend/hetzner-acme-webhook:latest .
```

### E2E conformance tests

Run the cert-manager DNS01 conformance suite against a real Hetzner zone:

```bash
export HETZNER_DNS_API_TOKEN="your-token"
export TEST_ZONE_NAME="example.com."   # trailing dot required
go test -v -count=1 -tags=e2e ./...
```

## Links

- [SECURITY.md](SECURITY.md)
- [CHANGELOG.md](CHANGELOG.md)
- [LICENSE](LICENSE) (GPL-3.0-only)
