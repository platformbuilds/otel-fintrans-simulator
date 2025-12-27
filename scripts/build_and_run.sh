#!/usr/bin/env bash
set -euo pipefail

# Build the simulator binary and run it. Usage:
# ./scripts/build_and_run.sh --config path/to/config.yaml [--log-output stdout] [other args passed to binary]

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
BINARY=${ROOT_DIR}/bin/otel-fintrans-simulator

if [[ ! -x "${BINARY}" ]]; then
  echo "Building simulator binary into ${BINARY}..."
  (cd "${ROOT_DIR}" && go build -o "${BINARY}" .)
fi

# run the binary with any args passed through
echo "Running ${BINARY} $@"
"${BINARY}" "$@"
