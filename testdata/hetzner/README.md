# E2e conformance test data

This directory holds configuration for the cert-manager DNS01 provider
conformance test suite.

## Running the tests

```bash
export HETZNER_DNS_API_TOKEN="your-token"
export TEST_ZONE_NAME="neue-grafik.de."
go test -v -count=1 -tags=e2e ./...
# or:
scripts/run-conformance.sh neue-grafik.de.
```

The token must have access to the zone specified in `TEST_ZONE_NAME`.
The zone name **must** include a trailing dot.

Notes:
- `secret.yaml` is created temporarily by `scripts/run-conformance.sh` and is not
  meant to be committed.
- The conformance test harness uses only manifest files from this directory
  (`.json`, `.yaml`, `.yml`). This README is documentation only.
- cert-manager's conformance suite uses `example.com` as the challenge `DNSName`
  by default. The actual DNS01 record under test is built from `TEST_ZONE_NAME`,
  e.g. `cert-manager-dns01-tests.neue-grafik.de.`.
