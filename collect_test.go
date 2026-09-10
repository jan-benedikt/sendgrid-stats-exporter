package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNormalizeAPIBase(t *testing.T) {
	if got := normalizeAPIBase("https://api.eu.sendgrid.com/"); got != "https://api.eu.sendgrid.com" {
		t.Fatalf("normalizeAPIBase() = %q", got)
	}
}

func TestFirstMetrics(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if _, err := firstMetrics(nil); err == nil {
			t.Fatal("expected error")
		}
		if _, err := firstMetrics([]*Statistics{}); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("nil metrics", func(t *testing.T) {
		if _, err := firstMetrics([]*Statistics{{Stats: []*Stat{{Metrics: nil}}}}); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("ok", func(t *testing.T) {
		m, err := firstMetrics([]*Statistics{{
			Stats: []*Stat{{Metrics: &Metrics{Delivered: 7}}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if m.Delivered != 7 {
			t.Fatalf("Delivered = %d", m.Delivered)
		}
	})
}

func TestTruncateBody(t *testing.T) {
	if got := truncateBody([]byte("  boom  ")); got != "boom" {
		t.Fatalf("short = %q", got)
	}
	long := truncateBody([]byte(strings.Repeat("a", maxErrorBodyBytes+10)))
	if !strings.HasSuffix(long, "...") {
		t.Fatalf("expected truncated body, got %q", long)
	}
	if len(long) != maxErrorBodyBytes+3 {
		t.Fatalf("long length = %d", len(long))
	}
}

func TestCollectSuccessAndCache(t *testing.T) {
	var statsHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/v3/stats"):
			statsHits++
			if statsHits == 1 {
				_ = json.NewEncoder(w).Encode([]*Statistics{{
					Date:  "2026-09-09",
					Stats: []*Stat{{Metrics: &Metrics{Requests: 11, Delivered: 9}}},
				}})
				return
			}
			http.Error(w, "rate limited", http.StatusTooManyRequests)
		case strings.HasSuffix(r.URL.Path, "/v3/user/credits"):
			_ = json.NewEncoder(w).Encode(CreditBalance{Total: 100, Remain: 40, Used: 60, Overage: 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	col := newTestCollector(srv.URL)
	first := gather(t, col)
	assertGauge(t, first, "sendgrid_up", 1)
	assertGauge(t, first, "sendgrid_requests", 11)
	assertGauge(t, first, "sendgrid_delivered", 9)
	assertGauge(t, first, "sendgrid_credit_total", 100)

	second := gather(t, col)
	assertGauge(t, second, "sendgrid_up", 0)
	assertGauge(t, second, "sendgrid_requests", 11)
	assertGauge(t, second, "sendgrid_credit_total", 100)
}

func TestCollectEmptyStatistics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/v3/stats") {
			_, _ = io.WriteString(w, "[]")
			return
		}
		_ = json.NewEncoder(w).Encode(CreditBalance{})
	}))
	defer srv.Close()

	metrics := gather(t, newTestCollector(srv.URL))
	assertGauge(t, metrics, "sendgrid_up", 0)
	if _, ok := metrics["sendgrid_requests"]; ok {
		t.Fatal("requests should be omitted when stats are empty and there is no cache")
	}
}

func TestCollectForbiddenIncludesStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()

	metrics := gather(t, newTestCollector(srv.URL))
	assertGauge(t, metrics, "sendgrid_up", 0)
}

func newTestCollector(apiBase string) *Collector {
	return newCollector(slog.New(slog.NewTextHandler(io.Discard, nil)), collectorConfig{
		userName:    "acct",
		apiBase:     apiBase,
		apiKey:      "test-key",
		httpTimeout: 2 * time.Second,
	}, nil)
}

func gather(t *testing.T, col prometheus.Collector) map[string]float64 {
	t.Helper()
	reg := prometheus.NewPedanticRegistry()
	if err := reg.Register(col); err != nil {
		t.Fatal(err)
	}
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}

	out := map[string]float64{}
	for _, mf := range mfs {
		for _, m := range mf.GetMetric() {
			out[mf.GetName()] = m.GetGauge().GetValue()
		}
	}
	return out
}

func assertGauge(t *testing.T, metrics map[string]float64, name string, want float64) {
	t.Helper()
	got, ok := metrics[name]
	if !ok {
		t.Fatalf("missing metric %s in %v", name, metrics)
	}
	if got != want {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}
