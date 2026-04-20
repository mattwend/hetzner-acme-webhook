# Security Policy

## Supported use

This project is a minimal cert-manager DNS01 webhook for Hetzner DNS.
It is intended to run inside Kubernetes with a narrowly scoped Hetzner API token and explicit zone configuration.

## Reporting a vulnerability

Until a dedicated security contact is published, please report vulnerabilities privately to the repository maintainers.
Do not open a public issue for unpatched vulnerabilities.

When reporting, include:
- affected version or commit
- deployment context
- reproduction steps
- expected impact
- any suggested mitigation

## Threat model

Primary assets:
- Hetzner DNS API token
- control of TXT records under the configured DNS zone
- certificate issuance flow that depends on `_acme-challenge` records

Expected trust boundaries:
- cert-manager invokes the webhook
- the webhook calls the Hetzner Cloud API over TLS
- Kubernetes provides the token secret to the webhook process

Main threats considered:
- challenge input causing DNS changes outside the intended zone
- path or API endpoint injection through record names
- accidental deletion of unrelated ACME challenge records
- excessive health probing causing unnecessary upstream API traffic
- oversized API responses causing memory pressure
- accidental secret exposure through environment configuration or logging

## Current hardening

The webhook currently:
- requires an explicit `HETZNER_DNS_ZONE`
- rejects FQDNs outside the configured zone
- validates record names before DNS mutations
- URL-escapes record names in delete paths
- disables startup garbage collection of challenge records
- limits upstream API response size during JSON decode
- caches health probe results briefly
- logs DNS mutations without logging the API token
- runs well in a distroless non-root container image

## Deployment guidance

Recommended controls:
- prefer mounting the Hetzner token as a file instead of an environment variable
- use a dedicated Hetzner token with the minimum permissions needed for the target zone
- restrict network access to the webhook where practical
- expose health endpoints only inside the cluster
- pin container images by digest
- enable Kubernetes audit logging and retain application logs for incident response
- rotate the Hetzner token if compromise is suspected

## Operational notes

Blast radius:
- limited to TXT record management in the configured zone, assuming the Hetzner token is scoped appropriately

External dependencies:
- Hetzner Cloud DNS API at `https://api.hetzner.cloud/v1`
- Kubernetes and cert-manager

Recovery:
- revoke and replace the Hetzner token
- redeploy the webhook with the new secret
- inspect and repair `_acme-challenge` TXT records in the configured zone
- re-trigger failed certificate issuances after DNS state is corrected
