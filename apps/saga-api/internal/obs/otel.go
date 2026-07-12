// Package obs wires OpenTelemetry traces and metrics for saga-api into the
// lab's otel-lgtm collector. Setup is disabled (a clean no-op) when no
// endpoint is configured, so local dev and tests never depend on a running
// collector; the returned instruments still work in that case (they record
// against the otel no-op providers), so callers never need to branch on
// whether OTel is enabled.
package obs

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
)

// instrumentationName scopes the tracer/meter to this service; it shows up
// in Tempo/Prometheus as the instrumentation library name.
const instrumentationName = "saga-api"

// Tracer is the package-level tracer the worker uses to start job spans. It
// is assigned once by Setup (real or no-op tracer, depending on whether an
// endpoint is configured) and read thereafter - Setup runs once at boot,
// before the worker starts claiming jobs.
var Tracer trace.Tracer

// Metrics are the per-job instruments, created once at boot and recorded by
// the worker on every job completion. Never recreate these per job - the
// SDK returns the same instrument for repeat calls with the same name
// anyway, but recreating them would just waste allocations.
type Metrics struct {
	SummaryDuration   metric.Float64Histogram
	TokensPerSecond   metric.Float64Histogram
	TranslateDuration metric.Float64Histogram
	TranscriptChars   metric.Float64Histogram
	Chunks            metric.Float64Histogram
	CostUSD           metric.Float64Histogram
	JobsTotal         metric.Int64Counter
}

// Met holds the instruments created by Setup. Populated once at boot.
var Met Metrics

// init populates Tracer and Met against the otel package's built-in no-op
// providers, so any code that starts spans or records measurements (the
// worker, and its tests) works before - or even without ever calling -
// Setup. Setup replaces both with the real SDK providers when an endpoint
// is configured; the instrument names/units are static literals here, so
// newMetrics erroring would be a programmer error caught by any test that
// imports this package.
func init() {
	Tracer = otel.Tracer(instrumentationName)
	if err := newMetrics(otel.Meter(instrumentationName)); err != nil {
		panic(fmt.Sprintf("obs: instrument setup: %v", err))
	}
}

// Setup configures the global OTel tracer/meter providers and returns a
// shutdown func that flushes both. When endpoint is empty, OTel stays on the
// no-op providers init already set up, and shutdown is a no-op that always
// returns nil - the disabled path must never fail boot.
func Setup(ctx context.Context, endpoint, serviceVersion string) (func(context.Context) error, error) {
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	res := resource.NewSchemaless(
		semconv.ServiceName(instrumentationName),
		semconv.ServiceVersion(serviceVersion),
	)

	traceExp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, fmt.Errorf("obs: trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	Tracer = tp.Tracer(instrumentationName)

	metricExp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, fmt.Errorf("obs: metric exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)
	if err := newMetrics(mp.Meter(instrumentationName)); err != nil {
		return nil, err
	}

	shutdown := func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}
	return shutdown, nil
}

// newMetrics creates every instrument once and stores them in Met. Errors
// only occur on malformed instrument names/units, which is a programmer
// error, not a runtime condition - callers surface it as a Setup failure.
func newMetrics(m metric.Meter) error {
	var errs []error
	var err error

	Met.SummaryDuration, err = m.Float64Histogram("saga.summary.duration",
		metric.WithDescription("Summarize pass wall-clock duration"), metric.WithUnit("ms"))
	errs = append(errs, err)

	Met.TokensPerSecond, err = m.Float64Histogram("saga.summary.tokens_per_second",
		metric.WithDescription("Output token generation rate for the summarize pass"), metric.WithUnit("{tokens}/s"))
	errs = append(errs, err)

	Met.TranslateDuration, err = m.Float64Histogram("saga.translate.duration",
		metric.WithDescription("Translate pass wall-clock duration"), metric.WithUnit("ms"))
	errs = append(errs, err)

	Met.TranscriptChars, err = m.Float64Histogram("saga.transcript.chars",
		metric.WithDescription("Transcript length in characters"), metric.WithUnit("{chars}"))
	errs = append(errs, err)

	Met.Chunks, err = m.Float64Histogram("saga.chunks",
		metric.WithDescription("Number of map-reduce chunks the transcript was split into"), metric.WithUnit("{chunks}"))
	errs = append(errs, err)

	Met.CostUSD, err = m.Float64Histogram("saga.cost_usd",
		metric.WithDescription("Total modeled cost of a job run"), metric.WithUnit("{USD}"))
	errs = append(errs, err)

	Met.JobsTotal, err = m.Int64Counter("saga.jobs_total",
		metric.WithDescription("Jobs reaching a terminal state"), metric.WithUnit("{job}"))
	errs = append(errs, err)

	return errors.Join(errs...)
}
