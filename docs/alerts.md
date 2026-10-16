# Alerts

Alert rules live in Postgres per project (`schemas/postgres/006_alerts.sql`) and are managed under **Alerts** in the web app. A rule watches one metric over a window of 1m to 24h:

- `error_count`, `error_rate` (errors per minute), `failure_rate` (percent of server requests that failed)
- `latency_p95` and `latency_p99` (server spans, ms), `throughput` (requests per minute; compare with "below" to catch drops)
- `event_count` for a named product event
- `new_issue` and `issue_regression`, which fire from the `issue.events` topic

Rules can filter by service, route and environment. The `alerting-worker` checks each rule on its own interval (10s to 1h), using a Redis lease so only one replica evaluates it. A rule moves between OK, ALERTING and NO_DATA, and every change is kept as history with an example issue and trace. Notifications go out when a rule fires and when it resolves, and optionally repeat while it keeps firing; that same interval stops a flapping rule from paging again. Muting a rule pauses notifications but not evaluation.

Channels are email (through `SMTP_ADDR`; Mailpit locally), Slack incoming webhooks, and generic webhooks. Webhook requests carry `X-Pulse-Signature: t=<unix>,v1=<hex>`, where `v1` is HMAC-SHA256 of `<unix>.<body>` with the channel's secret, which is shown once when the channel is created.
