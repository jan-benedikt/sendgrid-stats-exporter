package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNormalizeAPIBase(t *testing.T) {
	if got := normalizeAPIBase(" https://api.eu.sendgrid.com/ "); got != "https://api.eu.sendgrid.com" {
		t.Fatalf("normalizeAPIBase() = %q", got)
	}
}

func TestValidateAPIBase(t *testing.T) {
	if err := validateAPIBase("https://api.sendgrid.com"); err != nil {
		t.Fatal(err)
	}
	if err := validateAPIBase("http://127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}
	if err := validateAPIBase(""); err == nil {
		t.Fatal("expected error for empty base")
	}
	if err := validateAPIBase("api.sendgrid.com"); err == nil {
		t.Fatal("expected error for missing scheme")
	}
}

func TestScrapeTimeout(t *testing.T) {
	cfg := collectorConfig{httpTimeout: 10 * time.Second}
	if got := cfg.scrapeTimeout(); got != 10*time.Second {
		t.Fatalf("default scrapeTimeout = %s", got)
	}
	cfg.includeSubusers = true
	if got := cfg.scrapeTimeout(); got != 30*time.Second {
		t.Fatalf("subuser scrapeTimeout = %s", got)
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

func TestCollectSubusersMonthly(t *testing.T) {
	origStatsPage := subuserStatsPageSize
	origListPage := subuserListPageSize
	subuserStatsPageSize = 1
	subuserListPageSize = 10
	t.Cleanup(func() {
		subuserStatsPageSize = origStatsPage
		subuserListPageSize = origListPage
	})

	var monthlyHits, parentStatsHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		switch r.URL.Path {
		case "/v3/stats":
			parentStatsHits++
			http.Error(w, "should not be called", http.StatusInternalServerError)
		case "/v3/subusers/stats/monthly":
			monthlyHits++
			if monthlyHits > 3 {
				http.Error(w, "rate limited", http.StatusTooManyRequests)
				return
			}
			if r.URL.Query().Get("date") == "" {
				t.Error("monthly stats missing date")
			}
			if r.URL.Query().Get("sort_by_metric") != "requests" {
				t.Errorf("sort_by_metric = %q", r.URL.Query().Get("sort_by_metric"))
			}
			switch r.URL.Query().Get("offset") {
			case "0":
				_ = json.NewEncoder(w).Encode(SubuserMonthlyStats{
					Date: "2026-09-01",
					Stats: []*SubuserMonthlyStat{{
						Name:    "intr-tmop",
						Type:    "subuser",
						Metrics: &Metrics{Requests: 3150, Delivered: 3000},
					}},
				})
			case "1":
				_ = json.NewEncoder(w).Encode(SubuserMonthlyStats{
					Date: "2026-09-01",
					Stats: []*SubuserMonthlyStat{{
						Name:    "c373-373d",
						Type:    "subuser",
						Metrics: &Metrics{Requests: 1524, Delivered: 1400},
					}},
				})
			default:
				_ = json.NewEncoder(w).Encode(SubuserMonthlyStats{Date: "2026-09-01"})
			}
		case "/v3/subusers":
			_ = json.NewEncoder(w).Encode([]Subuser{
				{Username: "intr-tmop"},
				{Username: "c373-373d"},
				{Username: "idle-user"},
			})
		case "/v3/user/credits":
			_ = json.NewEncoder(w).Encode(CreditBalance{Total: 100, Remain: 40, Used: 60, Overage: 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	col := newCollector(slog.New(slog.NewTextHandler(io.Discard, nil)), collectorConfig{
		userName:        "parent-acct",
		apiBase:         srv.URL,
		apiKey:          "test-key",
		includeSubusers: true,
		httpTimeout:     2 * time.Second,
	}, nil)

	first := gatherLabeled(t, col)
	assertLabeled(t, first, "sendgrid_requests", "intr-tmop", 3150)
	assertLabeled(t, first, "sendgrid_delivered", "intr-tmop", 3000)
	assertLabeled(t, first, "sendgrid_requests", "c373-373d", 1524)
	assertLabeled(t, first, "sendgrid_requests", "idle-user", 0)
	assertLabeled(t, first, "sendgrid_up", "parent-acct", 1)
	assertLabeled(t, first, "sendgrid_credit_total", "parent-acct", 100)
	if parentStatsHits != 0 {
		t.Fatalf("parent /v3/stats hits = %d, want 0", parentStatsHits)
	}
	if monthlyHits != 3 {
		t.Fatalf("monthly hits = %d, want 3 (two pages + empty)", monthlyHits)
	}

	second := gatherLabeled(t, col)
	assertLabeled(t, second, "sendgrid_up", "parent-acct", 0)
	assertLabeled(t, second, "sendgrid_requests", "intr-tmop", 3150)
	assertLabeled(t, second, "sendgrid_requests", "c373-373d", 1524)
	assertLabeled(t, second, "sendgrid_credit_total", "parent-acct", 100)
}

func TestCollectSubusersListForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/subusers/stats/monthly":
			_ = json.NewEncoder(w).Encode(SubuserMonthlyStats{
				Date: "2026-09-01",
				Stats: []*SubuserMonthlyStat{{
					Name:    "intr-tmop",
					Metrics: &Metrics{Requests: 42},
				}},
			})
		case "/v3/subusers":
			http.Error(w, "forbidden", http.StatusForbidden)
		case "/v3/user/credits":
			_ = json.NewEncoder(w).Encode(CreditBalance{Total: 10})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	col := newCollector(slog.New(slog.NewTextHandler(io.Discard, nil)), collectorConfig{
		apiBase:         srv.URL,
		apiKey:          "test-key",
		includeSubusers: true,
		httpTimeout:     2 * time.Second,
	}, nil)

	got := gatherLabeled(t, col)
	assertLabeled(t, got, "sendgrid_requests", "intr-tmop", 42)
	assertLabeled(t, got, "sendgrid_up", "parent", 1)
	assertLabeled(t, got, "sendgrid_credit_total", "parent", 10)
}

func TestGetJSONErrorIncludesPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/v3/stats") {
			http.Error(w, "nope", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(CreditBalance{})
	}))
	defer srv.Close()

	col := newTestCollector(srv.URL)
	var statsErr error
	ctx := t.Context()
	_, statsErr = col.collectByDate(ctx, time.Now(), time.Now())
	if statsErr == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(statsErr.Error(), "/v3/stats") {
		t.Fatalf("error %q should include endpoint", statsErr)
	}
	if !strings.Contains(statsErr.Error(), "status=403") {
		t.Fatalf("error %q should include status", statsErr)
	}
}

func TestCollectSerializesScrapes(t *testing.T) {
	var inFlight, maxInFlight atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/v3/stats") {
			n := inFlight.Add(1)
			for {
				old := maxInFlight.Load()
				if n <= old || maxInFlight.CompareAndSwap(old, n) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			inFlight.Add(-1)
			_ = json.NewEncoder(w).Encode([]*Statistics{{
				Stats: []*Stat{{Metrics: &Metrics{Requests: 1}}},
			}})
			return
		}
		_ = json.NewEncoder(w).Encode(CreditBalance{})
	}))
	defer srv.Close()

	col := newTestCollector(srv.URL)
	reg := prometheus.NewPedanticRegistry()
	if err := reg.Register(col); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			if _, err := reg.Gather(); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}

	if got := maxInFlight.Load(); got != 1 {
		t.Fatalf("max in-flight SendGrid stats calls = %d, want 1", got)
	}
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
	labeled := gatherLabeled(t, col)
	out := map[string]float64{}
	for name, byUser := range labeled {
		for _, value := range byUser {
			out[name] = value
		}
	}
	return out
}

func gatherLabeled(t *testing.T, col prometheus.Collector) map[string]map[string]float64 {
	t.Helper()
	reg := prometheus.NewPedanticRegistry()
	if err := reg.Register(col); err != nil {
		t.Fatal(err)
	}
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}

	out := map[string]map[string]float64{}
	for _, mf := range mfs {
		name := mf.GetName()
		if out[name] == nil {
			out[name] = map[string]float64{}
		}
		for _, m := range mf.GetMetric() {
			user := ""
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "user_name" {
					user = lp.GetValue()
				}
			}
			out[name][user] = m.GetGauge().GetValue()
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

func assertLabeled(t *testing.T, metrics map[string]map[string]float64, name, userName string, want float64) {
	t.Helper()
	byUser, ok := metrics[name]
	if !ok {
		t.Fatalf("missing metric %s in %v", name, metrics)
	}
	got, ok := byUser[userName]
	if !ok {
		t.Fatalf("missing %s{user_name=%q} in %v", name, userName, byUser)
	}
	if got != want {
		t.Fatalf("%s{user_name=%q} = %v, want %v", name, userName, got, want)
	}
}
