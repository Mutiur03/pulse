# Source maps

Minified browser stacks are resolved to original files, lines, functions and code with source maps uploaded per release:

1. Set `release` in the browser SDK (for example `web@1.4.2`).
2. Create an upload token under **Settings > Source map upload tokens**. Upload tokens are secret and project-scoped, unlike public ingest keys, and can be revoked there.
3. After each build, inject debug ids and upload the maps, with `pulse-cli` (any bundler) or `@pulse/vite-plugin`:

```bash
cd go && go run ./cmd/pulse-cli sourcemaps inject ../dist
go run ./cmd/pulse-cli sourcemaps upload --release web@1.4.2 --url-prefix ~/assets \
  --url http://localhost:8081 --project 1 --token pul_... ../dist
```

`inject` appends a small snippet and a `//# debugId=` comment to each script (after all code, so no mapping moves) and writes the same `debugId` into its map; the SDK sends the ids of the scripts in each stack. Uploads go to `POST /v1/projects/{id}/releases/{release}/sourcemaps` (multipart, `Authorization: Bearer pul_...`, 50 MB per request, 20 MB per map) and are stored in object storage (`S3_ARTIFACT_BUCKET`, default `pulse-artifacts`), with releases and artifacts in Postgres (`schemas/postgres/007_releases.sql`).

The stream processor symbolicates JavaScript errors before grouping them: frames are matched to a map by debug id, falling back to the file URL within the error's release, and parsed maps are kept in an in-memory LRU. Issues are fingerprinted on original files and functions, so a new build with new bundle names and minified identifiers keeps the same issue. Both the raw stack and the resolved frames are stored. Maps uploaded after an error are applied when the issue is opened, or with **Reprocess**; that does not regroup existing events. The **Releases** page lists each release's artifacts.
