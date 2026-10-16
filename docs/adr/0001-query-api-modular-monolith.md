# ADR 0001: query-api as a modular monolith

- Status: Accepted
- Date: 2026-10-17

## Context

query-api serves all 78 REST operations through one `api.Server` struct with
40+ dependency fields and one generated `gen.ServerInterface`. The domain
packages behind it (`auth`, `orgs`, `projects`, `keys`, `issues`, `alerts`,
`dashboards`, `insights`, `governance`, `audit`, `artifacts`, `replay`, …) sit
side by side with no rules about who may use whom, and HTTP mapping,
validation and persistence are mixed in the handlers.

The pipeline services (ingest-gateway, stream-processor, alerting-worker,
governance-worker) and pulse-cli stay separate deployables; this ADR does not
change them beyond their imports.

## Decision

Organise the code behind query-api as modules under `go/internal/modules/`,
one per bounded context:

| Module | Owns |
|---|---|
| identity | sign-in, sign-up, sessions, email tokens, orgs, members, invites |
| projects | projects, allowed origins, ingest keys |
| telemetry | trace, log and metric queries |
| analytics | events, trends, funnels, retention, paths, persons |
| dashboards | saved dashboards |
| issues | issue list, detail and status |
| alerts | alert rules and notification channels |
| replay | session replay listing and playback |
| sourcemaps | releases, upload tokens, source map uploads |
| governance | data settings, scrubbing, deletion requests, audit log |

Each module uses the same layout:

```
modules/<name>/
  module.go          public API: New(Deps) *Module, Register(router), exported types
  domain/            entities and rules; no I/O
  ports/             interfaces the module needs (repositories, readers, senders)
  service/           use cases; depend on domain and ports only
  adapters/httpapi/  strict-server handlers for the module's OpenAPI tag
  adapters/httpapi/oapi  generated per-tag interface (do not edit)
  adapters/postgres/ repositories
  adapters/clickhouse/ readers, where the module owns the queries
```

Small modules may leave out layers they do not need.

### Rules

1. Code outside a module (other modules, `cmd/*`, workers, shared packages)
   imports only the module root. `domain`, `ports`, `service` and `adapters`
   are private.
2. Inside a module: `domain` imports nothing from the module; `ports` imports
   `domain`; `service` imports `domain` and `ports`; adapters may import all
   three; only the root wires them together.
3. Modules call each other through interfaces exported by the module root or
   through events; they never share tables. Issue lifecycle events go through
   a transactional outbox to Kafka.
4. Modules never import `internal/api`, which is now only the composition
   root that mounts every module's routes behind the shared middleware.
5. Dependencies are passed explicitly through constructors (`New(Deps)`);
   no package-level state.

`internal/archtest` checks rules 1, 2 and 4 on every `go test` run.

### API generation

Every operation in `schemas/openapi/api.yaml` carries a tag naming its
module. oapi-codegen v2 generates the shared models plus, per module, a
strict-server interface limited to that tag (`include-tags`) into
`adapters/http/oapi`. Handlers return typed responses; error statuses the
spec does not list (401, 429, 503, …) are returned as `apikit.Error` and
written by the shared error handler in the existing JSON format, so
responses stay byte-compatible. Multipart and streaming operations may use
the per-tag chi-server interface instead. Shared models stay in `internal/api/gen`.
During the migration, operations whose module had not moved yet were served
by a legacy chi server generated with `exclude-tags`; it has been removed.
The identity module uses the chi-server interface only, because its
handlers set cookies and `Retry-After` headers. Per-module generator
configs live in `schemas/openapi/modules/`.

`internal/apikit` holds the HTTP helpers every module adapter shares:
`apikit.Error`, project access, time ranges and the strict handler options.
A route test in `cmd/query-api` checks that every spec operation is served
exactly once.

URL paths and payloads do not change.

## Consequences

- Each module can be read, tested and changed on its own; cross-module
  coupling is visible in `module.go`.
- Handlers become plain functions from request objects to response objects,
  testable without HTTP plumbing.
- More packages and some wiring code in the composition root.
- Postgres tables are owned by convention and review, not separate schemas.
- `store/ch` stays a shared ClickHouse gateway used by the pipeline services;
  modules depend on it only through their own ports.

## Migration

One module per pull request, behaviour unchanged, E2E green: move the
domain and repository code into the module, add the service layer and the
strict handlers, generate the module's interface, exclude its tag from the
legacy server and delete the old handlers. The outbox for issue events comes
with the issues module: `issues.Record` writes the event to
`issue_outbox` in the same transaction, and a relay in stream-processor
publishes pending rows to Kafka.

## Shared kernel

Some packages stay outside `internal/modules` because the pipeline services
use them too: `internal/alerts` (rule engine, shared with alerting-worker),
`internal/governance` (scrubbing and retention, used by ingest),
`internal/projects`, `internal/keys`, `internal/auth`, `internal/orgs`,
`internal/audit`, `internal/mail` and `store/ch`. Modules reach them through
their ports. `internal/api` keeps only HTTP middleware and the shared models.
