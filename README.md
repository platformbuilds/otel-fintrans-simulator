# OpenTelemetry Financial Transaction Simulator

## Purpose

This simulator generates realistic OpenTelemetry metrics, logs, and traces for a financial transaction processing system. It is used for:

- **Local development**: Testing Mirador Core's correlation and RCA engines with realistic telemetry data
- **Demo scenarios**: Showcasing platform capabilities with domain-specific observability patterns
- **Load testing**: Generating controlled telemetry volumes for performance testing

## Overview

This simulator generates realistic OpenTelemetry metrics, logs, and traces for a financial transaction processing system. It is fully configuration-driven:

- Telemetry naming and outputs are configured through `simulator-config.yaml`.
- Failure scenarios (e.g., bursty failures) are configurable in `simulator-config.yaml`.
- Telemetry outputs can be OTLP, stdout, or both — controlled by `telemetry.outputs` in the YAML.

The simulator is primarily intended for local development, demo scenarios, and load testing.

## Usage

### Running Locally

```bash
# Build the simulator
go build -o bin/otel-fintrans-simulator cmd/otel-fintrans-simulator/main.go

# Run with default settings (sends to localhost:4317)
./bin/otel-fintrans-simulator

# Run with custom OTLP endpoint (configured via simulator-config.yaml)
# Edit `simulator-config.yaml` and set `telemetry.endpoint: "http://my-collector:4317"` and optionally `telemetry.insecure: true`.
# Then run normally:
./bin/otel-fintrans-simulator

# Run with specific transaction rate
TRANSACTION_RATE=100 ./bin/otel-fintrans-simulator

# Use a deterministic RNG seed for reproducible runs
./bin/otel-fintrans-simulator --rand-seed 12345

# Print simulator logs to stdout instead of no-op logging
./bin/otel-fintrans-simulator --log-output stdout
```

### Metric export interval

You can control how often the simulator collects and exports metrics to the configured exporters (OTLP/stdout) with the `--signal-time-interval` flag. The value is a Go duration string (for example `15s`, `30s`, `1m`). The default is `15s`.

Examples:

```bash
# default (15s)
./bin/otel-fintrans-simulator --signal-time-interval=15s

# set to 30 seconds
./bin/otel-fintrans-simulator --signal-time-interval=30s

# set to 1 minute
./bin/otel-fintrans-simulator --signal-time-interval=1m

# using `go run` with a custom interval
go run . --signal-time-interval=15s
```

Note: extremely short intervals may increase CPU/network load; pick an interval appropriate for your testing scenario.

### Configuration

Environment variables:

Telemetry endpoint & protocol
- `telemetry.endpoint`: OTLP collector endpoint (default: `localhost:4317` when not set in config). The simulator supports both gRPC (default port 4317) and HTTP/OTLP (default port 4318).
- `telemetry.insecure`: when true, use plaintext (no TLS) for the selected protocol (default: true).
- `telemetry.skip_tls_verify`: when using TLS, set to `true` to skip certificate verification (InsecureSkipVerify). Default: false.
Validation & helpful warnings
-----------------------------
The simulator performs lightweight validation of your `telemetry` settings at startup and logs warnings for inconsistent combinations. Examples:

- `telemetry.endpoint` uses `http://` but `telemetry.insecure=false` — HTTP is plaintext; either set `telemetry.insecure: true` or use `https://` for TLS.
- `telemetry.endpoint` uses `https://` but `telemetry.insecure=true` — that's inconsistent; either set `telemetry.insecure: false` to use TLS or change the endpoint scheme to `http://`.
- `telemetry.skip_tls_verify` is ignored when `telemetry.insecure` is `true` (plaintext).
Telemetry outputs
-----------------
Telemetry outputs are configured via the `telemetry.outputs` field in `simulator-config.yaml` (no CLI override).

Supported values (single or combined):
- `otlp` — send traces, metrics and logs to the configured OTLP endpoint (default)
- `stdout` — export traces + metrics to stdout (pretty-printed) and print logs to stdout
- `both` — export to both OTLP and stdout

Examples:

1) Use the default OTLP exporter (no change): keep `telemetry.outputs` empty / absent and the simulator will send telemetry to the OTLP endpoint.

2) Use stdout-only or both: edit `simulator-config.yaml` and add `telemetry.outputs: ["stdout"]` or `telemetry.outputs: ["otlp","stdout"]` for the desired behavior (then start simulator normally).

- `OTEL_SERVICE_NAME`: Service name for root traces (default: `api-gateway`)
- `TRANSACTION_RATE`: Transactions per second (default: `10`)
- `ERROR_RATE`: Percentage of failed transactions (default: `5`)
- `SIMULATION_DURATION`: How long to run (default: unlimited)

Configuration file (YAML)
-------------------------
The simulator now supports an optional YAML configuration file that controls telemetry names and failure scheduling.

By default the example config shipped with the tool is `simulator-config.yaml` (in this folder). Use `--config` to point to a custom config file:

```bash
# Use a custom YAML config
./bin/otel-fintrans-simulator --config ./cmd/otel-fintrans-simulator/simulator-config.yaml
```

The `failure` section supports a `bursty` mode and a list of `bursts` where the failure rate is multiplied for a time window. This enables more realistic, correlated failures.

Configuration example
---------------------
The bundled `simulator-config.yaml` (in this folder) contains a compact example which demonstrates:

- Overriding `service_names` used in spans/attributes
- Custom `metric_names` for all instrumented metrics
- A `failure` section that sets a base `rate`, chooses a mode (`bursty` recommended) and one or more `bursts` with `start`, `duration`, and `multiplier` values

Behavior notes
--------------
- If `--config` is not provided or the `failure` section is absent, the simulator falls back to the CLI flags `--failure-rate` and `--failure-mode` (original behaviour).
- If the YAML `failure.seed` is set, the simulator seeds randomness for deterministic runs, which is useful for reproducible demos/tests.

### Failure scenarios (config-driven)

The simulator supports richer, configuration-driven scenario injection. Use the `failure.scenarios` block in `simulator-config.yaml` to declare correlated, multi-metric scenarios. Each scenario contains a `start`, `duration`, optional `labels` (to scope the scenario to specific label values) and a list of `effects`.

An effect targets a named simulator dimension or metric and uses one of the following operations:
- `scale` — multiply the target by the specified value
- `add` — add the specified value
- `set` — set the target to the given value
- `ramp` — increment the target by `step` on each simulation tick

Example (see `simulator-config.yaml` in repo):

```yaml
failure:
  scenarios:
    - name: "db_slow_cascade"
      start: "5s"
      duration: "60s"
      labels:
        OrgId: ["bank_01", "bank_02"]
      effects:
        - metric: "db_latency"
          op: "scale"
          value: 5.0
        - metric: "jvm_gc"
          op: "scale"
          value: 3.0
        - metric: "transaction_failures"
          op: "scale"
          value: 4.0

    - name: "bank03_outage"
      start: "20s"
      duration: "40s"
      labels:
        OrgId: ["bank_03"]
      effects:
        - metric: "kafka_controller_UnderReplicatedPartitions"
          op: "add"
          value: 2
        - metric: "transaction_failures"
          op: "scale"
          value: 8.0
```

When a scenario is active the simulator applies its effects to the runtime state during each background tick. You can mix bursts (simple failure-rate multipliers) with scenario windows for rich, realistic fault patterns.

Hardware-fault scenarios
------------------------
In addition to service- and KPI-focused scenarios, the simulator now supports hardware/infra-fault style effects. These simulate problems such as disk failures impacting Kafka or a bad memory module impacting in-memory datastores (KeyDB/valkey). Example metric names you can use in scenario `effects` include:
- `kafka_disk_failure` — drives increased Kafka produce/consume errors and ISR noise
- `keydb_memory_fault` / `valkey_bad_memory` — drives KeyDB/valkey operation failures and increases redis memory/error signals

Use these effects to model outages that originate in underlying infrastructure (hardware, nodes, network) rather than just service deployments.

Network-fault scenarios
-----------------------
We also support network-specific scenarios to simulate packet drops and network-induced latency — useful when failures originate from unreliable network interfaces, congested links, or router problems. Typical metric names for scenario effects:
- `network_latency` / `node_network_latency_ms` — scales up simulated network latency (affects produce/consume and API gateway processing)
- `network_packet_drop` / `node_network_packet_drops_total` — increases packet drop counts and causes higher messaging errors

When these scenarios are active the simulator increases network latency on affected nodes and emits packet drop counters. That also increases Kafka/consumer errors and may cascade into higher transaction failures.

Scenario examples — copy/paste ready
-----------------------------------
Below are practical, ready-to-use scenario YAML snippets you can copy into `failure.scenarios` in your `simulator-config.yaml`. These show how to simulate common outage classes — service deployment problems, hardware failures, memory faults, and network problems.

1) Database slowdown / deployment outage
```yaml
- name: "db_slow_cascade"
  start: "5s"
  duration: "60s"
  labels:
    OrgId: ["bank_01", "bank_02"]
  effects:
    - metric: "db_latency"
      op: "scale"
      value: 5.0
    - metric: "transaction_failures"
      op: "scale"
      value: 4.0
```

Run this scenario (one-liner):

```bash
cat > /tmp/db_slow_cascade.yaml <<'YAML'
failure:
  scenarios:
    - name: "db_slow_cascade"
      start: "0s"
      duration: "60s"
      labels:
        OrgId: ["bank_01", "bank_02"]
      effects:
        - metric: "db_latency"
          op: "scale"
          value: 5.0
        - metric: "transaction_failures"
          op: "scale"
          value: 4.0
YAML

# start simulator with the scenario (stdout) -- use rand-seed for reproducibility
TRANSACTION_RATE=50 ./bin/otel-fintrans-simulator --config /tmp/db_slow_cascade.yaml --log-output stdout --rand-seed 12345
```

2) Kafka disk / storage failure (hardware-originating outage)
```yaml
- name: "kafka_disk_issue"
  start: "0s"
  duration: "90s"
  labels:
    OrgId: ["bank_01"]
  effects:
    - metric: "kafka_disk_failure"   # simulator maps this to higher kafka errors + ISR noise
      op: "scale"
      value: 4.0
    - metric: "kafka_controller_UnderReplicatedPartitions"
      op: "add"
      value: 10
```

Run this scenario (one-liner):

```bash
cat > /tmp/kafka_disk_issue.yaml <<'YAML'
failure:
  scenarios:
    - name: "kafka_disk_issue"
      start: "0s"
      duration: "90s"
      labels:
        OrgId: ["bank_01"]
      effects:
        - metric: "kafka_disk_failure"
          op: "scale"
          value: 4.0
        - metric: "kafka_controller_UnderReplicatedPartitions"
          op: "add"
          value: 10
YAML

TRANSACTION_RATE=50 ./bin/otel-fintrans-simulator --config /tmp/kafka_disk_issue.yaml --log-output stdout --rand-seed 12345
```

3) KeyDB / valkey memory fault (bad RAM causing in-memory DB failures)
```yaml
- name: "keydb_memory_corruption"
  start: "0s"
  duration: "2m"
  labels:
    OrgId: ["bank_02"]
  effects:
    - metric: "keydb_memory_fault"  # increases KeyDB failures and redis miss/eviction noise
      op: "scale"
      value: 5.0
    - metric: "redis_memory"
      op: "scale"
      value: 2.5
```

Run this scenario (one-liner):

```bash
cat > /tmp/keydb_memory_corruption.yaml <<'YAML'
failure:
  scenarios:
    - name: "keydb_memory_corruption"
      start: "0s"
      duration: "2m"
      labels:
        OrgId: ["bank_02"]
      effects:
        - metric: "keydb_memory_fault"
          op: "scale"
          value: 5.0
        - metric: "redis_memory"
          op: "scale"
          value: 2.5
YAML

TRANSACTION_RATE=50 ./bin/otel-fintrans-simulator --config /tmp/keydb_memory_corruption.yaml --log-output stdout --rand-seed 12345
```

4) Network packet loss
```yaml
- name: "network_packet_loss"
  start: "30s"
  duration: "90s"
  labels:
    OrgId: ["bank_03"]
  effects:
    - metric: "network_packet_drop"
      op: "scale"
      value: 6.0
    - metric: "node_network_packet_drops_total"
      op: "add"
      value: 10
```

Run this scenario (one-liner):

```bash
cat > /tmp/network_packet_loss.yaml <<'YAML'
failure:
  scenarios:
    - name: "network_packet_loss"
      start: "0s"
      duration: "90s"
      labels:
        OrgId: ["bank_03"]
      effects:
        - metric: "network_packet_drop"
          op: "scale"
          value: 6.0
        - metric: "node_network_packet_drops_total"
          op: "add"
          value: 10
YAML

TRANSACTION_RATE=50 ./bin/otel-fintrans-simulator --config /tmp/network_packet_loss.yaml --log-output stdout --rand-seed 12345
```

5) Network latency spike
```yaml
- name: "network_latency_spike"
  start: "45s"
  duration: "1m30s"
  labels:
    OrgId: ["bank_02"]
  effects:
    - metric: "network_latency"   # scales node-level network latency (ms)
      op: "scale"
      value: 5.0
```

Run this scenario (one-liner):

```bash
cat > /tmp/network_latency_spike.yaml <<'YAML'
failure:
  scenarios:
    - name: "network_latency_spike"
      start: "0s"
      duration: "1m30s"
      labels:
        OrgId: ["bank_02"]
      effects:
        - metric: "network_latency"
          op: "scale"
          value: 5.0
YAML

TRANSACTION_RATE=50 ./bin/otel-fintrans-simulator --config /tmp/network_latency_spike.yaml --log-output stdout --rand-seed 12345
```

6) Redis/KeyDB memory bloat
```yaml
- name: "redis_memory_bloat"
  start: "20s"
  duration: "90s"
  labels:
    OrgId: ["bank_03"]
  effects:
    - metric: "redis_memory"
      op: "scale"
      value: 2.0
    - metric: "redis_evicted_keys_total"
      op: "add"
      value: 20
```

Run this scenario (one-liner):

```bash
cat > /tmp/redis_memory_bloat.yaml <<'YAML'
failure:
  scenarios:
    - name: "redis_memory_bloat"
      start: "0s"
      duration: "90s"
      labels:
        OrgId: ["bank_03"]
      effects:
        - metric: "redis_memory"
          op: "scale"
          value: 2.0
        - metric: "redis_evicted_keys_total"
          op: "add"
          value: 20
YAML

TRANSACTION_RATE=40 ./bin/otel-fintrans-simulator --config /tmp/redis_memory_bloat.yaml --log-output stdout --rand-seed 12345
```

7) Tomcat thread ramp & queue growth (load generation)
```yaml
- name: "tomcat_thread_ramp"
  start: "15s"
  duration: "2m"
  labels:
    OrgId: ["bank_02", "bank_04"]
  effects:
    - metric: "tomcat_threads"
      op: "ramp"
      step: 2
    - metric: "tomcat_threads_queue_seconds"
      op: "add"
      value: 5
```

Run this scenario (one-liner):

```bash
cat > /tmp/tomcat_thread_ramp.yaml <<'YAML'
failure:
  scenarios:
    - name: "tomcat_thread_ramp"
      start: "0s"
      duration: "2m"
      labels:
        OrgId: ["bank_02", "bank_04"]
      effects:
        - metric: "tomcat_threads"
          op: "ramp"
          step: 2
        - metric: "tomcat_threads_queue_seconds"
          op: "add"
          value: 5
YAML

TRANSACTION_RATE=100 ./bin/otel-fintrans-simulator --config /tmp/tomcat_thread_ramp.yaml --log-output stdout --rand-seed 12345
```

8) Kafka under-replicated partitions burst (controller-level instability)
```yaml
- name: "kafka_underreplicated_burst"
  start: "30s"
  duration: "1m"
  labels:
    OrgId: ["bank_01", "bank_03"]
  effects:
    - metric: "kafka_controller_UnderReplicatedPartitions"
      op: "add"
      value: 5
    - metric: "transaction_failures"
      op: "scale"
      value: 6.0
```

Run this scenario (one-liner):

```bash
cat > /tmp/kafka_underreplicated_burst.yaml <<'YAML'
failure:
  scenarios:
    - name: "kafka_underreplicated_burst"
      start: "0s"
      duration: "60s"
      labels:
        OrgId: ["bank_01", "bank_03"]
      effects:
        - metric: "kafka_controller_UnderReplicatedPartitions"
          op: "add"
          value: 5
        - metric: "transaction_failures"
          op: "scale"
          value: 6.0
YAML

TRANSACTION_RATE=50 ./bin/otel-fintrans-simulator --config /tmp/kafka_underreplicated_burst.yaml --log-output stdout --rand-seed 12345
```

9) Composite / partial outage (multi-system cascade)
```yaml
- name: "partial_outage_bank03"
  start: "60s"
  duration: "2m"
  labels:
    OrgId: ["bank_03"]
  effects:
    - metric: "kafka_controller_UnderReplicatedPartitions"
      op: "add"
      value: 3
    - metric: "db_latency"
      op: "scale"
      value: 6.0
    - metric: "transaction_failures"
      op: "scale"
      value: 8.0
```

Run this scenario (one-liner):

```bash
cat > /tmp/partial_outage_bank03.yaml <<'YAML'
failure:
  scenarios:
    - name: "partial_outage_bank03"
      start: "0s"
      duration: "2m"
      labels:
        OrgId: ["bank_03"]
      effects:
        - metric: "kafka_controller_UnderReplicatedPartitions"
          op: "add"
          value: 3
        - metric: "db_latency"
          op: "scale"
          value: 6.0
        - metric: "transaction_failures"
          op: "scale"
          value: 8.0
YAML

TRANSACTION_RATE=60 ./bin/otel-fintrans-simulator --config /tmp/partial_outage_bank03.yaml --log-output stdout --rand-seed 12345
```

10) Mixed signals (contradictory telemetry)
```yaml
- name: "kafka_mixed_signals"
  start: "0s"
  duration: "90s"
  labels:
    OrgId: ["bank_01"]
  effects:
    - metric: "kafka_disk_failure"
      op: "scale"
      value: 4.0
    - metric: "kafka_throughput"
      op: "scale"
      value: 0.2

# This produces more kafka errors and URP while reducing requests/throughput — a mixed signal pattern
```

Run this scenario (one-liner):

```bash
cat > /tmp/kafka_mixed_signals.yaml <<'YAML'
failure:
  scenarios:
    - name: "kafka_mixed_signals"
      start: "0s"
      duration: "90s"
      labels:
        OrgId: ["bank_01"]
      effects:
        - metric: "kafka_disk_failure"
          op: "scale"
          value: 4.0
        - metric: "kafka_throughput"
          op: "scale"
          value: 0.2
YAML

TRANSACTION_RATE=50 ./bin/otel-fintrans-simulator --config /tmp/kafka_mixed_signals.yaml --log-output stdout --rand-seed 12345
```

Tips & mapping notes
- Metric names are flexible — the scheduler accepts the common names listed above and applies them to the simulator runtime state. If your telemetry backend expects custom names, update `telemetry.metric_names` in `simulator-config.yaml`.
- Use `labels` to scope scenarios to specific OrgIds, OrgNames, or transaction types — this helps exercise RCA/correlation engines against targeted faults.
- `scale` and `add` are useful for magnitude changes; `ramp` is useful for gradual increases over the scenario duration.

Now that you have concrete scenarios, your RCA and correlation engines can detect and classify whether failures originate in service deployments, infra (e.g., disks/memory), or network layers. Use the deterministic seed (`failure.seed`) to make demo runs reproducible for tests and demos.

Examples directory
------------------
I've added ready-to-run scenario configuration files under `examples/scenarios/`. Use `examples/run_scenario.sh <name>` to run a scenario quickly — the script will build the binary if necessary and run the simulator with sensible defaults.

Example:

```bash
# run the kafka_disk_issue scenario (from examples/scenarios/kafka_disk_issue.yaml)
examples/run_scenario.sh kafka_disk_issue
```

Available example scenario configs (full configs)
----------------------------------
Below are the example scenario files included in `examples/scenarios/`. Each file is a full, standalone simulator YAML (contains `telemetry`, `metric_names`, `labels` and the `failure` section), ready to run as-is.

- `db_slow_cascade.yaml` — database slowdown / deployment outage (increased db latency, more transaction failures)
- `kafka_disk_issue.yaml` — hardware disk/storage failure affecting Kafka (more produce/consume errors, higher URP)
- `keydb_memory_corruption.yaml` — bad memory for KeyDB / valkey (higher KeyDB failures and Redis noise)
- `network_packet_loss.yaml` — network packet drops (increased packet drops and noisy messaging errors)
- `network_latency_spike.yaml` — network latency spike (higher network latency across nodes, impacts consumer/producer latencies)
- `redis_memory_bloat.yaml` — Redis/KeyDB memory bloat and evictions
- `tomcat_thread_ramp.yaml` — Tomcat thread busy ramp and queue increases (load stress)
- `kafka_underreplicated_burst.yaml` — Kafka under-replication burst (controller instability)
- `kafka_mixed_signals.yaml` — mixed signal: more Kafka errors with reduced throughput (contradictory telemetry)
- `partial_outage_bank03.yaml` — composite partial outage targeting bank_03 (multi-system cascade)

Run any of them with the helper script, for example:

```bash
examples/run_scenario.sh kafka_mixed_signals
```

Or run directly using the binary and config path:

```bash
TRANSACTION_RATE=50 ./bin/otel-fintrans-simulator --config examples/scenarios/kafka_mixed_signals.yaml --log-output stdout
```

## Integration with Mirador Core

### Testing Correlation Engine

```bash
# 1. Start the simulator
./bin/otel-fintrans-simulator &

# 2. Wait for telemetry to accumulate (30s)
sleep 30

# 3. Query correlation API with time window
curl -X POST http://localhost:8010/api/v1/unified/correlate \
  -H "Content-Type: application/json" \
  -d '{
    "startTime": "2025-11-25T10:00:00Z",
    "endTime": "2025-11-25T10:15:00Z"
  }'
```

The correlation engine will:
1. Discover `transactions_failed_total` via KPI registry.
2. Identify correlated service latency patterns
3. Build service graph from observed trace telemetry
4. Return correlation result with confidence scores

### Testing RCA Engine

```bash
# Inject a specific failure
INJECT_DB_LATENCY=true ./bin/otel-fintrans-simulator &

# Query RCA for root cause analysis
curl -X POST http://localhost:8010/api/v1/unified/rca \
  -H "Content-Type: application/json" \
  -d '{
    "startTime": "2025-11-25T10:05:00Z",
    "endTime": "2025-11-25T10:10:00Z"
  }'
```

## Contributing

When modifying the simulator:

1. **Keep domain fidelity**: Financial transaction vocabulary should remain realistic
2. **Don't pollute engines**: Avoid copying simulator defaults into `internal/services/correlation_engine.go`; engines should discover KPIs dynamically via the registry.
3. **Update this README**: Document any new metrics, service names, or configuration changes
4. **Test with real Mirador**: Ensure correlation/RCA engines still work via registry discovery

## License

Apache 2.0 (see top-level LICENSE file)

## TODO / Next work items

Below is a proposed, ordered list of follow-up improvements and small issues we plan to take up one-by-one. These are intentionally scoped so we can address them in small, reviewable PRs that improve developer experience and simulation fidelity.

1. Make logging configurable and developer-friendly ✅
  - Add a `--log` or `--log-output` flag that allows `stdout`/`nop` (default) and `otlp` so developers can see console logs locally by default. (Implemented: `--log-output` supports `nop` and `stdout`.)
  - Add an environment variable override (e.g., `SIM_LOG_OUTPUT`).

2. Graceful shutdown and context cancellation ✅
  - Add a signal handler (SIGINT/SIGTERM) and a cancellable context to allow the simulator to stop quickly and shut down OTel providers cleanly. (Implemented)

3. Improve time-series scheduling and backfill behavior
  - Tweak scheduling so that future/edge offsets don't block for long periods (consider a bounded scheduler and non-blocking generation for unreachable timestamps).

4. Make telemetry configuration pluggable ✅
  - Done: `simulator-config.yaml` added and `--config` flag implemented. See `config.go` + examples and unit tests (`config_test.go`).

5. Fix metric instrument types & semantics ✅
  - Replace `transaction_amount_paisa_count` (previously an UpDownCounter) with a monotonic `Counter` (done).

6a. Module / developer setup
  - Add a `go.mod` and `go.sum` (module-aware layout) so `go test ./...` and other Go tooling work for developers and CI. This helps keep dependency versions stable for reproducible test runs.

Developer setup & Make targets
--------------------------------
For local development we've added a convenient Makefile with a couple of helpful targets:

- `make build` — builds the simulator binary into `bin/`.
- `make test` — runs `go test ./...`.
- `make localdev-sim` — builds and runs a small sample using default settings (targeting a local collector)

See `Makefile` at the project root.

6. Add reproducibility and seeding ✅
  - Add a `--rand-seed` flag so tests and demo runs can generate reproducible streams. (Implemented)

7. Improve failure-mode realism ✅
  - Done: burst/scheduled failure support and deterministic seeding implemented. See `config.go`, `simulator-config.yaml` and `scheduler_test.go` for examples and tests.

8. Add tests & CI automation for simulator build and basic runtime
  - Small unit tests for metrics initialization and a quick smoke `go test` that validates `initOTel` can be invoked in a test shim (using a fake collector or a no-op option).

CI status
---------
We've added a lightweight GitHub Actions workflow `.github/workflows/ci.yml` that runs `go test ./...`, `gofmt` checks, and `go vet` on pushes and PRs to `main`.

Releases & CI manual trigger
---------------------------
The repository includes a manual release workflow you can trigger from the GitHub Actions UI (Actions → CI → Run workflow). The workflow builds a platform-tagged binary and (optionally) builds & pushes a container image to GHCR and DockerHub.

Required inputs when running the workflow manually:
- `release_tag` (required) — the version that will be used for the release and image tag (for example `v1.0.0`).
- `push_image` (optional, default true) — whether to build & push container images.
- `platform` (optional, default `amd64`) — architecture part of the image tag, used for GOARCH and image tag.
- `dockerhub_repository` (optional) — `owner/repo` on Docker Hub if you want the workflow to push to Docker Hub in addition to GHCR.

Secrets / permissions
- GHCR: the workflow uses the repository's `GITHUB_TOKEN` to push container images to GitHub Container Registry. The workflow requests `packages: write` permission.
- DockerHub: if you want the CI to push images to Docker Hub, set these repository secrets:
  - `DOCKERHUB_USERNAME`
  - `DOCKERHUB_TOKEN` (or personal access token)

Image / asset naming
- Container image tag format: `{release_tag}-{platform}` (e.g. `v1.0.0-amd64`) — pushed as `ghcr.io/<owner>/<repo>:<tag>` and, if enabled, `docker.io/<dockerhub_repository>:<tag>`.
- Release binary asset name: `otel-fintrans-simulator-{release_tag}-{platform}` (uploaded to the GitHub Release created by the workflow).

NOTE: The manual release workflow is currently restricted to the repository owner when invoked from the UI; if you want to allow additional actors or enable automatic tag-driven releases, I can update the workflow accordingly.

9. Add a convenient local-dev Make target
  - `make localdev-sim` or `make run-simulator` that builds and runs the simulator against a local collector or logs to stdout for quick demos.

10. Document example `otel-collector` pipeline and how to point the simulator at it
   - Add clear examples showing collector config for receivers/exporters so new developers can get telemetry into Observability tooling quickly.

Collector example
-----------------
There's a minimal OTEL Collector pipeline example in `examples/otel-collector-pipeline.yaml` that accepts OTLP and writes logs (useful for local testing). Point the simulator at your collector using `OTEL_EXPORTER_OTLP_ENDPOINT` or `--otlp-endpoint`.

Additional instrumentation notes (metrics & labels) ✅
-----------------------------------------------
- Use clear, dedicated metrics for each subsystem's latency (e.g., `kafka_produce_latency_seconds`, `kafka_consume_latency_seconds`) instead of recording Kafka latency into `db_latency_seconds` to avoid conflating database and messaging latencies. (Implemented in code: kafka latency histograms are registered and used.)
- Ensure key metric instruments include useful attributes such as `service_name`, `messaging.destination` (topic), and `db_system` where appropriate to make traces/metrics more useful for RCA and correlation.

Histogram & PromQL compatibility
--------------------------------
To support PromQL queries that use histogram_quantile() the simulator emits Prom-style histogram counters for key latency metrics in addition to OpenTelemetry histograms. For example `transaction_latency_seconds` has these exported counters:

- `transaction_latency_seconds_bucket{le="..."}` (monotonic cumulative buckets)
- `transaction_latency_seconds_sum` (monotonic sum of latencies)
- `transaction_latency_seconds_count` (monotonic count)

Same applies for `db_latency_seconds` and other configured histogram metrics. The simulator emits these bucketed counters with the same attributes and labels you configure (e.g., `service_name`, `OrgId`) so PromQL queries like `histogram_quantile(0.95, sum(rate(transaction_latency_seconds_bucket[5m])) by (le))` will return meaningful values.

Labels and cardinality
----------------------
The simulator can emit attributes (labels) on metrics and traces — configured under `telemetry.labels` in `simulator-config.yaml`. Typical label sets include `OrgId`, `OrgName`, and `transaction_type`. Keep label cardinality small (5-25 unique values) to avoid large memory and ingestion costs in backends.

Example label config in `simulator-config.yaml`:

```yaml
telemetry:
  labels:
    org_ids: ["bank_01","bank_02","bank_03"]
    org_names: ["BankOne","BankTwo","BankThree"]
    transaction_types: ["merchant_payment","p2p","bill_payment"]
```


We'll take these up one at a time — the already completed items (4 and 7) are marked above. Tell me which remaining item you'd like me to start next and I’ll open a focused PR/branch for it.
