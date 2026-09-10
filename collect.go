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

	mu                sync.Mutex
	lastMetricsByUser map[string]*Metrics
	lastCredits       *CreditBalance

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
		client = newSendGridHTTPClient(cfg.requestTimeout())
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
	c.mu.Lock()
	defer c.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.scrapeTimeout())
	defer cancel()

	today := time.Now()
	if c.cfg.location != "" && c.cfg.timeOffset != 0 {
		today = time.Now().In(time.FixedZone(c.cfg.location, c.cfg.timeOffset))
	}

	queryDate := today
	if c.cfg.accumulatedMetrics {
		queryDate = now.With(today).BeginningOfMonth()
	}

	byUser, statsErr := c.collectStatsByUser(ctx, queryDate, today)
	creditBalance, creditErr := c.collectCreditBalance(ctx)

	up := 1.0
	metricsByUser := c.lastMetricsByUser
	if statsErr != nil {
		c.logger.Error("Failed to collect statistics", "err", statsErr)
		up = 0
	} else {
		c.lastMetricsByUser = byUser
		metricsByUser = byUser
	}

	credits := c.lastCredits
	if creditErr != nil {
		c.logger.Error("Failed to collect credit balance", "err", creditErr)
	} else {
		c.lastCredits = creditBalance
		credits = creditBalance
	}

	account := c.accountLabel()
	c.emit(ch, c.up, up, account)

	for userName, metrics := range metricsByUser {
		if metrics == nil {
			continue
		}
		c.emitStats(ch, userName, metrics)
	}
	if credits != nil {
		c.emitCredits(ch, account, credits)
	}
}

func (c *Collector) collectStatsByUser(ctx context.Context, queryDate, today time.Time) (map[string]*Metrics, error) {
	if c.cfg.includeSubusers {
		return c.collectSubuserMonthlyMetrics(ctx, today)
	}

	statistics, err := c.collectByDate(ctx, queryDate, today)
	if err != nil {
		return nil, err
	}
	m, err := firstMetrics(statistics)
	if err != nil {
		return nil, err
	}
	copied := *m
	return map[string]*Metrics{c.cfg.userName: &copied}, nil
}

func (c *Collector) accountLabel() string {
	if c.cfg.userName != "" {
		return c.cfg.userName
	}
	if c.cfg.includeSubusers {
		return "parent"
	}
	return ""
}

func (c *Collector) emitStats(ch chan<- prometheus.Metric, userName string, metrics *Metrics) {
	c.emit(ch, c.blocks, float64(metrics.Blocks), userName)
	c.emit(ch, c.bounceDrops, float64(metrics.BounceDrops), userName)
	c.emit(ch, c.bounces, float64(metrics.Bounces), userName)
	c.emit(ch, c.clicks, float64(metrics.Clicks), userName)
	c.emit(ch, c.deferred, float64(metrics.Deferred), userName)
	c.emit(ch, c.delivered, float64(metrics.Delivered), userName)
	c.emit(ch, c.invalidEmails, float64(metrics.InvalidEmails), userName)
	c.emit(ch, c.opens, float64(metrics.Opens), userName)
	c.emit(ch, c.processed, float64(metrics.Processed), userName)
	c.emit(ch, c.requests, float64(metrics.Requests), userName)
	c.emit(ch, c.spamReportDrops, float64(metrics.SpamReportDrops), userName)
	c.emit(ch, c.spamReports, float64(metrics.SpamReports), userName)
	c.emit(ch, c.uniqueClicks, float64(metrics.UniqueClicks), userName)
	c.emit(ch, c.uniqueOpens, float64(metrics.UniqueOpens), userName)
	c.emit(ch, c.unsubscribeDrops, float64(metrics.UnsubscribeDrops), userName)
	c.emit(ch, c.unsubscribes, float64(metrics.Unsubscribes), userName)
}

func (c *Collector) emitCredits(ch chan<- prometheus.Metric, userName string, credits *CreditBalance) {
	c.emit(ch, c.creditTotal, float64(credits.Total), userName)
	c.emit(ch, c.creditRemain, float64(credits.Remain), userName)
	c.emit(ch, c.creditUsed, float64(credits.Used), userName)
	c.emit(ch, c.creditOverage, float64(credits.Overage), userName)
}

func (c *Collector) emit(ch chan<- prometheus.Metric, desc *prometheus.Desc, value float64, userName string) {
	ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, value, userName)
}
