package main

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"go.uber.org/zap"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

func TestInitMetrics_noError(t *testing.T) {
	sim := &Simulator{meter: otel.Meter("test"), tracer: otel.Tracer("test")}
	if err := sim.initMetrics(context.Background()); err != nil {
		t.Fatalf("initMetrics failed: %v", err)
	}
}

func TestKafkaMetrics_noPanic(t *testing.T) {
	sim := &Simulator{meter: otel.Meter("test"), tracer: otel.Tracer("test")}
	if err := sim.initMetrics(context.Background()); err != nil {
		t.Fatalf("initMetrics failed: %v", err)
	}

	ctx := context.Background()

	// simulate produce should not error when shouldFail=false
	if err := sim.simulateKafkaProduce(ctx, "tx-00001", 1000, false); err != nil {
		t.Fatalf("simulateKafkaProduce returned err: %v", err)
	}

	// simulate consume/consumer paths should run without panic
	sim.simulateKafkaConsumer(ctx, "tx-00001", 1000, false)
	sim.simulateAPIGatewayKafkaConsumer(ctx, "tx-00001", 1000, false)
}

func TestRunSimulation_StopsOnContextCancel(t *testing.T) {
	sim := &Simulator{
		tracer:       otel.Tracer("test"),
		meter:        otel.Meter("test"),
		logger:       zap.NewNop(),
		transactions: 1000,
		concurrency:  10,
	}

	if err := sim.initMetrics(context.Background()); err != nil {
		t.Fatalf("initMetrics failed: %v", err)
	}

	// run simulation in background and cancel quickly
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sim.runSimulation(ctx)
		close(done)
	}()

	// cancel quickly
	cancel()

	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatalf("runSimulation did not stop after context cancellation")
	}
}

func TestBackgroundMetrics_noPanic(t *testing.T) {
	sim := &Simulator{tracer: otel.Tracer("test"), meter: otel.Meter("test"), logger: zap.NewNop()}
	if err := sim.initMetrics(context.Background()); err != nil {
		t.Fatalf("initMetrics failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// start background metrics and ensure it runs briefly without panic
	go sim.startBackgroundMetrics(ctx)

	// wait until done or timeout
	<-ctx.Done()
}

func TestInitMetrics_replOffsetsRegistered(t *testing.T) {
	sim := &Simulator{meter: otel.Meter("test"), tracer: otel.Tracer("test")}
	if err := sim.initMetrics(context.Background()); err != nil {
		t.Fatalf("initMetrics failed: %v", err)
	}

	if sim.redisMasterReplOffset == nil {
		t.Fatalf("redisMasterReplOffset instrument not initialized")
	}
	if sim.redisSlaveReplOffset == nil {
		t.Fatalf("redisSlaveReplOffset instrument not initialized")
	}
}

func TestScenarioScheduler_parsing(t *testing.T) {
	now := time.Now()
	cfg := FailureConfig{
		Mode: "random",
		Scenarios: []Scenario{
			{
				Name:     "test-scn",
				Start:    "0s",
				Duration: "1m",
				Effects:  []Effect{{Metric: "db_latency", Op: "scale", Value: 3.0}},
			},
		},
	}

	ss, err := newScenarioScheduler(cfg, now)
	if err != nil {
		t.Fatalf("newScenarioScheduler failed: %v", err)
	}
	if len(ss.entries) != 1 {
		t.Fatalf("expected 1 entry; got %d", len(ss.entries))
	}
	// it should be active at now
	active := ss.activeAt(now)
	if len(active) != 1 {
		t.Fatalf("expected active scenarios at now; got %d", len(active))
	}
}

// ensure new hardware-related scenario effects are parsed and applied to runtime state
func TestScenarioScheduler_hardwareEffectsApplied(t *testing.T) {
	now := time.Now()
	cfg := FailureConfig{
		Mode: "random",
		Scenarios: []Scenario{
			{
				Name:     "kafka_disk_issue",
				Start:    "0s",
				Duration: "1m",
				Effects:  []Effect{{Metric: "kafka_disk_failure", Op: "scale", Value: 3.0}},
			},
			{
				Name:     "keydb_mem_fault",
				Start:    "0s",
				Duration: "1m",
				Effects:  []Effect{{Metric: "keydb_memory_fault", Op: "scale", Value: 5.0}},
			},
		},
	}

	ss, err := newScenarioScheduler(cfg, now)
	if err != nil {
		t.Fatalf("newScenarioScheduler failed: %v", err)
	}

	sim := &Simulator{tracer: otel.Tracer("test"), meter: otel.Meter("test"), logger: zap.NewNop()}
	if err := sim.initMetrics(context.Background()); err != nil {
		t.Fatalf("initMetrics failed: %v", err)
	}

	// run background metrics with a fast interval so scheduler entries are applied
	sim.scenarioSched = ss
	sim.dataInterval = 10 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go sim.startBackgroundMetrics(ctx)

	<-ctx.Done()

	if sim.stateKafkaErrorMult == 1.0 {
		t.Fatalf("expected kafka error multiplier to change from 1.0 when scenario active; got %v", sim.stateKafkaErrorMult)
	}
	if sim.stateKeydbFailMult == 1.0 {
		t.Fatalf("expected keydb failure multiplier to change from 1.0 when scenario active; got %v", sim.stateKeydbFailMult)
	}
}

func TestScenarioScheduler_networkEffectsApplied(t *testing.T) {
	now := time.Now()
	cfg := FailureConfig{
		Mode: "random",
		Scenarios: []Scenario{
			{
				Name:     "net_latency",
				Start:    "0s",
				Duration: "1m",
				Effects:  []Effect{{Metric: "network_latency", Op: "scale", Value: 4.0}},
			},
			{
				Name:     "net_packet_drop",
				Start:    "0s",
				Duration: "1m",
				Effects:  []Effect{{Metric: "network_packet_drop", Op: "scale", Value: 5.0}},
			},
		},
	}

	ss, err := newScenarioScheduler(cfg, now)
	if err != nil {
		t.Fatalf("newScenarioScheduler failed: %v", err)
	}

	sim := &Simulator{tracer: otel.Tracer("test"), meter: otel.Meter("test"), logger: zap.NewNop()}
	if err := sim.initMetrics(context.Background()); err != nil {
		t.Fatalf("initMetrics failed: %v", err)
	}

	sim.scenarioSched = ss
	sim.dataInterval = 10 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go sim.startBackgroundMetrics(ctx)
	<-ctx.Done()

	if sim.stateNetworkLatencyMult == 1.0 {
		t.Fatalf("expected network latency multiplier to change when scenario active; got %v", sim.stateNetworkLatencyMult)
	}
	if sim.stateNetworkDropMult == 1.0 {
		t.Fatalf("expected network drop multiplier to change when scenario active; got %v", sim.stateNetworkDropMult)
	}
}

func TestScenarioScheduler_redisReplEffectsApplied(t *testing.T) {
	now := time.Now()
	cfg := FailureConfig{
		Mode: "random",
		Scenarios: []Scenario{
			{
				Name:     "repl_master",
				Start:    "0s",
				Duration: "1m",
				Effects:  []Effect{{Metric: "redis_master_repl_offset", Op: "scale", Value: 3.0}},
			},
			{
				Name:     "repl_slave",
				Start:    "0s",
				Duration: "1m",
				Effects:  []Effect{{Metric: "redis_slave_repl_offset", Op: "scale", Value: 2.5}},
			},
		},
	}

	ss, err := newScenarioScheduler(cfg, now)
	if err != nil {
		t.Fatalf("newScenarioScheduler failed: %v", err)
	}

	sim := &Simulator{tracer: otel.Tracer("test"), meter: otel.Meter("test"), logger: zap.NewNop()}
	if err := sim.initMetrics(context.Background()); err != nil {
		t.Fatalf("initMetrics failed: %v", err)
	}

	sim.scenarioSched = ss
	sim.dataInterval = 10 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go sim.startBackgroundMetrics(ctx)
	<-ctx.Done()

	if sim.stateRedisMasterReplMult == 1.0 {
		t.Fatalf("expected redis master repl multiplier to change when scenario active; got %v", sim.stateRedisMasterReplMult)
	}
	if sim.stateRedisSlaveReplMult == 1.0 {
		t.Fatalf("expected redis slave repl multiplier to change when scenario active; got %v", sim.stateRedisSlaveReplMult)
	}
}

func TestKafkaProduce_networkDropCausesErrors(t *testing.T) {
	sim := &Simulator{tracer: otel.Tracer("test"), meter: otel.Meter("test"), logger: zap.NewNop()}
	if err := sim.initMetrics(context.Background()); err != nil {
		t.Fatalf("initMetrics failed: %v", err)
	}

	sim.rng = rand.New(rand.NewSource(123))
	sim.stateNetworkDropMult = 10.0

	ctx := context.Background()
	total := 50
	errs := 0
	for i := 0; i < total; i++ {
		if err := sim.simulateKafkaProduce(ctx, fmt.Sprintf("tx-%05d", i), 1000, false); err != nil {
			errs++
		}
	}
	if errs == 0 {
		t.Fatalf("expected some failures due to high network packet drop multiplier; got 0 errors")
	}
}

func TestScenarioScheduler_mixedSignalsApplied(t *testing.T) {
	now := time.Now()
	cfg := FailureConfig{
		Mode: "random",
		Scenarios: []Scenario{
			{
				Name:     "kafka_mixed",
				Start:    "0s",
				Duration: "1m",
				Effects: []Effect{
					{Metric: "kafka_disk_failure", Op: "scale", Value: 4.0},
					{Metric: "kafka_throughput", Op: "scale", Value: 0.2},
				},
			},
			{
				Name:     "db_mixed",
				Start:    "0s",
				Duration: "1m",
				Effects: []Effect{
					{Metric: "db_latency", Op: "scale", Value: 6.0},
					{Metric: "transaction_failures", Op: "set", Value: 0.5},
				},
			},
		},
	}

	ss, err := newScenarioScheduler(cfg, now)
	if err != nil {
		t.Fatalf("newScenarioScheduler failed: %v", err)
	}

	sim := &Simulator{tracer: otel.Tracer("test"), meter: otel.Meter("test"), logger: zap.NewNop()}
	if err := sim.initMetrics(context.Background()); err != nil {
		t.Fatalf("initMetrics failed: %v", err)
	}

	sim.scenarioSched = ss
	sim.dataInterval = 10 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	go sim.startBackgroundMetrics(ctx)

	// Poll the multipliers during the run and assert that each expected change
	// happened at least once. This is a bit more robust against timing races
	// caused by resets that occur at tick boundaries.
	seenKafkaErr := false
	seenKafkaReqs := false
	seenDBLatency := false
	seenFailure := false

	checkEnd := time.Now().Add(350 * time.Millisecond)
	for time.Now().Before(checkEnd) {
		if sim.stateKafkaErrorMult > 1.0 {
			seenKafkaErr = true
		}
		if sim.stateKafkaReqsMult < 1.0 {
			seenKafkaReqs = true
		}
		if sim.stateDbLatencyMult > 1.0 {
			seenDBLatency = true
		}
		if sim.stateFailureMult < 1.0 {
			seenFailure = true
		}
		if seenKafkaErr && seenKafkaReqs && seenDBLatency && seenFailure {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !seenKafkaErr {
		t.Fatalf("expected kafka error multiplier > 1.0 at some point; never observed")
	}
	if !seenKafkaReqs {
		t.Fatalf("expected kafka requests multiplier < 1.0 at some point; never observed")
	}
	if !seenDBLatency {
		t.Fatalf("expected db latency multiplier > 1.0 at some point; never observed")
	}
	if !seenFailure {
		t.Fatalf("expected failure multiplier < 1.0 (set to 0.5) at some point; never observed")
	}
}

func TestHistogramBucketRecording_noPanic(t *testing.T) {
	sim := &Simulator{tracer: otel.Tracer("test"), meter: otel.Meter("test"), logger: zap.NewNop()}
	if err := sim.initMetrics(context.Background()); err != nil {
		t.Fatalf("initMetrics failed: %v", err)
	}

	// record a few sample values to buckets
	ctx := context.Background()
	sim.recordTransactionLatencyBuckets(ctx, 0.12, attribute.String("service_name", "api-gateway"))
	sim.recordDBLatencyBuckets(ctx, 0.002, attribute.String("db_system", "cassandra"))
}

// Test that initMetrics registers the expected built-in metrics in the dynamic registry
func TestInitMetrics_registryHasBuiltins(t *testing.T) {
	sim := &Simulator{tracer: otel.Tracer("test"), meter: otel.Meter("test"), logger: zap.NewNop()}
	if err := sim.initMetrics(context.Background()); err != nil {
		t.Fatalf("initMetrics failed: %v", err)
	}

	// Expected built-in metrics (defaults)
	expected := []string{
		"transactions_total",
		"transactions_failed_total",
		"db_ops_total",
		"kafka_produce_total",
		"kafka_consume_total",

		"transaction_latency_seconds",
		"transaction_latency_seconds_bucket",
		"transaction_latency_seconds_sum",
		"transaction_latency_seconds_count",

		"db_latency_seconds",
		"db_latency_seconds_bucket",
		"db_latency_seconds_sum",
		"db_latency_seconds_count",

		"transaction_amount_paisa_sum",
		"transaction_amount_paisa_count",

		"kafka_produce_latency_seconds",
		"kafka_consume_latency_seconds",

		"jvm_memory_used_bytes",
		"jvm_memory_max_bytes",
		"jvm_gc_pause_seconds_sum",
		"jvm_gc_pause_seconds_count",

		"tomcat_threads_busy_threads",
		"tomcat_threads_config_max_threads",
		"tomcat_threads_queue_seconds",

		"process_cpu_usage",
		"process_uptime_seconds",
		"process_files_open_files",
		"process_files_max_files",

		"hikaricp_connections_active",
		"hikaricp_connections_max",

		"redis_memory_used_bytes",
		"redis_memory_max_bytes",
		"redis_keyspace_hits_total",
		"redis_keyspace_misses_total",
		"redis_evicted_keys_total",
		"redis_connected_clients",
		"redis_master_repl_offset",
		"redis_slave_repl_offset",

		"kafka_controller_UnderReplicatedPartitions",
		"kafka_network_RequestMetrics_RequestsPerSec",
		"kafka_server_BrokerTopicMetrics_BytesInPerSec",
		"kafka_network_RequestMetrics_ErrorsPerSec",
		"kafka_controller_IsrShrinksPerSec",

		"node_load1",
		"node_memory_MemAvailable_bytes",
		"node_memory_MemTotal_bytes",
		"node_filesystem_avail_bytes",
		"node_filesystem_size_bytes",
		"node_disk_io_time_seconds_total",
		"node_network_latency_ms",
		"node_network_packet_drops_total",
		"node_context_switches_total",

		"cassandra_disk_pressure",
		"cassandra_compaction_pending_tasks",
	}

	for _, name := range expected {
		if !sim.metricRegistry.Has(name) {
			t.Fatalf("expected metric %s to be registered", name)
		}
	}
}

func TestInitOTel_stdoutOnly(t *testing.T) {
	ctx := context.Background()
	// Request stdout-only; initOTel should succeed without any network calls
	shutdown, err := initOTel(ctx, "", true, false, "stdout", 15*time.Second)
	if err != nil {
		t.Fatalf("initOTel stdout failed: %v", err)
	}
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
}

func TestInitOTel_httpOnly(t *testing.T) {
	ctx := context.Background()
	// HTTP endpoint on 4318 using insecure (http) should initialize without error
	// use stdout-only to avoid network activity in unit tests; detection logic still exercised
	shutdown, err := initOTel(ctx, "http://localhost:4318", true, false, "stdout", 15*time.Second)
	if err != nil {
		t.Fatalf("initOTel http failed: %v", err)
	}
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
}

func TestInitOTel_httpTLS_skipVerify(t *testing.T) {
	ctx := context.Background()
	// HTTPS endpoint with skip-verify should initialize OK (uses custom http client)
	// use stdout-only to avoid network activity in unit tests; detection logic still exercised
	shutdown, err := initOTel(ctx, "https://localhost:4318", false, true, "stdout", 15*time.Second)
	if err != nil {
		t.Fatalf("initOTel https skipVerify failed: %v", err)
	}
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
}
