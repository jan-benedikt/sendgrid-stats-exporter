package main

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jinzhu/now"
	"github.com/prometheus/client_golang/prometheus"
)

type Collector struct {
	logger *slog.Logger
	cfg    collectorConfig
	client *http.Client

	mu          sync.Mutex
	lastMetrics *Metrics
	lastCredits *CreditBalance

	up               *prometheus.Desc
	blocks           *prometheus.Desc
	bounceDrops      *prometheus.Desc
	bounces          *prometheus.Desc
	clicks           *prometheus.Desc
	creditOverage    *prometheus.Desc
	creditRemain     *prometheus.Desc
	creditTotal      *prometheus.Desc
	creditUsed       *prometheus.Desc
	deferred         *prometheus.Desc
	delivered        *prometheus.Desc
	invalidEmails    *prometheus.Desc
	opens            *prometheus.Desc
	processed        *prometheus.Desc
	requests         *prometheus.Desc
	spamReportDrops  *prometheus.Desc
	spamReports      *prometheus.Desc
	uniqueClicks     *prometheus.Desc
	uniqueOpens      *prometheus.Desc
	unsubscribeDrops *prometheus.Desc
	unsubscribes     *prometheus.Desc
}

func newCollector(logger *slog.Logger, cfg collectorConfig, client *http.Client) *Collector {
	if client == nil {
		timeout := cfg.httpTimeout
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}

	labels := []string{"user_name"}

	return &Collector{
		logger: logger,
		cfg:    cfg,
		client: client,

		up:               newDesc("up", "Whether the last SendGrid stats scrape succeeded (1) or failed (0).", labels),
		blocks:           newDesc("blocks", "Emails that were not allowed to be delivered by ISPs.", labels),
		bounceDrops:      newDesc("bounce_drops", "Emails dropped because of a bounce.", labels),
		bounces:          newDesc("bounces", "Emails that bounced instead of being delivered.", labels),
		clicks:           newDesc("clicks", "Total number of times recipients clicked links in emails.", labels),
		creditOverage:    newDesc("credit_overage", "Credits consumed beyond the plan allocation.", labels),
		creditRemain:     newDesc("credit_remaining", "Credits remaining in the billing period.", labels),
		creditTotal:      newDesc("credit_total", "Total credits available in the billing period.", labels),
		creditUsed:       newDesc("credit_used", "Credits already consumed in the billing period.", labels),
		deferred:         newDesc("deferred", "Emails that temporarily could not be delivered.", labels),
		delivered:        newDesc("delivered", "Emails SendGrid confirmed were delivered.", labels),
		invalidEmails:    newDesc("invalid_emails", "Recipients with malformed or rejected email addresses.", labels),
		opens:            newDesc("opens", "Total number of times emails were opened.", labels),
		processed:        newDesc("processed", "Requests processed via SMTP Relay or the API.", labels),
		requests:         newDesc("requests", "Emails requested to be delivered.", labels),
		spamReportDrops:  newDesc("spam_report_drops", "Emails dropped due to a previous spam report.", labels),
		spamReports:      newDesc("spam_reports", "Recipients who marked emails as spam.", labels),
		uniqueClicks:     newDesc("unique_clicks", "Unique recipients who clicked links in emails.", labels),
		uniqueOpens:      newDesc("unique_opens", "Unique recipients who opened emails.", labels),
		unsubscribeDrops: newDesc("unsubscribe_drops", "Emails dropped due to a previous unsubscribe.", labels),
		unsubscribes:     newDesc("unsubscribes", "Recipients who unsubscribed.", labels),
	}
}

func newDesc(name, help string, labels []string) *prometheus.Desc {
	return prometheus.NewDesc(prometheus.BuildFQName(namespace, "", name), help, labels, nil)
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	timeout := c.cfg.httpTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	today := time.Now()
	if c.cfg.location != "" && c.cfg.timeOffset != 0 {
		today = time.Now().In(time.FixedZone(c.cfg.location, c.cfg.timeOffset))
	}

	queryDate := today
	if c.cfg.accumulatedMetrics {
		queryDate = now.With(today).BeginningOfMonth()
	}

	statistics, statsErr := c.collectByDate(ctx, queryDate, today)
	creditBalance, creditErr := c.collectCreditBalance(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()

	up := 1.0
	metrics := c.lastMetrics
	if statsErr != nil {
		c.logger.Error("Failed to collect statistics", "err", statsErr)
		up = 0
	} else if m, err := firstMetrics(statistics); err != nil {
		c.logger.Error("Failed to parse statistics", "err", err)
		up = 0
	} else {
		c.lastMetrics = m
		metrics = m
	}

	credits := c.lastCredits
	if creditErr != nil {
		c.logger.Error("Failed to collect credit balance", "err", creditErr)
	} else {
		c.lastCredits = creditBalance
		credits = creditBalance
	}

	c.emit(ch, c.up, up)

	if metrics != nil {
		c.emitStats(ch, metrics)
	}
	if credits != nil {
		c.emitCredits(ch, credits)
	}
}

func (c *Collector) emitStats(ch chan<- prometheus.Metric, metrics *Metrics) {
	c.emit(ch, c.blocks, float64(metrics.Blocks))
	c.emit(ch, c.bounceDrops, float64(metrics.BounceDrops))
	c.emit(ch, c.bounces, float64(metrics.Bounces))
	c.emit(ch, c.clicks, float64(metrics.Clicks))
	c.emit(ch, c.deferred, float64(metrics.Deferred))
	c.emit(ch, c.delivered, float64(metrics.Delivered))
	c.emit(ch, c.invalidEmails, float64(metrics.InvalidEmails))
	c.emit(ch, c.opens, float64(metrics.Opens))
	c.emit(ch, c.processed, float64(metrics.Processed))
	c.emit(ch, c.requests, float64(metrics.Requests))
	c.emit(ch, c.spamReportDrops, float64(metrics.SpamReportDrops))
	c.emit(ch, c.spamReports, float64(metrics.SpamReports))
	c.emit(ch, c.uniqueClicks, float64(metrics.UniqueClicks))
	c.emit(ch, c.uniqueOpens, float64(metrics.UniqueOpens))
	c.emit(ch, c.unsubscribeDrops, float64(metrics.UnsubscribeDrops))
	c.emit(ch, c.unsubscribes, float64(metrics.Unsubscribes))
}

func (c *Collector) emitCredits(ch chan<- prometheus.Metric, credits *CreditBalance) {
	c.emit(ch, c.creditTotal, float64(credits.Total))
	c.emit(ch, c.creditRemain, float64(credits.Remain))
	c.emit(ch, c.creditUsed, float64(credits.Used))
	c.emit(ch, c.creditOverage, float64(credits.Overage))
}

func (c *Collector) emit(ch chan<- prometheus.Metric, desc *prometheus.Desc, value float64) {
	ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, value, c.cfg.userName)
}
