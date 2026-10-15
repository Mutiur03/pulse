# Browser demo

A tiny SPA that uses `@pulse/browser` against the local stack.

```bash
make compose-up                      # from the repo root
pnpm --filter @pulse/browser build
pnpm --filter @pulse/browser-demo start
# open http://localhost:5173 (use ?ingest=http://host:8080 for another gateway)
```

Click around, then look at the data:

```bash
curl -s localhost:8123 --data "SELECT event, distinct_id, session_id, trace_id FROM events ORDER BY timestamp DESC LIMIT 10 FORMAT PrettyCompact"
curl -s localhost:8123 --data "SELECT span_name, service_name, session_id, duration_ns FROM spans WHERE service_name = 'browser-demo' ORDER BY timestamp DESC LIMIT 10 FORMAT PrettyCompact"
curl -s localhost:8123 --data "SELECT exception_type, message, trace_id FROM errors ORDER BY timestamp DESC LIMIT 5 FORMAT PrettyCompact"
```

"Pay" shows the `traceparent` and `baggage` headers the fake `/api/pay`
route received. Typing in the email field is never captured.

## Source maps

The build is minified. To see original files and lines in issues, create an
upload token under Settings, then inject debug ids and upload the maps for the
demo's release (`browser-demo@dev`, or `?release=` in the page URL):

```bash
pnpm --filter @pulse/browser-demo build
PULSE_PROJECT=1 PULSE_UPLOAD_TOKEN=pul_... pnpm --filter @pulse/browser-demo sourcemaps
```

Click "Throw error": the issue's stack shows `src/main.ts` with the code
around the throw. Maps uploaded after an error are applied when the issue is
opened.
