# Data governance

Each project has PII scrubbing rules, retention periods and person deletion
requests under **Settings → Data governance** (`/settings/governance`). Members can
view them and preview scrubbing. Only owners can change settings or request
deletions. Every change, deletion request, completed or failed deletion and
retention run is written to the organization's audit log.

## PII scrubbing at ingest

The ingest gateway scrubs data over HTTP and OTLP/gRPC before anything is
written to Kafka, so scrubbed values are never stored. The rules apply to:

- product event properties (including `identify` traits)
- error messages, stack traces and URLs
- log bodies and attributes
- span status messages, database statements and attributes
- text inside session replay chunks

| Rule | Replaced with | Default |
| --- | --- | --- |
| Email addresses | `[email]` | off |
| Card numbers (13 to 19 digits, Luhn-checked, spaces or dashes allowed) | `[card]` | on |
| IPv4 and IPv6 addresses | `[ip]` | off |
| Sensitive keys: a property or attribute whose name contains a key (case-insensitive) | `[redacted]` | `password`, `passwd`, `secret`, `token`, `api_key`, `authorization`, `cookie`, `ssn` and similar |
| Custom patterns (RE2 regular expressions, up to 20) | `[redacted]` | none |

Ids are never scrubbed (`distinct_id`, `$anon_distinct_id`, `alias`,
`$session_id`), so identity stitching keeps working. The gateway caches each
project's rules for 30 seconds; a change applies to data received after that.
Metrics are not scrubbed. Use **Preview** to try rules on a text or JSON
sample before saving.

## Retention

| Data | Limit (days) | Enforced with |
| --- | --- | --- |
| Product events and sessions | 30 | table TTL, then per-project deletes |
| Traces (spans) | 7 | table TTL (whole daily parts dropped), then per-project deletes |
| Logs | 3 | as above |
| Errors | 90 | table TTL, then per-project deletes |
| Session replays (rows and objects) | 30 | table TTL; objects removed from storage |
| Source maps | none (kept until deleted) | artifacts expired and objects removed |

A project can keep data for less than the limit, not for longer. The
`governance-worker` service applies the periods every `RETENTION_INTERVAL`
(default one hour): it deletes rows older than the project's period with
ClickHouse lightweight deletes, removes replay objects older than the replay
period from object storage, and, when a source map period is set, expires
those artifacts and their objects. Each run that deleted something is
recorded as `data.retention` in the audit log.

## Deletion requests

A deletion request names any id of a person (anonymous, identified or alias).
The id is resolved to the person and all linked ids. The worker, polling every
`DELETION_POLL_INTERVAL` (default 5s), recomputes the linked ids and deletes:

- events and sessions of those ids, and their sessions' replays (rows and
  objects in storage)
- errors, spans and logs of their sessions and traces, and logs tagged with
  `enduser.id`
- the id links themselves

The request shows `pending`, `running`, `completed` (with deleted counts per
table) or `failed`. Failed requests are retried up to five times, and a request
left running for 15 minutes is picked up again. Requests are created from the
data governance page, from **Delete person data** on a person profile, or with
`POST /v1/projects/{project_id}/deletion-requests`.

Deleting does not block new data: if the person's apps keep sending with the
same ids, new data is stored again. Stop collection for them first.
