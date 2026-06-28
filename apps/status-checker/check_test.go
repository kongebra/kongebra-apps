package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheckUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	res := check(context.Background(), newHTTPClient(), Target{Name: "x", URL: srv.URL, HealthPath: "/health"})
	if res.Status != StatusUp {
		t.Fatalf("status = %q, vil ha up", res.Status)
	}
	if res.LatencyMs == nil {
		t.Error("latency skal være satt ved up")
	}
	if res.HTTPCode == nil || *res.HTTPCode != 200 {
		t.Errorf("http_code = %v, vil ha 200", res.HTTPCode)
	}
	if res.Reason != nil {
		t.Errorf("reason skal være nil ved up, var %v", *res.Reason)
	}
}

func TestCheckDown5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	res := check(context.Background(), newHTTPClient(), Target{Name: "x", URL: srv.URL, HealthPath: "/"})
	if res.Status != StatusDown {
		t.Fatalf("status = %q, vil ha down", res.Status)
	}
	if res.Reason == nil || *res.Reason != "http_5xx" {
		t.Errorf("reason = %v, vil ha http_5xx", res.Reason)
	}
	if res.LatencyMs != nil {
		t.Error("latency skal være nil ved down")
	}
}

func TestCheckDownTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	res := check(ctx, newHTTPClient(), Target{Name: "x", URL: srv.URL, HealthPath: "/"})
	if res.Status != StatusDown {
		t.Fatalf("status = %q, vil ha down", res.Status)
	}
	if res.Reason == nil || *res.Reason != "timeout" {
		t.Errorf("reason = %v, vil ha timeout", res.Reason)
	}
}

func TestCheckRedirectFollowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redir" {
			http.Redirect(w, r, "/final", http.StatusMovedPermanently)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	res := check(context.Background(), newHTTPClient(), Target{Name: "x", URL: srv.URL, HealthPath: "/redir"})
	if res.Status != StatusUp {
		t.Fatalf("status = %q, vil ha up (redirect fulgt)", res.Status)
	}
	if res.HTTPCode == nil || *res.HTTPCode != 200 {
		t.Errorf("http_code = %v, vil ha endelig 200", res.HTTPCode)
	}
}
