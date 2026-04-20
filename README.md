# hetzner-acme-webhook

Minimal [cert-manager](https://cert-manager.io/) DNS01 webhook for Hetzner DNS.

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
- `HETZNER_DNS_ZONE` — optional default Hetzner DNS zone, for example `example.com`; used when solver config omits `zone`, and also enables upstream-backed health checks. The install manifest leaves this unset by default so you must set `zone` in solver config unless you add the env var yourself.

Webhook prefers reading the token from `/var/run/secrets/hetzner-dns/token` and falls back to `HETZNER_DNS_API_TOKEN` when the file is absent or empty.

The webhook needs a DNS zone because Hetzner's API is zone-based. For example, to solve challenges for `app.example.com`, use the zone `example.com` if that is the zone hosted in Hetzner DNS.

Solver config:

```json
{
  "zone": "example.com"
}
```

`zone` is the Hetzner DNS zone where the ACME TXT record should be created.

If `zone` is omitted, the webhook falls back to `HETZNER_DNS_ZONE`.

If neither is set, challenge handling fails because the webhook cannot determine which Hetzner zone to update, but the process still starts and exposes local health endpoints.

## Deploy

This repository provides:
- [`deploy/manifests.yaml`](deploy/manifests.yaml) — complete install manifest for the webhook
- [`deploy/clusterissuer-example.yaml`](deploy/clusterissuer-example.yaml) — example `ClusterIssuer` to customize

The install manifest sets up the deployment, service, TLS, RBAC, and `APIService` registration required by cert-manager. It also applies a restrictive pod security context with `RuntimeDefault` seccomp.

Prerequisites:
- cert-manager `v1.0+` is already installed in the `cert-manager` namespace
- cert-manager's controller ServiceAccount is named `cert-manager`
- cert-manager cainjector is enabled so the `APIService` CA bundle is injected
- the published image tag `ghcr.io/mattwend/hetzner-acme-webhook:v1.0.0` exists

Before applying the install manifest, replace the placeholder token. The install manifest intentionally does not set `HETZNER_DNS_ZONE`; keep `zone` in your solver config unless you want to add a cluster-wide default zone to the Deployment for upstream-backed health checks.

```bash
kubectl apply -f deploy/manifests.yaml
```

If you want to use the example issuer, customize [`deploy/clusterissuer-example.yaml`](deploy/clusterissuer-example.yaml) first. Update at least:
- `metadata.name`
- `spec.acme.email`
- `spec.acme.privateKeySecretRef.name`
- `spec.acme.solvers[].dns01.webhook.config.zone`

For higher availability, you can also scale the Deployment above one replica and add your preferred affinity/topology settings.

Then apply it:

```bash
kubectl apply -f deploy/clusterissuer-example.yaml
```

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
