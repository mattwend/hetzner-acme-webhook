# hetzner-acme-webhook

Minimal [cert-manager](https://cert-manager.io/) DNS01 webhook for Hetzner DNS.

> If you came here by accident and just want a maintained Hetzner webhook for cert-manager, you probably want [`hetzner/cert-manager-webhook-hetzner`](https://github.com/hetzner/cert-manager-webhook-hetzner) instead. This repository is intentionally small and opinionated. If you want to understand why it exists and how it differs, see [How this differs from the maintained Hetzner webhook](#how-this-differs-from-the-maintained-hetzner-webhook).

## Webhook identity

- `groupName`: `acme.mattwend.github.io`
- `solverName`: `hetzner`
- image: `ghcr.io/mattwend/hetzner-acme-webhook`
- webhook HTTPS: service `:443` -> container `:8443`
- health endpoints: `:8080/healthz` and `:8080/readyz`

## Configuration

Environment variables:

- `HETZNER_DNS_API_TOKEN` — optional if token file exists; token file is preferred when both are set
- `HETZNER_DNS_API_BASE_URL` — optional, defaults to `https://api.hetzner.cloud/v1`
- `HETZNER_DNS_ZONE` — optional explicit default Hetzner DNS zone, for example `example.com`; used when solver config omits `zone`, and also enables upstream-backed health checks. If this env var is unset and solver config omits `zone`, the webhook auto-detects the matching zone from the challenge FQDN by querying Hetzner DNS. The install manifest leaves this unset by default.

Webhook prefers reading the token from `/var/run/secrets/hetzner-dns/token` and falls back to `HETZNER_DNS_API_TOKEN` when the file is absent or empty.

The webhook needs a DNS zone because Hetzner's API is zone-based. For example, to solve challenges for `app.example.com`, use the zone `example.com` if that is the zone hosted in Hetzner DNS.

Solver config:

```json
{
  "zone": "example.com"
}
```

`zone` is an optional explicit Hetzner DNS zone override for where the ACME TXT record should be created.

Zone resolution order during challenge handling:
1. `config.zone`
2. `HETZNER_DNS_ZONE`
3. automatic detection from the challenge FQDN via `GET /zones`

Auto-detection matches the challenge FQDN against accessible Hetzner zone names by longest suffix. For example, `_acme-challenge.test.sub.example.com` matches `sub.example.com` before `example.com`.

If no accessible zone matches, challenge handling fails, but the process still starts and exposes local health endpoints.

## Deploy

This repository provides:
- [`deploy/manifests.yaml`](deploy/manifests.yaml) — complete install manifest for the webhook
- [`deploy/clusterissuer-example.yaml`](deploy/clusterissuer-example.yaml) — example `ClusterIssuer` using automatic zone detection
- [`deploy/clusterissuer-example-explicit-zone.yaml`](deploy/clusterissuer-example-explicit-zone.yaml) — example `ClusterIssuer` with an explicit zone override

The install manifest sets up the deployment, service, TLS, RBAC, and `APIService` registration required by cert-manager. It also applies a restrictive pod security context with `RuntimeDefault` seccomp.

Prerequisites:
- cert-manager `v1.0+` is already installed in the `cert-manager` namespace
- cert-manager's controller ServiceAccount is named `cert-manager`
- cert-manager cainjector is enabled so the `APIService` CA bundle is injected
- the published image tag `ghcr.io/mattwend/hetzner-acme-webhook:v1.0.0` exists

Before applying the install manifest, replace the placeholder token. The install manifest intentionally does not set `HETZNER_DNS_ZONE`; you can either keep `zone` in your solver config, set a cluster-wide default zone in the Deployment for upstream-backed health checks, or rely on automatic zone detection during challenge handling.

```bash
kubectl apply -f deploy/manifests.yaml
```

If you want to use one of the example issuers, customize it first:
- [`deploy/clusterissuer-example.yaml`](deploy/clusterissuer-example.yaml) uses automatic zone detection with `config: {}`
- [`deploy/clusterissuer-example-explicit-zone.yaml`](deploy/clusterissuer-example-explicit-zone.yaml) uses `config.zone` to lock the solver to a specific zone

Update at least:
- `metadata.name`
- `spec.acme.email`
- `spec.acme.privateKeySecretRef.name`
- `spec.acme.solvers[]...config.zone` in the explicit-zone variant

For higher availability, you can also scale the Deployment above one replica and add your preferred affinity/topology settings.

Then apply the variant you want:

```bash
kubectl apply -f deploy/clusterissuer-example.yaml
# or
kubectl apply -f deploy/clusterissuer-example-explicit-zone.yaml
```

## How this differs from the maintained Hetzner webhook

This project exists because I wanted a webhook that is easy to read, easy to audit, and easy to adapt for a small single-tenant setup.

In short:

- this repository favors a **small codebase** over a broad feature set
- it is optimized for **simple self-hosted deployments** rather than being the default recommendation for everyone
- it includes **automatic zone detection** from the challenge FQDN when no explicit zone is configured
- it intentionally keeps dependencies and moving parts limited where practical

Compared with [`hetzner/cert-manager-webhook-hetzner`](https://github.com/hetzner/cert-manager-webhook-hetzner):

- the maintained Hetzner webhook is the better default choice if you want the **officially maintained** implementation
- this webhook is more opinionated and currently targets a narrower operational model
- this webhook prioritizes **minimalism and local readability** over feature breadth and optimization work such as extra caching layers
- this webhook may diverge in behavior and implementation details when that keeps the design simpler for this repository's use case

When to choose which:

- choose the maintained Hetzner webhook if you want the safer default, broader upstream attention, or you expect higher scale / more demanding operational requirements
- choose this repository if you specifically want this project's behavior, deployment shape, or a smaller codebase that is easy to inspect and modify

This project is not trying to replace the maintained Hetzner webhook. It is a focused alternative.

## Development

For local verification:

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
