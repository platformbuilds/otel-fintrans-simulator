# Dynamic Telemetry / Metrics Registry — Design

Purpose
-------
This document describes the dynamic telemetry registry design for the simulator. It enables runtime creation of new metrics from configuration rather than requiring hard-coded instrument variables in Go source.

Goals
-----
- Provide a config-driven way to declare metrics (name, type, numeric type, buckets where needed)
- Create a runtime registry that constructs OTEL instruments based on config and exposes a safe runtime API to record values
- Keep the hot-path performance reasonable by supporting cached references to instruments (optional)
- Provide validation & tests to fail fast for mismatching types

Prometheus metric types (mapping)
---------------------------------
- counter -> OTEL Float64Counter / Int64Counter
- gauge -> OTEL Float64UpDownCounter / Int64UpDownCounter (gauge-like)
- histogram -> OTEL histogram (Record). Requires bucket bounds in config
- summary -> not directly supported; recommend mapping to histogram or rejecting in validation

YAML schema example (telemetry.dynamic_metrics)
------------------------------------------------
telemetry:
  dynamic_metrics:
    - name: cassandra_disk_pressure
      type: gauge
      dataType: float
      description: "0..1 synthetic disk pressure indicator"

    - name: cassandra_compaction_pending_tasks
      type: gauge
      dataType: int
      description: "pending compaction tasks"

    - name: api_request_latency_seconds
      type: histogram
      dataType: float
      description: "API latency distribution"
      buckets: [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0]

Runtime registry API (high level)
--------------------------------
- MetricRegistry: central registry that maps metric name -> instrument handle
- RegisterMetrics(config) -> validates and creates instruments during init
- Lookup(name) -> returns a handle and expected type information
- Helpers/wrappers:
  - RecordFloat(name, value, labels...)
  - AddFloat(name, delta, labels...)
  - AddInt(name, delta, labels...)
  - ObserveHistogram(name, value, labels...)

Validation
----------
- Ensure metric names are valid strings and do not collide with built-ins
- Ensure instrument types are supported and correct bucket config for histograms
- On validation failure simulator startup should fail with a helpful error

Hot-path considerations
-----------------------
- For maximum performance, core, frequently-recorded instruments (e.g. transaction latency histogram) may remain as hard-coded typed fields in the simulator. But under full dynamic mode, the code will perform a map-based lookup.
- To reduce overhead in hot loops, MetricRegistry will expose a GetCached(name) method that returns a small typed struct wrapper to call quickly (no map lookup) — this can be set once at simulation startup for hot sites.

Next steps
----------
1) Implement MetricRegistry, instrument factory and validation
2) Extend config parsing for telemetry.dynamic_metrics
3) Implement wrapper functions and gradually migrate existing recording calls
4) Add tests to validate registry creation and runtime recording
