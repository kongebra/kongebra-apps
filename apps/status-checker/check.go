package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

type Status string

const (
	StatusUp      Status = "up"
	StatusDown    Status = "down"
	StatusUnknown Status = "unknown"
)

// Result er siste sjekk-resultat for en target. Pekere = nullable i JSON.
type Result struct {
	Name        string     `json:"name"`
	URL         string     `json:"url"`
	Status      Status     `json:"status"`
	LatencyMs   *int64     `json:"latency_ms"`
	HTTPCode    *int       `json:"http_code"`
	Reason      *string    `json:"reason"`
	LastChecked *time.Time `json:"last_checked"`
}

// newHTTPClient bygger EN delt klient. Timeout styres per-sjekk via context (ikke Client.Timeout).
// Eksplisitt Transport: default MaxIdleConnsPerHost=2 serialiserer parallelle sjekker mot samme host.
func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
		},
	}
}

// check utfører en enkelt GET. Følger redirects (default). Endelig 2xx = up.
func check(ctx context.Context, client *http.Client, t Target) Result {
	res := Result{Name: t.Name, URL: t.URL}
	now := time.Now()
	res.LastChecked = &now

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL+t.HealthPath, nil)
	if err != nil {
		res.Status, res.Reason = StatusDown, ptr("other")
		return res
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		res.Status = StatusDown
		res.Reason = ptr(classifyErr(err))
		return res
	}
	defer func() {
		io.Copy(io.Discard, resp.Body) // drain for connection-reuse
		resp.Body.Close()
	}()

	code := resp.StatusCode
	res.HTTPCode = &code
	lat := time.Since(start).Milliseconds()

	if code >= 200 && code < 300 {
		res.Status = StatusUp
		res.LatencyMs = &lat
	} else {
		res.Status = StatusDown
		if code >= 500 {
			res.Reason = ptr("http_5xx")
		} else if code >= 400 {
			res.Reason = ptr("http_4xx")
		} else {
			res.Reason = ptr("other")
		}
	}
	return res
}

// classifyErr mapper en transport-feil til en reason-streng.
func classifyErr(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return "conn_refused"
	case strings.Contains(msg, "tls"), strings.Contains(msg, "certificate"):
		return "tls_error"
	case strings.Contains(msg, "no such host"), strings.Contains(msg, "dns"):
		return "dns_error"
	default:
		return "other"
	}
}

func ptr[T any](v T) *T { return &v }
