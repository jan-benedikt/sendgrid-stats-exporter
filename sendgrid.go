package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultSendGridAPIBase = "https://api.sendgrid.com"
	maxErrorBodyBytes      = 2048
	maxResponseBodyBytes   = 1 << 20
	maxSubuserPages        = 50
)

var (
	subuserStatsPageSize = 100
	subuserListPageSize  = 100
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

type SubuserMonthlyStats struct {
	Date  string                `json:"date,omitempty"`
	Stats []*SubuserMonthlyStat `json:"stats,omitempty"`
}

type SubuserMonthlyStat struct {
	Name    string   `json:"name,omitempty"`
	Type    string   `json:"type,omitempty"`
	Metrics *Metrics `json:"metrics,omitempty"`
}

type Subuser struct {
	Username string `json:"username,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

func (c *Collector) collectByDate(ctx context.Context, timeStart, timeEnd time.Time) ([]*Statistics, error) {
	layout := "2006-01-02"
	query := url.Values{}
	query.Set("start_date", timeStart.Format(layout))
	query.Set("end_date", timeEnd.Format(layout))
	if c.cfg.accumulatedMetrics {
		query.Set("aggregated_by", "month")
	} else {
		query.Set("aggregated_by", "day")
	}

	rawURL, err := c.apiURL("/v3/stats", query)
	if err != nil {
		return nil, err
	}

	var stats []*Statistics
	if err := c.getJSON(ctx, rawURL, &stats); err != nil {
		return nil, err
	}

	return stats, nil
}

func (c *Collector) collectSubuserMonthlyMetrics(ctx context.Context, date time.Time) (map[string]*Metrics, error) {
	out := make(map[string]*Metrics)
	dateStr := date.Format("2006-01-02")
	pageSize := subuserStatsPageSize
	if pageSize <= 0 {
		pageSize = 100
	}

	lastPageFull := false
	for offset := 0; offset < maxSubuserPages*pageSize; offset += pageSize {
		query := url.Values{}
		query.Set("date", dateStr)
		query.Set("limit", strconv.Itoa(pageSize))
		query.Set("offset", strconv.Itoa(offset))
		query.Set("sort_by_metric", "requests")
		query.Set("sort_by_direction", "desc")

		rawURL, err := c.apiURL("/v3/subusers/stats/monthly", query)
		if err != nil {
			return nil, err
		}

		var page SubuserMonthlyStats
		if err := c.getJSON(ctx, rawURL, &page); err != nil {
			return nil, err
		}

		for _, st := range page.Stats {
			if st == nil || st.Name == "" {
				continue
			}
			m := Metrics{}
			if st.Metrics != nil {
				m = *st.Metrics
			}
			out[st.Name] = &m
		}

		lastPageFull = len(page.Stats) >= pageSize
		if !lastPageFull {
			break
		}
	}
	if lastPageFull {
		c.logger.Warn("reached subuser monthly stats page limit; some subusers may be missing")
	}

	names, err := c.listSubusers(ctx)
	if err != nil {
		c.logger.Warn("Failed to list subusers; exporting monthly stats only", "err", err)
		return out, nil
	}
	for _, name := range names {
		if _, ok := out[name]; !ok {
			out[name] = &Metrics{}
		}
	}

	return out, nil
}

func (c *Collector) listSubusers(ctx context.Context) ([]string, error) {
	pageSize := subuserListPageSize
	if pageSize <= 0 {
		pageSize = 100
	}

	var names []string
	lastPageFull := false
	for offset := 0; offset < maxSubuserPages*pageSize; offset += pageSize {
		query := url.Values{}
		query.Set("limit", strconv.Itoa(pageSize))
		query.Set("offset", strconv.Itoa(offset))

		rawURL, err := c.apiURL("/v3/subusers", query)
		if err != nil {
			return nil, err
		}

		var page []Subuser
		if err := c.getJSON(ctx, rawURL, &page); err != nil {
			return nil, err
		}

		for _, su := range page {
			if su.Username != "" {
				names = append(names, su.Username)
			}
		}

		lastPageFull = len(page) >= pageSize
		if !lastPageFull {
			break
		}
	}
	if lastPageFull {
		c.logger.Warn("reached subuser list page limit; some subusers may be missing")
	}

	return names, nil
}

func (c *Collector) apiURL(path string, query url.Values) (string, error) {
	parsedURL, err := url.Parse(c.cfg.apiBase + path)
	if err != nil {
		return "", err
	}
	if query != nil {
		parsedURL.RawQuery = query.Encode()
	}
	return parsedURL.String(), nil
}

func (c *Collector) collectCreditBalance(ctx context.Context) (*CreditBalance, error) {
	rawURL, err := c.apiURL("/v3/user/credits", nil)
	if err != nil {
		return nil, err
	}

	var creditBalance CreditBalance
	if err := c.getJSON(ctx, rawURL, &creditBalance); err != nil {
		return nil, err
	}

	return &creditBalance, nil
}

func newSendGridHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		cloned := t.Clone()
		cloned.TLSHandshakeTimeout = 10 * time.Second
		cloned.IdleConnTimeout = 90 * time.Second
		transport = cloned
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (c *Collector) getJSON(ctx context.Context, rawURL string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.cfg.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", exporterName)

	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBodyBytes+1))
	if err != nil {
		return fmt.Errorf("read sendgrid response: %w", err)
	}
	if len(body) > maxResponseBodyBytes {
		return fmt.Errorf("sendgrid API %s: response too large", requestPath(req))
	}

	switch res.StatusCode {
	case http.StatusTooManyRequests:
		return fmt.Errorf("sendgrid API %s: rate limit exceeded", requestPath(req))
	case http.StatusOK:
		if err := json.Unmarshal(body, dest); err != nil {
			return fmt.Errorf("decode sendgrid response %s: %w", requestPath(req), err)
		}
		return nil
	default:
		return fmt.Errorf("sendgrid API %s status=%d body=%s", requestPath(req), res.StatusCode, truncateBody(body))
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
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func validateAPIBase(raw string) error {
	if raw == "" {
		return fmt.Errorf("sendgrid API base is empty")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("sendgrid API base: %w", err)
	}
	if parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("sendgrid API base must be an absolute http(s) URL")
	}
	return nil
}

func requestPath(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	return req.URL.RequestURI()
}
