# Dashboards and insights

## Insights (trend query builder)

`/insights` charts product events over time. A query has:

- **Series** (A, B, … up to 10). Each series is one event, or every event, counted as:
  - total count, unique users (`distinct_id`) or unique sessions;
  - sum, average, minimum, maximum or p95 of a numeric property.
- **Filters**, on one series or on all of them:
  - operators `=`, `≠`, contains, does not contain, `>`, `<`, is set and is not set;
  - a filter's property is `distinct_id`, `session_id`, `event` or any key of the event properties (for example `$pathname`, `$current_url`, `$referrer` or your own keys).
- **Breakdown** by one property. The top 10 values by event count get their own line; the rest are grouped as `$other`.
- **Interval**: hour, day, week or month, at most 1000 buckets per query.
- **Formula** over the series letters, such as `B / A * 100`, with `+ - * /`, parentheses and numbers:
  - division by zero charts as 0;
  - with a breakdown, the formula is evaluated per breakdown value;
  - the formula replaces the series in the result.
- **Display**: line chart, bar chart of totals, single number (the first series' total) or table.

Totals cover the whole range. For unique math they count real unique users or sessions, not a sum of the buckets.

Everything the user types travels as a bound ClickHouse parameter. Only the interval function and the breakdown limit are fixed literals in the SQL. Query endpoint: `POST /v1/insights/trends`.

## Dashboards

A dashboard is a named grid of widgets, each a saved query:

| Kind | Query | Source |
| --- | --- | --- |
| `trend` | a trend query plus `display` | `POST /v1/insights/trends` |
| `funnel` | `steps` and `window_seconds` | `POST /v1/funnels/query` |
| `metric` | `name`, optional `agg` and `group_by` | `GET /v1/metrics/query` |
| `traces` | optional `service` | `GET /v1/traces/stats` (root span requests, errors, p50/p95) |

Widgets are half or full width. The dashboard's range picker (24 hours, 7 days or 30 days) applies to every widget.

Validation rules:

- Saved queries are validated when the dashboard is saved, with the same rules as the live endpoints. For example, a formula that uses a missing series is rejected.
- A dashboard holds at most 30 widgets, and each widget query is at most 8 KiB.

### Sharing and permissions

- A new dashboard is **private**: only its creator sees it.
- A **shared** dashboard is visible to everyone who can read the project. In practice that is every member of the project's organization.
- The creator can always edit and delete. Project owners can also edit and delete shared dashboards, so a team can clean up after someone leaves.
- Other members get a read-only view; the API answers their changes with 403.

Dashboards live in Postgres (`dashboards`, see `schemas/postgres/009_dashboards.sql`) and are deleted with their project.
