# 10. PII scrubbing at the ingest gateway

Date: 2026-10-16. Status: accepted.

## Context

Customers send personal data by accident (emails in URLs, card numbers in
error messages, tokens in headers). Data that reaches Kafka and ClickHouse
is hard to remove everywhere.

## Decision

Scrub in the ingest gateway, before Kafka, with per-project rules: built-in
email, Luhn-checked card and IP patterns, sensitive key names and custom
RE2 patterns. Rules are cached in the gateway for 30 seconds. Identity ids
are never scrubbed so stitching keeps working. Deletion requests and
retention are handled by a separate governance worker.

## Consequences

- Scrubbed values never reach any store, backups included.
- Every request pays the regex cost; patterns are bounded in number and
  length, and the load test numbers include scrubbing.
- Rule changes do not rewrite data already stored.
