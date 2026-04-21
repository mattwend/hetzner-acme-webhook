# E2e conformance test data

This directory holds configuration for the cert-manager DNS01 provider
conformance test suite.

## Running the tests

```bash
export HETZNER_DNS_API_TOKEN="your-token"
export TEST_ZONE_NAME="example.com."
go test -v -count=1 -tags=e2e ./...
```

The token must have access to the zone specified in `TEST_ZONE_NAME`.
The zone name **must** include a trailing dot.
