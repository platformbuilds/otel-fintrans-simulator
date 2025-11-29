# OpenTelemetry Financial Transaction Simulator

## Purpose

This simulator generates realistic OpenTelemetry metrics, logs, and traces for a financial transaction processing system. It is used for:

- **Local development**: Testing Mirador Core's correlation and RCA engines with realistic telemetry data
- **Demo scenarios**: Showcasing platform capabilities with domain-specific observability patterns
- **Load testing**: Generating controlled telemetry volumes for performance testing

## Hardcoded Values - Exemption Rationale

**NOTE (HCB-008)**: Historically this simulator contained hardcoded metric names, service names, and transaction attributes **by design**. This remains an approved exemption from AGENTS.md §3.6 for simulation fidelity, however the simulator now supports a fully-configurable domain model via `simulator-config.yaml` or the `--config` CLI flag. The values listed below are the default names the simulator uses when no config is provided.

### Why Hardcoding is Acceptable Here

1. **Simulation Fidelity**: The simulator must generate specific, realistic metric names (`transactions_total`, `transactions_failed_total`, etc.) that mirror actual financial services observability patterns.

2. **Not Production Code**: This is a development/testing tool, not part of the Mirador Core engine logic. It exists solely to generate synthetic data.

3. **Domain-Specific Vocabulary**: Financial transaction metrics (`transaction_amount_paisa_sum`, `db_latency_seconds`) represent a coherent domain model that would be nonsensical if abstracted.

4. **Self-Contained**: The simulator does not couple to or influence the behavior of the correlation/RCA engines, which remain registry-driven.

### Default elements (configurable)

The following default elements are included for simulation fidelity and are used when no configuration is supplied. All of these defaults can be overridden using `simulator-config.yaml` (see `telemetry.service_names` and `telemetry.metric_names`) or via CLI flags where applicable.

#### Metric Names
- `transactions_total`
- `transactions_failed_total`
- `db_ops_total`
- `kafka_produce_total`
- `kafka_consume_total`
- `transaction_latency_seconds`
- `db_latency_seconds`
- `transaction_amount_paisa_sum`
- `transaction_amount_paisa_count`

#### Service Names (defaults)
- `api-gateway`
- `tps` (Transaction Processing Service)
- `postgres` (Database)
- `kafka` (Message queue)

#### Transaction Attributes (defaults)
- `transaction.id`
- `transaction.type` (UPI, CREDIT_CARD, DEBIT_CARD, NETBANKING, WALLET)
- `transaction.status` (SUCCESS, FAILED, PENDING)
- `transaction.amount`
- `transaction.currency`

#### Resource Attributes
- `service.name`
- `service.version`
- `deployment.environment`
- `telemetry.sdk.name`
- `telemetry.sdk.language`
- `telemetry.sdk.version`

### Configuration and migration path

If you need to **customize** the simulator's domain model (preferred):

1. Edit the default `simulator-config.yaml` in this repository (or create your own) to override `telemetry.service_names`, `telemetry.metric_names`, or `telemetry.outputs`.
2. Start the simulator using `--config ./simulator-config.yaml` (or point it at your config path).
3. The simulator will read your config and generate telemetry using the configured names and failure schedules. When `telemetry.outputs` includes `stdout` the simulator will also print telemetry to stdout in a readable format.

This is **not required** for the current use case but provides a path forward if simulation needs diversify.

### Relationship to Mirador Core

**Important**: The correlation and RCA engines in `internal/services/correlation_engine.go` and `internal/rca/` **must never** hardcode these metric/service names. Engines discover KPIs via:

- Stage-00 KPI registry (`internal/repo/kpi_repo.go`)
- EngineConfig (`internal/config/config.go`)
- Dynamic service graph discovery

The simulator merely **produces** data that matches patterns the engines **discover** at runtime.

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

### Configuration

Environment variables:

Telemetry endpoint
- `telemetry.endpoint`: OTLP collector endpoint (default: `http://localhost:4317` when not set in config)
- `telemetry.insecure`: whether to use an insecure (no-TLS) OTLP connection (default: true)
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

### Failure Scenarios

The simulator can inject realistic failure patterns:

```bash
# Inject database latency spike
INJECT_DB_LATENCY=true ./bin/otel-fintrans-simulator

# Inject Kafka producer failures
INJECT_KAFKA_FAILURES=true ./bin/otel-fintrans-simulator

# Inject cascading failures
INJECT_CASCADING_FAILURES=true ./bin/otel-fintrans-simulator
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
1. Discover `transactions_failed_total` via KPI registry (not hardcoded!)
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
2. **Don't pollute engines**: Never copy simulator hardcoded names into `internal/services/correlation_engine.go`
3. **Update this README**: Document any new hardcoded attributes/metrics
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


We'll take these up one at a time — the already completed items (4 and 7) are marked above. Tell me which remaining item you'd like me to start next and I’ll open a focused PR/branch for it.
