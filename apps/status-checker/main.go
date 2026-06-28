package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const (
	checkInterval = 30 * time.Second // ponytail: hardkodet, én operatør; env hvis det trengs
	checkTimeout  = 5 * time.Second  // ponytail: per-sjekk timeout
)

// version settes ved build via -ldflags "-X main.version=<git-sha>", env VERSION overstyrer.
var version = "dev"

// setupOTel konfigurerer traces + metrics via OTLP HTTP. No-op hvis
// OTEL_EXPORTER_OTLP_ENDPOINT mangler, så lokal kjøring uten collector funker.
func setupOTel(ctx context.Context, v string) (func(context.Context) error, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return func(context.Context) error { return nil }, nil
	}

	host, _ := os.Hostname() // k8s: pod-hostname = pod-navn (metadata.name) by default
	attrs := []attribute.KeyValue{
		semconv.ServiceName("status-checker"),
		semconv.ServiceVersion(v),
		semconv.K8SPodName(host),
	}
	// node + namespace via Downward API (spec.nodeName / metadata.namespace), tomme lokalt -> dropp attr
	if node := os.Getenv("NODE_NAME"); node != "" {
		attrs = append(attrs, semconv.K8SNodeName(node))
	}
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		attrs = append(attrs, semconv.K8SNamespaceName(ns))
	}
	res, err := resource.New(ctx, resource.WithAttributes(attrs...))
	if err != nil {
		return nil, err
	}

	traceExp, err := otlptracehttp.New(ctx) // leser OTEL_EXPORTER_OTLP_ENDPOINT
	if err != nil {
		return nil, err
	}
	tp := trace.NewTracerProvider(trace.WithBatcher(traceExp), trace.WithResource(res))
	otel.SetTracerProvider(tp)

	metricExp, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}
	mp := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(metricExp)),
		metric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	return func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}, nil
}

// statusHandler serverer siste snapshot som JSON.
func statusHandler(c *Checker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			CheckedAt string   `json:"checked_at"`
			Services  []Result `json:"services"`
		}{
			CheckedAt: time.Now().UTC().Format(time.RFC3339),
			Services:  c.Snapshot(),
		})
	}
}

func main() {
	v := version // bakt inn ved build
	if env := os.Getenv("VERSION"); env != "" {
		v = env
	}

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		log.Fatal("CONFIG_PATH må settes")
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("config: %v", err) // fail-fast
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	shutdownOTel, err := setupOTel(ctx, v)
	if err != nil {
		log.Fatalf("otel setup: %v", err)
	}

	checker := newChecker(cfg.Targets, checkInterval, checkTimeout)
	go checker.Run(ctx) // poller til ctx cancelleres (SIGTERM)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", statusHandler(checker))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	// otelhttp gir auto server-spans + http.server-metrics; span navngis per rute.
	handler := otelhttp.NewHandler(mux, "server",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)
	srv := &http.Server{Addr: ":" + port, Handler: handler}

	go func() {
		log.Printf("status-checker listening on :%s (%d targets)", port, len(cfg.Targets))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
	shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shCtx)
	_ = shutdownOTel(shCtx)
}
