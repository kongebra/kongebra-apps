// Package otelsetup gir standard OTel-init for TrønderLeikan-tjenestene:
// traces + metrics via OTLP/HTTP. Endpoint leses av SDK-en fra standard-envene
// (OTEL_EXPORTER_OTLP_ENDPOINT m.fl.) - i clusteret peker den på alloy,
// lokalt på otel-lgtm (SPEC §11).
//
// ponytail: kun traces + metrics. Logger går til stdout og plukkes opp av
// cluster-stacken; OTLP-logger legges til her om/når vi vil ha dem korrelert.
package otelsetup

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
)

// Setup initialiserer globale tracer- og meter-providers med OTLP/HTTP-export
// og W3C-propagering. Returnert shutdown-funksjon flusher og stenger begge -
// kall den ved graceful shutdown (typisk defer i main).
func Setup(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, fmt.Errorf("build otel resource: %w", err)
	}

	traceExp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create otlp trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)

	metricExp, err := otlpmetrichttp.New(ctx)
	if err != nil {
		// rydd opp trace-provideren vi allerede har laget
		_ = tp.Shutdown(ctx)
		return nil, fmt.Errorf("create otlp metric exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
		sdkmetric.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}, nil
}
