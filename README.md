# sendgrid-stats-exporter

Prometheus exporter for SendGrid daily metrics exposed by SendGrid Stats API (v3).

    +---------------------------+                          +------------+                        +--------------+
    |  SendGrid Stats API (v3)  |---(collect /v3/stats)--->|  exporter  |<---(scrape /metrics)---|  Prometheus  |
    +---------------------------+                          +------------+                        +--------------+

## Usage

```
$ make
$ ./dist/exporter --sendgrid.api-key='secret' --web.listen-address=':9154' --web.disable-exporter-metrics
```

```
$ curl localhost:9154/-/healthy
$ curl localhost:9154/metrics
```

```
$ ./exporter -h
usage: exporter [<flags>]

Flags:
  -h, --help                  Show context-sensitive help (also try --help-long and --help-man).
      --web.listen-address=":9154"
                              Address to listen on for web interface and telemetry.
      --web.disable-exporter-metrics
                              Exclude metrics about the exporter itself (promhttp_*, process_*, go_*).
      --sendgrid.api-key      [Required] SendGrid API key
      --sendgrid.username=""  [Optional] SendGrid username as a label for each metric.
      --sendgrid.api-base="https://api.sendgrid.com"
                              [Optional] SendGrid API base URL. Use https://api.eu.sendgrid.com for the EU region.
      --sendgrid.timeout=10s  [Optional] Timeout for SendGrid API requests.
      --sendgrid.location=""  [Optional] Time zone name (e.g. Asia/Tokyo). The default is UTC.
      --sendgrid.time-offset=0
                              [Optional] Offset in seconds from UTC (e.g. 32400). Must be set together with location.
      --sendgrid.accumulated-metrics=false
                              [Optional] Accumulate SendGrid metrics by month, to calculate monthly email limit.
      --log.level=info        Only log messages with the given severity or above. One of: [debug, info, warn, error]
      --log.format=logfmt     Output format of log messages. One of: [logfmt, json]
      --version               Show application version.
```

## Endpoints

Name     | Description
---------|-------------
`/metrics` | Prometheus metrics
`/-/healthy` | Process liveness check

## Metrics

Name     | Description
---------|------------
up | 1 if the last SendGrid stats scrape succeeded, otherwise 0. On failure the last successful stats/credits are re-exported.
blocks | The number of emails that were not allowed to be delivered by ISPs.
bounce_drops | The number of emails that were dropped because of a bounce.
bounces | The number of emails that bounced instead of being delivered.
clicks | The number of times recipients clicked links in your emails.
credit_overage | The number of credits consumed beyond the plan's allocation.
credit_remaining | The number of credits currently remaining in the billing period.
credit_total | The total number of credits available in the billing period.
credit_used | The number of credits already consumed in the billing period.
deferred | The number of emails that temporarily could not be delivered.
delivered | The number of emails SendGrid was able to confirm were actually delivered to a recipient.
invalid_emails | The number of recipients who had malformed email addresses or whose mail provider reported the address as invalid.
opens | The number of times emails were opened.
processed | Requests from your website, application, or mail client via SMTP Relay or the API that SendGrid processed.
requests | The number of emails that were requested to be delivered.
spam_report_drops | The number of emails that were dropped due to a recipient previously marking your emails as spam.
spam_reports | The number of recipients who marked your email as spam.
unique_clicks | The number of unique recipients who clicked links in your emails.
unique_opens | The number of unique recipients who opened your emails.
unsubscribe_drops | The number of emails dropped due to a recipient unsubscribing from your emails.
unsubscribes | The number of recipients who unsubscribed from your emails.

## Dashboard

A sample dashboard using those metrics has been published [here](https://grafana.com/grafana/dashboards/16319).

### Running with Docker

```
$ docker run -d -p 9154:9154 -e SENDGRID_API_KEY=secret chatwork/sendgrid-stats-exporter
```

For the EU region, also set `SENDGRID_API_BASE=https://api.eu.sendgrid.com`.

#### Running with `docker-compose`

```
$ cp .env.example .env
$ vi .env
$ docker-compose up -d
```

You can check the metrics by accessing Prometheus ([http://127.0.0.1:9090](http://127.0.0.1:9090)).

#### Running with `helm`

https://github.com/chatwork/sendgrid-stats-exporter/tree/main/charts

## Building

### Building locally

```
$ make
$ make test
```

### Building with Docker

```
$ docker build -t sendgrid-stats-exporter .
```
