package main

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	"go.opentelemetry.io/otel"
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

func TestInitOTel_stdoutOnly(t *testing.T) {
	ctx := context.Background()
	// Request stdout-only; initOTel should succeed without any network calls
	shutdown, err := initOTel(ctx, "", true, "stdout")
	if err != nil {
		t.Fatalf("initOTel stdout failed: %v", err)
	}
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
}
