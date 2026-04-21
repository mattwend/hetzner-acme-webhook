# hetzner-acme-webhook

Minimal [cert-manager](https://cert-manager.io/) DNS01 webhook for Hetzner DNS.

> If you just want a maintained Hetzner webhook for cert-manager, you probably want [`hetzner/cert-manager-webhook-hetzner`](https://github.com/hetzner/cert-manager-webhook-hetzner) instead. This repository is intentionally small and opinionated — see [How this differs](#how-this-differs-from-the-maintained-hetzner-webhook) for details.

## Quick start

Prerequisites: cert-manager `v1.0+` with cainjector enabled, installed in the `cert-manager` namespace.

1. **Edit the Secret** in [`deploy/manifests.yaml`](deploy/manifests.yaml) — replace `REPLACE_WITH_HETZNER_DNS_API_TOKEN` with your [Hetzner DNS API token](https://docs.hetzner.com/cloud/api/getting-started/generating-api-token).

2. **Apply the install manifest:**

   ```bash
   kubectl apply -f deploy/manifests.yaml
   ```

3. **Create a ClusterIssuer.** Pick one of the examples, customize `email` and (if applicable) `zone`, and apply:

   ```bash
   # Auto-detect zone from challenge FQDN (simplest):
   kubectl apply -f deploy/clusterissuer-example.yaml

   # Or lock to a specific zone:
   kubectl apply -f deploy/clusterissuer-example-explicit-zone.yaml
   ```

4. **Request a certificate** as described in the cert-manager [usage docs](https://cert-manager.io/docs/usage/).

That's it. The manifest sets up the Deployment, Service, TLS (via self-signed CA), RBAC, and `APIService` registration.

## Configuration

### Token

The webhook reads the Hetzner DNS API token from the file `/var/run/secrets/hetzner-dns/token` (mounted from a Kubernetes Secret in the install manifest). It falls back to the `HETZNER_DNS_API_TOKEN` environment variable when the file is absent or empty.

### Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `HETZNER_DNS_API_TOKEN` | only if token file is absent | — | Hetzner DNS API token (token file is preferred) |
| `HETZNER_DNS_API_BASE_URL` | no | `https://api.hetzner.cloud/v1` | API base URL |
| `HETZNER_DNS_ZONE` | no | — | Default zone (e.g. `example.com`); enables upstream-backed health checks |

### Solver config

Passed via the `config` field on the Issuer/ClusterIssuer webhook solver:

```yaml
solvers:
  - dns01:
      webhook:
        groupName: acme.mattwend.github.io
        solverName: hetzner
        config:
          zone: example.com   # optional
```

| Field | Required | Description |
|---|---|---|
| `zone` | no | Explicit Hetzner DNS zone for this issuer |

### Zone resolution order

When handling a challenge, the webhook resolves the target zone in this order:

1. `config.zone` (per-issuer override)
2. `HETZNER_DNS_ZONE` (cluster-wide default)
3. Automatic detection from the challenge FQDN via `GET /zones` (longest suffix match)

Auto-detection matches the challenge FQDN against accessible Hetzner zone names. For example, `_acme-challenge.test.sub.example.com` matches `sub.example.com` before `example.com`.

## Webhook identity

| Property | Value |
|---|---|
| `groupName` | `acme.mattwend.github.io` |
| `solverName` | `hetzner` |
| Image | `ghcr.io/mattwend/hetzner-acme-webhook` |
| Webhook HTTPS | Service `:443` → container `:8443` |
| Health endpoints | `:8080/healthz` and `:8080/readyz` |

## Deploy details

The install manifest in [`deploy/manifests.yaml`](deploy/manifests.yaml) includes:

- **Namespace** — uses `cert-manager`
- **Secret** — holds the Hetzner DNS API token
- **ServiceAccount + RBAC** — least-privilege access for the webhook and cert-manager
- **Self-signed CA + TLS Certificate** — issued by cert-manager itself; CA bundle injected into the `APIService` via cainjector
- **Deployment** — single replica with a restrictive security context (`readOnlyRootFilesystem`, `RuntimeDefault` seccomp, no privilege escalation)
- **Service + APIService** — registers the webhook with the Kubernetes API aggregation layer

Optional tuning:
- **Scale replicas** and add affinity/topology constraints for higher availability
- **Set `HETZNER_DNS_ZONE`** on the Deployment to enable upstream-backed health checks
- **Adjust resource requests/limits** if your workload differs

### Customizing the ClusterIssuer examples

Before applying an example, update at least:
- `metadata.name`
- `spec.acme.email`
- `spec.acme.privateKeySecretRef.name`
- `spec.acme.solvers[]...config.zone` (explicit-zone variant only)

## Troubleshooting

**Certificate stuck in pending state:**
```bash
kubectl describe certificate <name>
kubectl describe order <name>
kubectl describe challenge <name>
kubectl logs -n cert-manager deploy/hetzner-acme-webhook
```

**Webhook pod not ready:**
- Check that cert-manager and cainjector are running — the webhook TLS certificate is issued by cert-manager itself
- If `HETZNER_DNS_ZONE` is set, the health check verifies the zone is reachable; an invalid zone or token will keep the pod unready

**"no matching zone found" error:**
- The API token must have access to the zone that contains the domain being validated
- Check that the zone exists in your Hetzner DNS console and matches the certificate's domain

## How this differs from the maintained Hetzner webhook

This project exists because I wanted a webhook that is easy to read, easy to audit, and easy to adapt for a small single-tenant setup.

| | This webhook | [`hetzner/cert-manager-webhook-hetzner`](https://github.com/hetzner/cert-manager-webhook-hetzner) |
|---|---|---|
| **Target audience** | Single-tenant, self-hosted | General purpose, officially maintained |
| **Zone detection** | Auto-detect, env var, or per-issuer config | Relies on cert-manager's `ResolvedZone` |
| **Token management** | Single token via file/env | Per-issuer tokens via K8s Secret references |
| **API client** | Raw HTTP (no SDK dependency) | `hcloud-go` SDK with Prometheus metrics |
| **Packaging** | Static `kubectl apply` manifest | Helm chart |
| **Codebase** | ~700 lines of Go | Larger, with more abstractions |

**When to choose which:**
- Choose the maintained Hetzner webhook for the safer default, broader upstream attention, or multi-tenant needs
- Choose this webhook if you want a smaller codebase that is easy to inspect and modify, or you specifically want automatic zone detection

This project is not trying to replace the maintained Hetzner webhook. It is a focused alternative.

## Development

```bash
go test ./...
go vet ./...
```

To build a local container image:

```bash
podman build -t ghcr.io/mattwend/hetzner-acme-webhook:latest .
```

## Security

See [SECURITY.md](SECURITY.md).

## Changelog

See [CHANGELOG.md](CHANGELOG.md).

## License

GPL-3.0-only. See [LICENSE](LICENSE).
