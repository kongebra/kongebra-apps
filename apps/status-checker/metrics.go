package main

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type recordFn func([]Result)

// setupMetrics registrerer per-target up/down-gauge + latency-histogram på global meter.
// Meter-provideren settes av setupOTel; uten OTLP-endpoint er det en no-op-provider.
func setupMetrics() (recordFn, error) {
	m := otel.Meter("status-checker")
	upGauge, err := m.Int64Gauge("target_up")
	if err != nil {
		return nil, err
	}
	latHist, err := m.Float64Histogram("target_latency_ms")
	if err != nil {
		return nil, err
	}
	return func(results []Result) {
		ctx := context.Background()
		for _, r := range results {
			attrs := metric.WithAttributes(attribute.String("name", r.Name))
			up := int64(0)
			if r.Status == StatusUp {
				up = 1
			}
			upGauge.Record(ctx, up, attrs)
			if r.LatencyMs != nil {
				latHist.Record(ctx, float64(*r.LatencyMs), attrs)
			}
		}
	}, nil
}
