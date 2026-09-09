package main

import (
	"context"
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
)

const (
	stopTimeoutSecond = 10
)

var (
	gitCommit     string
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
		"[Required] Set SendGrid API key",
	).Default("secret").Envar("SENDGRID_API_KEY").String()
	sendGridUserName = kingpin.Flag(
		"sendgrid.username",
		"[Optional] Set SendGrid username as a label for each metrics. This is for identifying multiple SendGrid users metrics.",
	).Default("").Envar("SENDGRID_USER_NAME").String()
	location = kingpin.Flag(
		"sendgrid.location",
		"[Optional] Set a zone name.(e.g. 'Asia/Tokyo') The default is UTC.",
	).Default("").Envar("SENDGRID_LOCATION").String()
	timeOffset = kingpin.Flag(
		"sendgrid.time-offset",
		"[Optional] Specify the offset in second from UTC as an integer.(e.g. '32400') This needs to be set along with location.",
	).Default("0").Envar("SENDGRID_TIME_OFFSET").Int()
	accumulatedMetrics = kingpin.Flag(
		"sendgrid.accumulated-metrics",
		"[Optional] Accumulated SendGrid Metrics by month, to calculate monthly email limit.",
	).Default("False").Envar("SENDGRID_ACCUMULATED_METRICS").Bool()
)

func main() {
	promslogConfig := &promslog.Config{}
	flag.AddFlags(kingpin.CommandLine, promslogConfig)
	kingpin.Version(version.Print(exporterName))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	logger := promslog.New(promslogConfig)

	logger.Info("Starting "+exporterName, "version", version.Info(), "build_context", version.BuildContext(), "git_commit", gitCommit)
	logger.Info("Listening on address", "address", *listenAddress)

	collector := collector(logger)
	prometheus.MustRegister(collector)
	prometheus.Unregister(collectors.NewGoCollector())
	registry := prometheus.NewRegistry()

	if !*disableExporterMetrics {
		registry.MustRegister(
			collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
			collectors.NewGoCollector(),
		)
	}

	registry.MustRegister(collector)

	sig := make(chan os.Signal, 1)
	signal.Notify(
		sig,
		syscall.SIGTERM,
		syscall.SIGINT,
	)

	defer signal.Stop(sig)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/-/healthy", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`OK`))
	})

	srv := &http.Server{
		Addr:    *listenAddress,
		Handler: mux,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server stopped", "err", err)
		}
	}()

	<-sig

	ctx, cancel := context.WithTimeout(context.Background(), stopTimeoutSecond*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("HTTP server shutdown failed", "err", err)
	}
}
