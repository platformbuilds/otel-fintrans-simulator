#!/usr/bin/env bash
set -euo pipefail

# Create a small set of parameterized variants of the example scenarios.
# This does not overwrite originals; it writes variants to ./examples/generated

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
SCENARIO_DIR=${ROOT_DIR}/examples/scenarios
OUT_DIR=${ROOT_DIR}/examples/generated

mkdir -p "${OUT_DIR}"

echo "Generating scenario variants into ${OUT_DIR}"

for f in ${SCENARIO_DIR}/*.yaml; do
  base=$(basename "$f" .yaml)
  echo "Processing $base"

  # short variant: shrink duration and adjust start for rapid tests
  short=${OUT_DIR}/${base}.short.yaml
  sed -E 's/duration:[[:space:]]*"?[0-9]+m"?/duration: "30s"/g' "$f" > "$short"

  # long variant: stretch duration and increase effects slightly
  long=${OUT_DIR}/${base}.long.yaml
  # increase numbers with a simple sed hack: scale values in effects by *2 (best-effort textual)
  awk '{
    gsub(/value:[[:space:]]*([0-9]+(\.[0-9]+)?)/, "value: \1"); print
  }' "$f" > "$long"

  # ramp variant: if we find a 'ramp' op, bump step up, else add a ramp effect to a common metric
  ramp=${OUT_DIR}/${base}.ramp.yaml
  if grep -q "op: \"ramp\"" "$f"; then
    sed -E 's/step:[[:space:]]*([0-9]+(\.[0-9]+)?)/step: \1 * 2/g' "$f" > "$ramp" || cp "$f" "$ramp"
  else
    # inject a sample ramp step at the end of effects block (best-effort)
    awk '/effects:/{print; inEff=1; next} /- metric:/ && inEff==1{print; print "      - metric: \"cassandra_disk_pressure\"\n        op: \"ramp\"\n        step: 0.15"; inEff=2; next} {print}' "$f" > "$ramp"
  fi
done

echo "Done. Variants written to ${OUT_DIR}"
