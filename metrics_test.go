package main

import (
	"context"
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

func TestInitOTel_stdoutOnly(t *testing.T) {
	ctx := context.Background()
	// Request stdout-only; initOTel should succeed without any network calls
	shutdown, err := initOTel(ctx, "", true, false, "stdout")
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
	shutdown, err := initOTel(ctx, "http://localhost:4318", true, false, "stdout")
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
	shutdown, err := initOTel(ctx, "https://localhost:4318", false, true, "stdout")
	if err != nil {
		t.Fatalf("initOTel https skipVerify failed: %v", err)
	}
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
}
