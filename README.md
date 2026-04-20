# hetzner-acme-webhook

Minimal [cert-manager](https://cert-manager.io/) DNS01 webhook for Hetzner DNS.

## Configuration

Environment variables:

- `HETZNER_DNS_API_TOKEN` — optional if token file exists; token file preferred
- `HETZNER_DNS_API_BASE_URL` — optional, defaults to `https://api.hetzner.cloud/v1`
- `HETZNER_DNS_ZONE` — required unless solver config provides `zone`

Webhook reads token from `/var/run/secrets/hetzner-dns/token` if `HETZNER_DNS_API_TOKEN` is unset. For deployments, token file is preferred over environment variable.

Solver config:

```json
{
  "zone": "example.com"
}
```

If `zone` is omitted, webhook falls back to `HETZNER_DNS_ZONE`.

## Build

```bash
go test ./...
go build ./cmd/hetzner-acme-webhook
docker build -t ghcr.io/mattwend/hetzner-acme-webhook:latest .
```

## cert-manager example

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-hetzner
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: you@example.com
    privateKeySecretRef:
      name: letsencrypt-hetzner
    solvers:
      - dns01:
          webhook:
            groupName: acme.example.com
            solverName: hetzner
            config:
              zone: example.com
```

## Security

See [SECURITY.md](SECURITY.md).

## License

GPL-3.0-only. See [LICENSE](LICENSE).
