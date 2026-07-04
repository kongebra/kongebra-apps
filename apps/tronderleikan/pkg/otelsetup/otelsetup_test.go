package otelsetup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
)

// Røyktest: Setup lykkes, providers settes globalt, og shutdown flusher
// mot en falsk OTLP-endpoint uten feil.
func TestSetupAndShutdown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // tomt proto-svar er gyldig OTLP-respons
	}))
	defer srv.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", srv.URL)

	shutdown, err := Setup(context.Background(), "test-service")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if otel.GetTracerProvider() == nil || otel.GetMeterProvider() == nil {
		t.Fatal("globale providers ikke satt")
	}
	// litt aktivitet så shutdown faktisk har noe å flushe
	_, span := otel.Tracer("test").Start(context.Background(), "op")
	span.End()

	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}
