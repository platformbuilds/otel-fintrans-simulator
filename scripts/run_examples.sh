#!/usr/bin/env bash
set -euo pipefail

# Utility to list or run example scenarios shipped in examples/scenarios
# Usage:
#   ./scripts/run_examples.sh list
#   ./scripts/run_examples.sh run <scenario-name|path-to-yaml>
#   ./scripts/run_examples.sh run-all  # runs every scenario one-by-one using lightweight settings

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
EXAMPLE_DIR=${ROOT_DIR}/examples/scenarios
RUN_SCRIPT=${ROOT_DIR}/examples/run_scenario.sh

if [[ $# -lt 1 ]]; then
  echo "Usage: $0 <list|run|run-all> [args...]"
  exit 2
fi

cmd=${1}
shift || true

case "$cmd" in
  list)
    echo "Available example scenarios:" && ls -1 ${EXAMPLE_DIR}/*.yaml || true
    ;;

  run)
    if [[ $# -lt 1 ]]; then
      echo "Usage: $0 run <scenario-name-or-path>"
      exit 2
    fi
    SCENARIO=$1
    shift
    # Delegate to examples/run_scenario.sh (it will build binary if missing)
    "$RUN_SCRIPT" "$SCENARIO" "$@"
    ;;

  run-all)
    echo "Running all examples sequentially with lightweight settings (short run, stdout logging)"
    TRANSACTIONS=${TRANSACTIONS:-50}
    TIME_WINDOW=${TIME_WINDOW:-30s}
    DATA_INTERVAL=${DATA_INTERVAL:-1s}
    SIGNAL_TIME_INTERVAL=${SIGNAL_TIME_INTERVAL:-5s}
    CONCURRENCY=${CONCURRENCY:-4}

    for f in ${EXAMPLE_DIR}/*.yaml; do
      echo "--- Running: ${f} ---"
      # call example runner with environment overrides
      TRANSACTIONS=${TRANSACTIONS} TIME_WINDOW=${TIME_WINDOW} DATA_INTERVAL=${DATA_INTERVAL} \
        SIGNAL_TIME_INTERVAL=${SIGNAL_TIME_INTERVAL} CONCURRENCY=${CONCURRENCY} \
        "$RUN_SCRIPT" "$f"
      echo "--- Completed: ${f} ---\n"
    done
    ;;

  *)
    echo "Unknown command: $cmd"
    exit 2
    ;;
esac
