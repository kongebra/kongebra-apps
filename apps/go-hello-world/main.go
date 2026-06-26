package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// version settes ved build via -ldflags "-X main.version=<git-sha>", env VERSION overstyrer.
var version = "dev"

// logRequest logger metode, path og remote til stdout for hver request.
func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

// setupOTel konfigurerer traces + metrics via OTLP HTTP. No-op hvis
// OTEL_EXPORTER_OTLP_ENDPOINT mangler, så lokal kjøring uten collector funker.
func setupOTel(ctx context.Context, v string) (func(context.Context) error, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return func(context.Context) error { return nil }, nil
	}

	host, _ := os.Hostname() // container-ID i swarm
	node := os.Getenv("NODE_HOSTNAME")
	if node == "" {
		node = host // lokal fallback: ingen swarm-node
	}
	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceName("go-hello-world"),
		semconv.ServiceVersion(v),
		semconv.HostName(node),    // swarm-node (host-maskin) = OTEL host.name
		semconv.ContainerID(host), // container/replica
	))
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

func main() {
	v := version // baket inn ved build
	if env := os.Getenv("VERSION"); env != "" {
		v = env // Dokploy env overstyrer
	}

	ctx := context.Background()
	shutdownOTel, err := setupOTel(ctx, v)
	if err != nil {
		log.Fatalf("otel setup: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Hello World")
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"version":%q}`+"\n", v)
	})
	mux.HandleFunc("/whoami", func(w http.ResponseWriter, r *http.Request) {
		host, _ := os.Hostname()           // container-hostname = replica-identitet
		node := os.Getenv("NODE_HOSTNAME") // valgfri swarm-template env, ellers tom
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"host":%q,"node":%q,"version":%q}`+"\n", host, node, v)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080" // ponytail: dokploy injiserer PORT, 8080 er fallback lokalt
	}

	// otelhttp gir auto server-spans + http.server-metrics; span navngis per rute.
	handler := otelhttp.NewHandler(logRequest(mux), "server",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)
	srv := &http.Server{Addr: ":" + port, Handler: handler}

	go func() {
		log.Printf("listening on :%s (version=%s)", port, v)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	// graceful shutdown: swarm sender SIGTERM ved stopp/rolling update.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop
	log.Println("shutting down")

	shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shCtx)
	_ = shutdownOTel(shCtx)
}
