#!/usr/bin/env bash
set -euo pipefail

# Usage: examples/run_scenario.sh <scenario-file-or-name>
# If a single basename is provided (no /), it looks under examples/scenarios/<name>.yaml
# This script will build the simulator binary if ./bin/otel-fintrans-simulator is not present.

SCENARIO=${1:-}
if [[ -z "$SCENARIO" ]]; then
  echo "Usage: $0 <scenario-file-or-name>"
  exit 2
fi

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
BIN=${ROOT_DIR}/bin/otel-fintrans-simulator

# Determine scenario file
if [[ "${SCENARIO}" == */* || -f "${SCENARIO}" ]]; then
  CFG_PATH="${SCENARIO}"
else
  CFG_PATH="${ROOT_DIR}/examples/scenarios/${SCENARIO}.yaml"
fi

if [[ ! -f "${CFG_PATH}" ]]; then
  echo "Scenario config not found: ${CFG_PATH}"
  exit 1
fi

# Build binary if missing
if [[ ! -x "${BIN}" ]]; then
  echo "Building simulator binary into ${BIN}..."
  (cd "${ROOT_DIR}" && go build -o "${BIN}" .)
fi

echo "Running simulator with config: ${CFG_PATH}"
# default: run with log-output stdout for local debugging
TRANSACTION_RATE=${TRANSACTION_RATE:-50} "$BIN" --config "${CFG_PATH}" --log-output stdout --rand-seed 12345
