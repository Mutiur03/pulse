# Security

This page describes how Pulse protects accounts, data and the services
themselves, and which settings an operator should review before going to
production. To report a vulnerability, email the maintainer privately rather
than opening a public issue.

## Production checklist

| Setting | Service | Production value |
| --- | --- | --- |
| `APP_URL` | query-api, alerting-worker | The public `https://` URL of the web app. It is also the origin allowed to make state-changing API calls. |
| `COOKIE_SECURE` | query-api | Unset (defaults to `true`). Only plain-HTTP local stacks set `false`. |
| `TRUSTED_PROXIES` | query-api | The address(es) of the web app, e.g. `10.0.3.7` or `10.0.3.0/28`. Never a range that clients can reach directly. |
| `TRUST_PROXY_HEADERS` | web | `true` only if a load balancer you control sits in front of the web app and sets `X-Forwarded-For`. |
| `OUTBOUND_ALLOWLIST` | query-api, alerting-worker | Empty. The compose stack sets `host.docker.internal` for local webhook tests only. |
| `HSTS_MAX_AGE` | web, query-api | Web defaults to one year (`31536000`); set it on the API too if it is exposed over HTTPS. `0` turns it off. |
| `CSRF_TRUSTED_ORIGINS` | query-api | Extra origins of the web app, comma separated, if it is served under more than one. |
| `SESSION_IDLE_HOURS` | query-api | Default 24. |

## Accounts and sessions

- Passwords are hashed with bcrypt at cost 12. A login for an unknown email
  still runs a bcrypt comparison, so response times do not reveal which
  accounts exist; password reset answers the same way for every address.
- A session is a random 256-bit token in the `pulse_session` cookie
  (`HttpOnly`, `SameSite=Lax`, `Secure` unless `COOKIE_SECURE=false`, `Path=/`).
  Only its SHA-256 is stored in Postgres.
- Sessions end after 7 days (absolute) or after `SESSION_IDLE_HOURS` without
  use (idle). `last_seen_at` is updated at most once a minute.
- Every login and signup creates a new token and deletes the session the
  browser had before, so a planted cookie cannot be carried into a login.
- **Settings → Sessions → Sign out everywhere** (`POST /v1/auth/logout-all`)
  deletes every session of the user. Resetting a password does the same.
- Email verification, password reset, invite and upload tokens are random,
  single purpose and stored only as SHA-256 hashes; they are looked up by
  hash, so no secret is compared byte by byte in application code. Webhook
  signatures are checked with `hmac.Equal`.

### Rate limits

All counters live in Redis and fail open if Redis is down.

| Limit | Default | Setting |
| --- | --- | --- |
| Failed logins per email and client address | 5 per 15 min | `LOGIN_FAIL_LIMIT` |
| Failed logins per email from any address | 20 per 15 min | `LOGIN_EMAIL_FAIL_LIMIT` |
| Login attempts per client address | 100 per 15 min | `LOGIN_IP_LIMIT` |
| Signups per client address | 20 per hour | `SIGNUP_IP_LIMIT` |
| Password reset requests per client address | 20 per hour | `RESET_IP_LIMIT` |
| Reset emails per email and client address | 5 per hour | `RESET_EMAIL_LIMIT` |
| Ingest requests per key | `rate_limit_per_sec` of the key (500/s) | per key |

Limited requests get `429` with `Retry-After`.

### Client addresses

Browsers talk to the web app, which proxies `/api/*` to the query API. The
production web server is started with `peer-ip.cjs`, which records the TCP
peer of each request in a header that clients cannot set; the proxy sends it
on as `X-Forwarded-For` (after any inbound hops when `TRUST_PROXY_HEADERS=true`).
The query API reads `X-Forwarded-For` only when the request comes from an
address in `TRUSTED_PROXIES`, walking it from the right and stopping at the
first address that is not a trusted proxy. Anything a client writes into
the header itself is therefore ignored. Without `TRUSTED_PROXIES` the API uses
the direct peer.

## Cross-site request forgery

The session cookie is `SameSite=Lax`, and the query API additionally:

1. requires `Content-Type: application/json` on every state-changing
   request, which a cross-site HTML form cannot send without a CORS preflight
   (the API never answers preflights);
2. refuses state-changing requests whose `Origin` is not `APP_URL` (or one of
   `CSRF_TRUSTED_ORIGINS`), and any that the browser marks
   `Sec-Fetch-Site: cross-site` or `same-site`. The web proxy forwards both
   headers.

Source map uploads authenticated with an upload token (`Authorization:
Bearer`) skip the session and these checks; browsers never attach that
header on their own.

## Browser security headers

The web app sends on every page:

- `Content-Security-Policy` with a per-request nonce:
  `script-src 'self' 'nonce-…' 'strict-dynamic'`, `object-src 'none'`,
  `base-uri 'self'`, `form-action 'self'`, `frame-ancestors 'none'` and
  `connect-src 'self'`. Styles allow `'unsafe-inline'` (React style
  attributes and the replay player need it) and images and fonts may come
  from any https/http host so replays of other sites render.
- `Strict-Transport-Security` (`HSTS_MAX_AGE`), `X-Content-Type-Options: nosniff`,
  `X-Frame-Options: DENY`, `Referrer-Policy: strict-origin-when-cross-origin`,
  `Permissions-Policy` and `Cross-Origin-Opener-Policy: same-origin`.

API responses carry `nosniff`, `X-Frame-Options: DENY`, a `default-src 'none'`
CSP, `Referrer-Policy: no-referrer` and `Cache-Control: no-store`.

## Ingest and CORS

Ingest keys are public: they ship in browser bundles and can only write data
into one project. To stop other sites from using a key, owners can list the
project's **allowed origins** in Settings (exact origins or `https://*.example.com`).

- With an empty list, any origin may send data (the default, so the browser
  SDK works out of the box).
- With a list, a request whose `Origin` is not on it gets `403` and nothing
  is stored. The check runs on the server because CORS-simple requests (the
  SDK's `text/plain` beacons) reach the server even when the browser would
  hide the response. Allowed origins are echoed in
  `Access-Control-Allow-Origin` with `Vary: Origin`.
- Requests without an `Origin` (servers, collectors, gRPC) are not affected,
  so this is a browser control, not authentication.

Changes apply immediately: the project's keys are dropped from the gateway's
Redis cache.

### Size limits

| Input | Limit |
| --- | --- |
| Ingest HTTP body | 5 MiB |
| OTLP/gRPC message (after decompression) | 16 MiB (`OTLP_GRPC_MAX_RECV_MIB`) |
| Replay chunk | 2 MiB compressed, 16 MiB after gunzip |
| Source map upload | 50 MiB per request, multipart parsed in 8 MiB of memory |
| JSON bodies of the query API | 16 KiB (4 KiB for project names) |
| Allowed origins | 50 per project, 255 characters each |

## Outgoing requests (SSRF)

Alert webhooks and Slack URLs are entered by users, so Pulse never lets them
reach internal networks:

- When a channel is saved, the host is resolved and the URL is refused if any
  address is loopback, private (RFC 1918), carrier-grade NAT, link-local
  (including the cloud metadata address `169.254.169.254`), IPv6 unique-local
  or link-local, multicast, NAT64 or another reserved range. URLs with
  credentials are refused too.
- When a notification is sent, the dialer resolves the host again and connects
  only to the checked address, so a name that later resolves to an internal
  address (DNS rebinding) is refused at connect time.
- Redirects are followed at most three times and each target goes through
  the same dial-time check. Proxy environment variables are ignored.
- `OUTBOUND_ALLOWLIST` (hostnames and CIDRs) exempts destinations; it exists
  for development and tests only.

Failed deliveries (network errors, `429` and `5xx`) are retried three times
with exponential backoff starting at one second, honouring `Retry-After` up to
30 seconds. Blocked destinations and other `4xx` answers are not retried.

## Roles

Organizations have **owners** and **members**; projects inherit the
organization role (or a direct project role).

| Action | Member | Owner |
| --- | --- | --- |
| Read traces, issues, replays, metrics, releases, alerts | yes | yes |
| Resolve issues, create and edit alert rules | yes | yes |
| Create, delete or test alert channels | no | yes |
| Create, rotate or revoke ingest keys; set allowed origins | no | yes |
| Create or revoke source map upload tokens | no | yes |
| Rename a project | no | yes |
| Create a project in the organization | no | yes |
| Invite, remove members, change roles | no | yes |
| Read the audit log | no | yes |

Members cannot create projects inside an organization because projects come
with ingest keys and decide where data goes. Every signup gets an organization
of its own, so anyone can still create projects there. Alert rules stay open
to members because they only choose among channels an owner already approved. The last owner of an organization cannot
be removed or demoted.

## Audit log

Security-relevant actions are stored in the Postgres `audit_log` table with
the actor, client address and details, and shown to owners under
**Settings → Audit log**:

`login.success`, `login.failure`, `login.blocked`, `signup`, `logout.all`,
`key.create`, `key.rotate`, `key.revoke`, `upload_token.create`,
`upload_token.revoke`, `invite.create`, `invite.revoke`, `invite.accept`,
`member.remove`, `member.role`, `project.create`, `project.update`
(including allowed origins), `alert_channel.create` and `alert_channel.delete`.

Logins belong to the user rather than an organization; an organization's log
includes the logins of its current members. Failed logins are attributed to
the account whose email was tried. Writing the log never fails the request; a
failed write is logged.

## Secrets in logs

- The query API logs one line per request with sensitive query parameters
  (`token`, `key`, `secret`, `password`, `code`, `signature`) replaced by
  `REDACTED`; headers and cookies are never logged, and values are quoted so
  a request cannot forge log lines.
- Ingest keys are never logged. Audit entries show only a key prefix.
- Without `SMTP_ADDR`, emails (including their links) are written to the log
  so local development works without a mail server. Always configure SMTP in
  production.

## Dependency and code checks in CI

- `govulncheck` for the services and the Go demo API: fails on any known
  vulnerability reachable from our code.
- `pnpm audit --audit-level high`: fails on high or critical advisories.
  `GHSA-vfj7-8cjw-p6xm` (braces, via Tailwind and the Next.js lint plugin at
  build time only) has no fixed version and is ignored in `package.json`.
  `postcss` and `sharp` are pinned to patched versions with pnpm overrides.
- `golangci-lint` runs `gosec` with every rule except G115 (integer
  conversions bounded by the data model). The few remaining suppressions are
  marked inline with the reason.
