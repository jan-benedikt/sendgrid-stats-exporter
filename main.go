package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promslog"
	"github.com/prometheus/common/promslog/flag"
	"github.com/prometheus/common/version"
)

const (
	namespace    = "sendgrid"
	exporterName = "sendgrid-stats-exporter"

	stopTimeout     = 10 * time.Second
	httpReadTimeout = 10 * time.Second
	httpIdleTimeout = 60 * time.Second
)

type collectorConfig struct {
	userName           string
	apiBase            string
	apiKey             string
	location           string
	timeOffset         int
	accumulatedMetrics bool
	httpTimeout        time.Duration
}

func main() {
	var (
		listenAddress = kingpin.Flag(
			"web.listen-address",
			"Address to listen on for web interface and telemetry.",
		).Default(":9154").Envar("LISTEN_ADDRESS").String()
		disableExporterMetrics = kingpin.Flag(
			"web.disable-exporter-metrics",
			"Exclude metrics about the exporter itself (promhttp_*, process_*, go_*).",
		).Envar("DISABLE_EXPORTER_METRICS").Bool()
		sendGridAPIKey = kingpin.Flag(
			"sendgrid.api-key",
			"SendGrid API key.",
		).Envar("SENDGRID_API_KEY").Required().String()
		sendGridUserName = kingpin.Flag(
			"sendgrid.username",
			"SendGrid username as a label for each metric. Useful when scraping multiple accounts.",
		).Default("").Envar("SENDGRID_USER_NAME").String()
		sendGridAPIBase = kingpin.Flag(
			"sendgrid.api-base",
			"SendGrid API base URL. Use https://api.eu.sendgrid.com for the EU region.",
		).Default(defaultSendGridAPIBase).Envar("SENDGRID_API_BASE").String()
		sendGridHTTPTimeout = kingpin.Flag(
			"sendgrid.timeout",
			"Timeout for SendGrid API requests.",
		).Default("10s").Envar("SENDGRID_TIMEOUT").Duration()
		location = kingpin.Flag(
			"sendgrid.location",
			"Time zone name (e.g. Asia/Tokyo). Default is the local time zone / UTC.",
		).Default("").Envar("SENDGRID_LOCATION").String()
		timeOffset = kingpin.Flag(
			"sendgrid.time-offset",
			"Offset in seconds from UTC (e.g. 32400). Must be set together with location.",
		).Default("0").Envar("SENDGRID_TIME_OFFSET").Int()
		accumulatedMetrics = kingpin.Flag(
			"sendgrid.accumulated-metrics",
			"Accumulate SendGrid metrics by month, to calculate monthly email limit.",
		).Default("false").Envar("SENDGRID_ACCUMULATED_METRICS").Bool()
	)

	promslogConfig := &promslog.Config{}
	flag.AddFlags(kingpin.CommandLine, promslogConfig)
	kingpin.Version(version.Print(exporterName))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	logger := promslog.New(promslogConfig)
	os.Exit(run(logger, collectorConfig{
		userName:           *sendGridUserName,
		apiBase:            normalizeAPIBase(*sendGridAPIBase),
		apiKey:             *sendGridAPIKey,
		location:           *location,
		timeOffset:         *timeOffset,
		accumulatedMetrics: *accumulatedMetrics,
		httpTimeout:        *sendGridHTTPTimeout,
	}, *listenAddress, *disableExporterMetrics))
}

func run(logger *slog.Logger, cfg collectorConfig, listenAddress string, disableExporterMetrics bool) int {
	logger.Info("Starting "+exporterName, "version", version.Info(), "build_context", version.BuildContext(), "api_base", cfg.apiBase)
	logger.Info("Listening on address", "address", listenAddress)

	col := newCollector(logger, cfg, nil)
	registry := prometheus.NewRegistry()
	if !disableExporterMetrics {
		registry.MustRegister(
			collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
			collectors.NewGoCollector(),
		)
	}
	registry.MustRegister(col)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		Timeout: cfg.httpTimeout + 5*time.Second,
	}))
	mux.HandleFunc("/-/healthy", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	srv := &http.Server{
		Addr:         listenAddress,
		Handler:      mux,
		ReadTimeout:  httpReadTimeout,
		WriteTimeout: cfg.httpTimeout + 10*time.Second,
		IdleTimeout:  httpIdleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sig)

	select {
	case s := <-sig:
		logger.Info("Shutting down", "signal", s.String())
	case err := <-errCh:
		logger.Error("HTTP server stopped", "err", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("HTTP server shutdown failed", "err", err)
		return 1
	}

	return 0
}
