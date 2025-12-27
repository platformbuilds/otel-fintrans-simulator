package main

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
)

func TestMetricRegistry_RegisterAndRecord(t *testing.T) {
	sim := &Simulator{tracer: otel.Tracer("test"), meter: otel.Meter("test")}
	sim.telemCfg.DynamicMetrics = []DynamicMetricConfig{
		{Name: "test_dynamic_gauge", Type: "gauge", DataType: "float", Description: "test gauge"},
		{Name: "test_dynamic_hist", Type: "histogram", DataType: "float", Description: "test hist"},
		{Name: "test_dynamic_int", Type: "gauge", DataType: "int", Description: "test int gauge"},
	}

	if err := sim.initMetrics(context.Background()); err != nil {
		t.Fatalf("initMetrics failed: %v", err)
	}

	if sim.metricRegistry == nil {
		t.Fatalf("expected metric registry to be initialized")
	}

	if !sim.metricRegistry.Has("test_dynamic_gauge") {
		t.Fatalf("expected registry to have test_dynamic_gauge")
	}

	if err := sim.metricRegistry.AddFloat(context.Background(), "test_dynamic_gauge", 0.5); err != nil {
		t.Fatalf("AddFloat failed: %v", err)
	}

	if err := sim.metricRegistry.RecordFloat(context.Background(), "test_dynamic_hist", 0.1); err != nil {
		t.Fatalf("RecordFloat failed: %v", err)
	}

	if err := sim.metricRegistry.AddInt(context.Background(), "test_dynamic_int", 3); err != nil {
		t.Fatalf("AddInt failed: %v", err)
	}
}
