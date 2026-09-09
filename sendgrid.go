package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultSendGridAPIBase = "https://api.sendgrid.com"
	maxErrorBodyBytes      = 2048
	maxResponseBodyBytes   = 1 << 20
)

type Metrics struct {
	Blocks           int64 `json:"blocks,omitempty"`
	BounceDrops      int64 `json:"bounce_drops,omitempty"`
	Bounces          int64 `json:"bounces,omitempty"`
	Clicks           int64 `json:"clicks,omitempty"`
	Deferred         int64 `json:"deferred,omitempty"`
	Delivered        int64 `json:"delivered,omitempty"`
	InvalidEmails    int64 `json:"invalid_emails,omitempty"`
	Opens            int64 `json:"opens,omitempty"`
	Processed        int64 `json:"processed,omitempty"`
	Requests         int64 `json:"requests,omitempty"`
	SpamReportDrops  int64 `json:"spam_report_drops,omitempty"`
	SpamReports      int64 `json:"spam_reports,omitempty"`
	UniqueClicks     int64 `json:"unique_clicks,omitempty"`
	UniqueOpens      int64 `json:"unique_opens,omitempty"`
	UnsubscribeDrops int64 `json:"unsubscribe_drops,omitempty"`
	Unsubscribes     int64 `json:"unsubscribes,omitempty"`
}

type Stat struct {
	Metrics *Metrics `json:"metrics,omitempty"`
}

type Statistics struct {
	Date  string  `json:"date,omitempty"`
	Stats []*Stat `json:"stats,omitempty"`
}

type CreditBalance struct {
	Remain  int64 `json:"remain"`
	Overage int64 `json:"overage"`
	Total   int64 `json:"total"`
	Used    int64 `json:"used"`
}

func (c *Collector) collectByDate(ctx context.Context, timeStart, timeEnd time.Time) ([]*Statistics, error) {
	parsedURL, err := url.Parse(c.cfg.apiBase + "/v3/stats")
	if err != nil {
		return nil, err
	}

	layout := "2006-01-02"
	query := url.Values{}
	query.Set("start_date", timeStart.Format(layout))
	query.Set("end_date", timeEnd.Format(layout))
	if c.cfg.accumulatedMetrics {
		query.Set("aggregated_by", "month")
	} else {
		query.Set("aggregated_by", "day")
	}
	parsedURL.RawQuery = query.Encode()

	var stats []*Statistics
	if err := c.getJSON(ctx, parsedURL.String(), &stats); err != nil {
		return nil, err
	}

	return stats, nil
}

func (c *Collector) collectCreditBalance(ctx context.Context) (*CreditBalance, error) {
	var creditBalance CreditBalance
	if err := c.getJSON(ctx, c.cfg.apiBase+"/v3/user/credits", &creditBalance); err != nil {
		return nil, err
	}

	return &creditBalance, nil
}

func (c *Collector) getJSON(ctx context.Context, rawURL string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.cfg.apiKey)
	req.Header.Set("Accept", "application/json")

	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBodyBytes))
	if err != nil {
		return fmt.Errorf("read sendgrid response: %w", err)
	}

	switch res.StatusCode {
	case http.StatusTooManyRequests:
		return fmt.Errorf("sendgrid API rate limit exceeded")
	case http.StatusOK:
		if err := json.Unmarshal(body, dest); err != nil {
			return fmt.Errorf("decode sendgrid response: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("sendgrid API status=%d body=%s", res.StatusCode, truncateBody(body))
	}
}

func firstMetrics(statistics []*Statistics) (*Metrics, error) {
	if len(statistics) == 0 {
		return nil, fmt.Errorf("empty sendgrid statistics")
	}

	for _, stat := range statistics[0].Stats {
		if stat != nil && stat.Metrics != nil {
			return stat.Metrics, nil
		}
	}

	return nil, fmt.Errorf("no metrics in sendgrid statistics")
}

func truncateBody(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) <= maxErrorBodyBytes {
		return s
	}
	return s[:maxErrorBodyBytes] + "..."
}

func normalizeAPIBase(raw string) string {
	return strings.TrimRight(raw, "/")
}
