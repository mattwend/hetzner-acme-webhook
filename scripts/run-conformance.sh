#!/usr/bin/env bash
set -euo pipefail

# Run the cert-manager DNS01 webhook conformance test locally.
#
# Requirements:
# - HETZNER_DNS_API_TOKEN exported in the shell
# - Go installed
# - Internet access to download envtest assets on first run
#
# Usage:
#   scripts/run-conformance.sh neue-grafik.de.
#   K8S_VERSION=1.34.x! scripts/run-conformance.sh neue-grafik.de.
#
# Notes:
# - The zone argument must include a trailing dot.
# - This script never prints the Hetzner token.
# - The test only copies manifest files (.json/.yaml/.yml) from testdata/hetzner,
#   so README.md in that directory is ignored.
# - TMPDIR is set to ./.tmp for go test to avoid temp-dir related fixture issues.
# - Podman is preferred when running the optional containerized variant,
#   but this script runs the test directly on the host.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

ZONE="${1:-}"
K8S_VERSION="${K8S_VERSION:-1.34.x!}"
SETUP_ENVTEST_VERSION="${SETUP_ENVTEST_VERSION:-release-0.22}"
SETUP_ENVTEST="${SETUP_ENVTEST:-$(go env GOPATH)/bin/setup-envtest}"
TESTBIN_DIR="${TESTBIN_DIR:-$ROOT_DIR/.testbin}"
SECRET_FILE="$ROOT_DIR/testdata/hetzner/secret.yaml"

usage() {
  cat <<'EOF'
Usage:
  scripts/run-conformance.sh <zone-with-trailing-dot>

Example:
  scripts/run-conformance.sh neue-grafik.de.

This script creates testdata/hetzner/secret.yaml temporarily for the test run
and removes it again on exit.

Environment:
  HETZNER_DNS_API_TOKEN   Required. Hetzner DNS API token.
  K8S_VERSION             Optional. Envtest Kubernetes version selector. Default: 1.34.x!
  SETUP_ENVTEST_VERSION   Optional. setup-envtest branch/version. Default: release-0.22
  TESTBIN_DIR             Optional. Directory for envtest binaries. Default: ./.testbin
  SETUP_ENVTEST           Optional. Path to setup-envtest binary.
EOF
}

if [[ -z "$ZONE" ]]; then
  usage >&2
  exit 1
fi

if [[ "$ZONE" != *. ]]; then
  echo "error: zone must include a trailing dot, e.g. neue-grafik.de." >&2
  exit 1
fi

if [[ -z "${HETZNER_DNS_API_TOKEN:-}" ]]; then
  echo "error: HETZNER_DNS_API_TOKEN is not set" >&2
  exit 1
fi

cleanup() {
  rm -f "$SECRET_FILE"
}
trap cleanup EXIT

mkdir -p "$ROOT_DIR/testdata/hetzner"

python3 - <<'PY'
import os, pathlib
root = pathlib.Path.cwd()
path = root / 'testdata' / 'hetzner' / 'secret.yaml'
token = os.environ['HETZNER_DNS_API_TOKEN']
path.write_text(
    'apiVersion: v1\n'
    'kind: Secret\n'
    'metadata:\n'
    '  name: hetzner-dns-secret\n'
    '  namespace: cert-manager\n'
    'stringData:\n'
    f'  token: "{token}"\n'
)
print(f'wrote {path.relative_to(root)}')
PY

export PATH="$(go env GOPATH)/bin:$PATH"
if ! command -v setup-envtest >/dev/null 2>&1; then
  echo "Installing setup-envtest (${SETUP_ENVTEST_VERSION})..."
  go install "sigs.k8s.io/controller-runtime/tools/setup-envtest@${SETUP_ENVTEST_VERSION}"
fi

mkdir -p "$TESTBIN_DIR"
ENVTEST_PATH="$(setup-envtest use "$K8S_VERSION" --bin-dir "$TESTBIN_DIR" -p path)"

export TEST_ASSET_ETCD="$ENVTEST_PATH/etcd"
export TEST_ASSET_KUBE_APISERVER="$ENVTEST_PATH/kube-apiserver"
export TEST_ASSET_KUBECTL="$ENVTEST_PATH/kubectl"
export TEST_ZONE_NAME="$ZONE"

for f in "$TEST_ASSET_ETCD" "$TEST_ASSET_KUBE_APISERVER" "$TEST_ASSET_KUBECTL"; do
  if [[ ! -x "$f" ]]; then
    echo "error: missing or non-executable test asset: $f" >&2
    exit 1
  fi
done

echo "Using setup-envtest version: $SETUP_ENVTEST_VERSION"
echo "Using envtest selector: $K8S_VERSION"
echo "Envtest asset directory: $ENVTEST_PATH"
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum "$TEST_ASSET_ETCD" "$TEST_ASSET_KUBE_APISERVER" "$TEST_ASSET_KUBECTL"
elif command -v shasum >/dev/null 2>&1; then
  shasum -a 256 "$TEST_ASSET_ETCD" "$TEST_ASSET_KUBE_APISERVER" "$TEST_ASSET_KUBECTL"
else
  echo "warning: no SHA256 tool found; skipping checksum output" >&2
fi

echo "Running conformance test for zone: $TEST_ZONE_NAME"
TMPDIR="$ROOT_DIR/.tmp" go test -v -tags=e2e -count=1 . -run TestRunsSuite

echo "Done. Removed temporary testdata/hetzner/secret.yaml"
