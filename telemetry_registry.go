package main

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel/metric"
)

// MetricRegistry holds dynamically created instruments
type MetricRegistry struct {
	meter   metric.Meter
	mu      sync.RWMutex
	metrics map[string]*metricHandle
}

type metricHandle struct {
	name  string
	mtype string // counter|gauge|histogram
	dtype string // float|int

	// concrete instruments (only one populated depending on type/dtype)
	floatCounter metric.Float64Counter
	intCounter   metric.Int64Counter
	floatUpDown  metric.Float64UpDownCounter
	intUpDown    metric.Int64UpDownCounter
	floatHist    metric.Float64Histogram
}

// NewMetricRegistry creates a registry backed by the provided meter
func NewMetricRegistry(m metric.Meter) *MetricRegistry {
	return &MetricRegistry{meter: m, metrics: make(map[string]*metricHandle)}
}

// Register validates and registers dynamic metrics from configuration
func (r *MetricRegistry) Register(cfgs []DynamicMetricConfig) error {
	if len(cfgs) == 0 {
		return nil
	}
	for _, c := range cfgs {
		if c.Name == "" {
			return errors.New("dynamic metric: name required")
		}
		if c.Type == "" {
			return fmt.Errorf("dynamic metric %s: type required", c.Name)
		}
		dtype := c.DataType
		if dtype == "" {
			dtype = "float"
		}

		// do not override existing metrics in registry
		r.mu.Lock()
		if _, ok := r.metrics[c.Name]; ok {
			r.mu.Unlock()
			return fmt.Errorf("metric %s already registered", c.Name)
		}

		mh := &metricHandle{name: c.Name, mtype: c.Type, dtype: dtype}
		var err error
		switch c.Type {
		case "counter":
			if dtype == "float" {
				mh.floatCounter, err = r.meter.Float64Counter(c.Name, metric.WithDescription(c.Description))
			} else {
				mh.intCounter, err = r.meter.Int64Counter(c.Name, metric.WithDescription(c.Description))
			}
		case "gauge":
			if dtype == "float" {
				mh.floatUpDown, err = r.meter.Float64UpDownCounter(c.Name, metric.WithDescription(c.Description))
			} else {
				mh.intUpDown, err = r.meter.Int64UpDownCounter(c.Name, metric.WithDescription(c.Description))
			}
		case "histogram":
			if dtype != "float" {
				r.mu.Unlock()
				return fmt.Errorf("histogram %s must be float datatype", c.Name)
			}
			mh.floatHist, err = r.meter.Float64Histogram(c.Name, metric.WithDescription(c.Description))
			// note: bucket counters / sum/count exported by prometheus exporter are handled separately
		default:
			r.mu.Unlock()
			return fmt.Errorf("unsupported metric type %s for %s", c.Type, c.Name)
		}

		if err != nil {
			r.mu.Unlock()
			return fmt.Errorf("create instrument %s: %w", c.Name, err)
		}

		r.metrics[c.Name] = mh
		r.mu.Unlock()
	}
	return nil
}

// Has returns true if a metric by that name exists in registry
func (r *MetricRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.metrics[name]
	return ok
}

// Getters for concrete instruments (return nil if not present)
func (r *MetricRegistry) GetFloatCounter(name string) metric.Float64Counter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if mh, ok := r.metrics[name]; ok {
		return mh.floatCounter
	}
	return nil
}

func (r *MetricRegistry) GetIntCounter(name string) metric.Int64Counter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if mh, ok := r.metrics[name]; ok {
		return mh.intCounter
	}
	return nil
}

func (r *MetricRegistry) GetFloatUpDown(name string) metric.Float64UpDownCounter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if mh, ok := r.metrics[name]; ok {
		return mh.floatUpDown
	}
	return nil
}

func (r *MetricRegistry) GetIntUpDown(name string) metric.Int64UpDownCounter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if mh, ok := r.metrics[name]; ok {
		return mh.intUpDown
	}
	return nil
}

func (r *MetricRegistry) GetFloatHistogram(name string) metric.Float64Histogram {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if mh, ok := r.metrics[name]; ok {
		return mh.floatHist
	}
	return nil
}

// AddFloat adds a delta to a named float counter/updown. Returns error if wrong type.
func (r *MetricRegistry) AddFloat(ctx context.Context, name string, delta float64, opts ...metric.AddOption) error {
	r.mu.RLock()
	mh, ok := r.metrics[name]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("metric '%s' not registered", name)
	}
	if mh.floatCounter != nil {
		mh.floatCounter.Add(ctx, delta, opts...)
		return nil
	}
	if mh.floatUpDown != nil {
		mh.floatUpDown.Add(ctx, delta, opts...)
		return nil
	}
	return fmt.Errorf("metric '%s' is not a float counter/gauge", name)
}

// AddInt adds a delta to a named int counter/updown
func (r *MetricRegistry) AddInt(ctx context.Context, name string, delta int64, opts ...metric.AddOption) error {
	r.mu.RLock()
	mh, ok := r.metrics[name]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("metric '%s' not registered", name)
	}
	if mh.intCounter != nil {
		mh.intCounter.Add(ctx, delta, opts...)
		return nil
	}
	if mh.intUpDown != nil {
		mh.intUpDown.Add(ctx, delta, opts...)
		return nil
	}
	return fmt.Errorf("metric '%s' is not an int counter/gauge", name)
}

// RecordFloat records a value for a float histogram
func (r *MetricRegistry) RecordFloat(ctx context.Context, name string, value float64, opts ...metric.RecordOption) error {
	r.mu.RLock()
	mh, ok := r.metrics[name]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("metric '%s' not registered", name)
	}
	if mh.floatHist != nil {
		mh.floatHist.Record(ctx, value, opts...)
		return nil
	}
	return fmt.Errorf("metric '%s' is not a float histogram", name)
}
