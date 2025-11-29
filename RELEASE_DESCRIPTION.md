# Release <release_tag> — OpenTelemetry Financial Transaction Simulator

A configuration-driven simulator that generates realistic OpenTelemetry metrics, traces and logs for a financial transaction processing system.

## ✅ Highlights
- Configuration-driven simulator: telemetry outputs and failure scenarios are controlled via `simulator-config.yaml`.
- Supports OTLP/gRPC (4317) and OTLP/HTTP (4318), with TLS/insecure and skip-tls-verify options.
- Helpful configuration validation warns about common issues (e.g., using `http://` on port 4317).
- Formatted, repeatable runs with deterministic RNG seeding and graceful shutdown.

## 📡 Telemetry & instrumentation
- Monotonic counters for totals (transactions, DB ops).
- Separate histograms for Kafka produce/consume latency and DB latency.
- Traces, metrics and logs supported with:
  - `telemetry.outputs` — `otlp`, `stdout`, or `both`
  - `telemetry.endpoint`, `telemetry.insecure`, `telemetry.skip_tls_verify`

## 🧭 Runtime improvements
- Graceful shutdown on SIGINT/SIGTERM with cancellable context.
- Deterministic runs via `--rand-seed` or `failure.seed` in YAML.
- `--log-output` to control logging behaviour (stdout/no-op).

## ⚙️ Configuration & examples
- Example config provided: `simulator-config.yaml` (canonical gRPC form `localhost:4317` and OTLP/HTTP example `http://localhost:4318`).
- Config & examples included in the repo make local development and demos straightforward.

## 🧪 Testing, CI & Release
- Unit tests expanded for telemetry init, validation, scheduler and cancellation.
- CI workflow runs `go test`, `gofmt`, `go vet`.
- Manual release workflow available in Actions to build, publish container images and create a GitHub Release.

## 🐳 Release artifacts & image names
- Built binary uploaded to release:
  - `otel-fintrans-simulator-<release_tag>-<platform>`
  - Example: `otel-fintrans-simulator-v1.0.0-amd64`
- Container images:
  - GHCR: `ghcr.io/platformbuilds/otel-fintrans-simulator:<release_tag>-<platform>`
  - Docker Hub: `docker.io/platformbuilds/otel-fintrans-simulator:<release_tag>-<platform>`
  - Example: `platformbuilds/otel-fintrans-simulator:v1.0.0-amd64`
- Docker Hub pushes are automatic (when `DOCKERHUB_USERNAME` & `DOCKERHUB_TOKEN` are present in repo secrets). GHCR uses `GITHUB_TOKEN`.

## ⚠️ Known or notable changes
- Example YAML normalized to avoid confusing OTLP/gRPC vs OTLP/HTTP (no `http://` on port 4317).
- Validation will warn on inconsistent telemetry settings (http vs https vs insecure vs skip verification).

## 🔭 Suggested next steps
- Add automatic tag-triggered releases (create release on tag push) for fully automated publishing.
- Add multi-arch builds (multi-platform images) if you need both amd64 & arm64 in the same release.
- Add a short "Common misconfigurations" section in the README linking the validation rules.

---

Replace `<release_tag>` and `<platform>` with the actual values (for example: `v1.0.0` and `amd64`) when pasting into the GitHub Release body.
