# Product analytics

Pulse turns the events your apps send into people, retention cohorts and
user paths.

## Persons and identity

Each browser starts with an anonymous id. When a visitor signs in, call
`identify`; the SDK sends an `$identify` event that carries the old anonymous
id in `$anon_distinct_id`. To link another id you already use (for example an
account id sent from your backend), call `alias`:

```ts
import { identify, alias } from '@pulse/browser';

identify('user_42', { email: 'ana@shop.example', plan: 'pro' });
alias('acct_9001'); // events sent as acct_9001 belong to user_42 too
```

The stream processor writes each link to the `person_aliases` table in
ClickHouse. Queries resolve every id to its person when they run, so events
sent before the link are merged too. Links are resolved one level deep: an
alias of an alias points at the person the first alias resolved to when it
was written.

- **Persons** lists people seen in the time range with their ids, traits,
  first and last seen time and event count. Search matches ids and traits.
- **Person profile** shows traits (from the latest `identify` call), all linked ids,
  the browser, OS and country, sessions with their duration, error count and
  replay link, and the latest events.

Trends and funnels still count distinct ids, not persons.

## Retention

Pick a start event and a return event (empty means any event) and a day or
week period. Each person joins the cohort of the period of their first start
event in the range. A cell shows how many of them sent the return event in
each later period. The top row is the average, weighted by cohort size.

## Paths

Paths follow sessions page by page (`$pageview` mode, by `$pathname`) or
event by event. Repeats of the same step are collapsed. Set a start page or
event to only follow sessions from it. Each step keeps its 8 most common
nodes and groups the rest as "Other". The page shows the flow, the drop-off
after each node and the most common full paths.

## Device and location columns

The ingest gateway fills `$browser`, `$browser_version`, `$os` and
`$device_type` from the request's User-Agent unless the event already has
them. Set `COUNTRY_HEADER` to the header your CDN or proxy sets with the
visitor's country (Cloudflare uses `CF-IPCountry`) to fill `$country`. Only
set it when that proxy overwrites the header, since clients can send any
value. The browser SDK adds `$release` when `release` is configured. Events
stored before this change keep empty columns.
