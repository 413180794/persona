# Bounded work-context collection

The collection routes share the existing owner `/api` and explicitly enabled legacy `/agent/v1` account boundary. This skill uses the latter through `ol.sh`; it never enables the listener. Check the live `GET /agent/v1` catalog before calling these routes. Select an account with `lark-connector_ACCOUNT`, not a field inside the collection specification.

## API workflow

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/context/capabilities` | Supported source, limits and known gaps |
| POST | `/context/jobs` | Create or reuse an idempotent job; returns HTTP 202 and `{job}` |
| GET | `/context/jobs/JOB_ID` | Read state, progress, specification and coverage |
| POST | `/context/jobs/JOB_ID/cancel` | Cancel local collection work |
| GET | `/context/jobs/JOB_ID/bundle` | Bounded message excerpts and source references |
| POST | `/context/jobs/JOB_ID/query` | Filter revalidated messages by text, chat or sender |
| POST | `/context/jobs/JOB_ID/read` | Read full unchanged text for explicit source references |
| GET | `/context/jobs/JOB_ID/export` | One JSON page with full unchanged text |

Create a specification file, replacing the example chat ID with the intended chat:

```json
{
  "profile": "im-work-context",
  "window": {
    "from": "2026-10-01T00:00:00+08:00",
    "to": "2026-10-19T00:00:00+08:00",
    "timezone": "Asia/Shanghai"
  },
  "chatIds": ["1234567890"],
  "includeMentions": true,
  "idempotencyKey": "planning-context-october-1"
}
```

```bash
ol GET /context/capabilities
ol POST /context/jobs @context.json
ol GET /context/jobs/JOB_ID
ol GET '/context/jobs/JOB_ID/bundle?maxItems=100&maxBytes=65536'
ol POST /context/jobs/JOB_ID/query '{"query":"project planning","presentationBudget":{"maxItems":100,"maxBytes":65536}}'
ol POST /context/jobs/JOB_ID/read '{"sourceRefs":["PAGE_UUID:0"]}'
```

Replace `JOB_ID` and the entire example reference with values returned by the service. `queued` and `running` require waiting; `needs_auth` requires authentication recovery. `completed`, `partial`, `cancelled` and `failed` may expose already committed evidence, subject to current identity and source checks. A terminal state alone does not make coverage complete.

To resume, submit the same profile, window and source selection with a **new** idempotency key, `resumeFrom` set to the stopped job ID, and fresh collection budgets. Resume accepts `partial`, `cancelled`, `failed` or `needs_auth` jobs under the same identity and source version. A changed account identity cannot resume or read the old job.

## Evidence pages

Every response includes `sourceManifestHash`, `sourcePageCount`, coverage, `validatedAt`, visible `items` and `withheld` states. The manifest describes committed source pages and does not vary with presentation budgets; the page count is not a unique-message count. Raw decoded source payloads are never returned by these routes.

`bundle` and `query` limit each text excerpt to 1,200 Unicode code points and set `textTruncated` when necessary. Use `read` with up to 100 returned `sourceRef` values for full text. `query` accepts optional `query`, `chatIds`, `senderIds`, `presentationBudget` and `cursor`; the text query is a case-insensitive substring match, applied after access and version checks.

Pass a response's `continuation` unchanged as the next request's `cursor`, retaining the same mode and filter. Follow it even when `items` is empty: a response scans at most 1,000 candidate messages. `export` is paginated JSON, not an automatic full-job download. Only the absence of `continuation` ends this presentation; it says nothing about upstream completeness.

The default presentation is 100 items and 256 KiB. Overrides allow at most 1,000 items and 10 MiB per response. `maxBytes` includes all JSON metadata; an insufficient budget returns `CONTEXT_PRESENTATION_BUDGET_TOO_SMALL` rather than an invalid or silently truncated object. Source references remain bound to their stored version: `changed`, `deleted`, `permission_denied`, `unknown` or `restricted` entries do not release saved text. A changed source does not return new text under an old reference. A read that exhausts its 1,000 revalidation requests fails with `CONTEXT_REVALIDATION_LIMIT` (400), one cut off by its 60-second deadline with the recoverable `CONTEXT_READ_TIMEOUT` (504), and one cut off by shutdown with `CONTEXT_CLOSED` (503). None of them is reported as `unknown` entries; retry the first two with a smaller `maxItems` and the last once the server is back.

## Job limits and retention

At most 4 jobs per account may be `queued` or `running` at the same time. A fifth create returns HTTP 429 `CONTEXT_ACTIVE_JOBS_LIMIT` (recoverable: cancel or wait for an active job, then retry). Terminal jobs do not count against the cap; reusing an idempotency key also returns the existing job instead of creating another.

Terminal jobs (`completed`, `partial`, `failed`, `cancelled`, `needs_auth`), their units and stored artifacts are deleted automatically after a TTL measured from `finishedAt`; the sweep runs at service start and hourly. The TTL is configured with `lark-connector_CONTEXT_JOB_TTL_DAYS` (whole days, at most 36,500) and defaults to 7 days; `0` disables deletion. A `needs_auth` job is never re-run: after re-authenticating, resume it with `resumeFrom` before it expires. Cancelling a job that is already terminal is a no-op. An expired ancestor that a still-existing job could resume from (via `resumeFrom`) is protected until its descendant is also deleted. Resume chains deeper than 32 jobs were never accepted. Deletion is per account schema and never touches active jobs.

## Agent CLI (preferred)

`removedctx job` drives these routes from the bundled `plugins/lark-connector/server/removedctx.mjs` with Node's own `fetch` (no Bash, curl or JSON files needed). It reads `lark-connector_AGENT_BASE`, `lark-connector_AGENT_TOKEN` and `lark-connector_ACCOUNT` (or `--account`); run it as `node --env-file=.env plugins/lark-connector/server/removedctx.mjs job ...`.

```bash
removedctx job collect --chat 7012345678901234567 --mentions --days 30 --wait --select job.id,job.state,job.coverage
removedctx job collect --mentions --from 2026-09-01 --to 2026-10-01 --dry-run
removedctx job status JOB_ID --wait --wait-timeout 600
removedctx job bundle JOB_ID --max-items 50 --select items.sourceRef,items.text,continuation
removedctx job query JOB_ID --query "release plan" --chat 7012345678901234567 --sender 7000000000000000001
removedctx job read JOB_ID --refs JOB_ID:12,JOB_ID:15
removedctx job export JOB_ID --cursor CONTINUATION
removedctx job cancel JOB_ID
removedctx job capabilities
```

- `collect` builds the specification from flags: `--chat ID` (numeric chat ids only, repeatable; selectors such as `active:N` or chat names are resolved only by `harvest`, so list their ids with `removedctx harvest --sources chats --chat <selector> --days 7 --json --select places.id,places.name`), `--mentions` (`includeMentions`), `--days N` (1–93, default 30) or `--from`/`--to` (ISO or epoch ms; `to` defaults to now), `--tz` (default the machine's zone), `--resume-from`, `--max-requests` and `--max-wall-time` (`collectionBudget`). Without `--idempotency-key` the key is a hash of the specification, so retrying the identical spec (same explicit `--to`) returns the same job. `--dry-run` prints the validated specification and sends nothing. `--spec FILE` still accepts a full specification and cannot be mixed with the inline flags.
- `bundle` and `export` take `--max-items`, `--max-bytes`, `--cursor`; `query` adds `--query`, `--chat`, `--sender`; `read` takes `--refs` (at most 100). Each also accepts `--input FILE` with the HTTP request body instead of the flags. Pass `continuation` unchanged as `--cursor`, including after an empty page.
- `--wait` (on `collect` and `status`) polls every second for up to `--wait-timeout` seconds (default 300, at most 3,600) and writes one plain progress line to stderr whenever the state or unit counts change (`state=… units=done/total failed=… items=… pages=… requests=…`, the first line prefixed `job=<id>`). `completed` exits 0. `partial`, `failed`, `cancelled` and `needs_auth` print the job on stdout, add a `JOB_<STATE>` error line with a resume hint on stderr and exit 1. A wait timeout exits 10 and SIGINT exits 130 (both with `jobId`); neither cancels the server job, so continue with `removedctx job status JOB_ID --wait`.
- Every command accepts `--select a.b,c.d` to keep only those JSON paths (arrays are mapped over, unknown paths omitted), and `--json` is accepted as a no-op. Failures leave stdout empty and write one line `{"error":{code,message,hint?,status?,recoverable?,retryAfterSeconds?,traceId?},"jobId"?}` to stderr. Exit codes: 0 success, 1 waited job not completed or local error, 2 usage or invalid request (including `CONTEXT_IDEMPOTENCY_CONFLICT`, `CONTEXT_SCOPE_DENIED`, `CONTEXT_INVALID_CURSOR`, `CONTEXT_PRESENTATION_BUDGET_TOO_SMALL`), 3 configuration or sign-in, 4 account not synced, 5 server or network, 10 wait timeout, 130 interrupted. `removedctx schema job collect` prints the flags, output and exit codes as JSON.

## Repository CLI

The older repository CLI below stays supported; prefer `removedctx job` above. It runs on the host from a complete repository checkout with Bash, curl and the bundled `ol.sh`. Build with `npm run build` first, or use `context:dev` instead of `context` during development. This is not a standalone package or a command installed inside the runtime Docker image; the backend itself may run in Docker. The CLI uses the helper's existing base URL, token lookup and `lark-connector_ACCOUNT` handling.

```bash
npm run --silent context -- capabilities
npm run --silent context -- collect --spec context.json --wait --wait-timeout 300
npm run --silent context -- status JOB_ID
npm run --silent context -- bundle JOB_ID
npm run --silent context -- query JOB_ID --input query.json
npm run --silent context -- read JOB_ID --input refs.json
npm run --silent context -- export JOB_ID --input page.json
npm run --silent context -- cancel JOB_ID
```

`collect` requires a specification file and a stable idempotency key. `query` and `read` input files use the API request bodies; `read` requires `--input FILE` and exits 2 before any request without it. `bundle` and `export` accept an optional file containing `maxItems`, `maxBytes` and/or `cursor`. Export returns one JSON page on stdout and preserves `continuation`. Use `--silent` to suppress npm's own banner when consuming machine output.

`--wait` polls internally, defaults to 300 seconds and accepts up to 3,600 seconds. Only `completed` exits 0; `partial`, `cancelled`, `failed` and `needs_auth` return the job with exit 1. A wait timeout exits 3 with a recoverable error and `jobId`; SIGINT exits 130 and stops local waiting without cancelling the server job. Resume status inspection with the returned ID. Per-request `OL_TIMEOUT` must be greater than 0 and at most 600 seconds. Input JSON files are limited to 1 MiB and HTTP output to 16 MiB. The success JSON envelope is the only thing written to stdout; while waiting, deduplicated progress lines (`state=… requests=… pages=… items=… units=done/total failed=…`, plus `reason=…` when present) go to stderr. A `--wait` that ends in a non-`completed` terminal state still prints that job envelope on stdout (exit 1). Every other failure writes its error JSON to stderr and leaves stdout empty, so stdout stays safe for `| jq`.

## Collection contract

- A request names explicit chats and/or mentions of the current user. It does not imply all project chats or all user activity.
- The fixed time window is `[from, to)`, with explicit ISO timestamps and an IANA timezone. Windows must be positive and no longer than 93 days; at most 100 numeric chat IDs are accepted.
- Collection budgets bound upstream requests, stored source payload bytes and elapsed wall time. Presentation budgets separately bound the items and bytes returned to a caller. Truncating a presentation never changes source coverage.
- Active jobs take turns one page at a time: a worker serves the job it served least recently first. A long job does not hold back a newer one, so concurrent jobs can finish in any order; each job's results do not depend on that order.
- Wall time starts when the job is created and includes waiting and application downtime. Resume creates a new job with a fresh budget; it is not an extension of the expired job's budget.
- Repeating the same idempotency key with the same normalized specification returns the same job. Reusing the key with a different specification is a conflict.
- `profile` defaults to `im-work-context`. The `harvest` profile and its fields (`sources`, numeric `chats`, `noise`, `bodies`; windows up to 92 days) are off unless the server sets `lark-connector_CONTEXT_HARVEST_JOBS=1`: creating one otherwise returns `CONTEXT_PROFILE_DISABLED` (400), and an IM specification that sets any of those fields is invalid input. `GET /context/capabilities` lists both profiles under `profiles`, each with `enabled`; the harvest entry names the sources a durable job can collect (`sources`, and `bodies` for those that also allow `bodies:true`). When enabled, a harvest create is refused with 400 `CONTEXT_WINDOW_IN_FUTURE` (window ending over 2 minutes from now), `CONTEXT_WALL_BUDGET_TOO_SMALL` (`collectionBudget.maxWallTimeSeconds` under 60; the minimum is in `error.details.minWallTimeSeconds` and in the capabilities entry), `CONTEXT_SOURCE_NOT_DURABLE` (an explicit source, or `chats` without `sources`, that is `ineligible` or `unavailable`; listed in `error.details.sources`) or `CONTEXT_NO_DURABLE_SOURCES` (no source left when `sources` is omitted; reasons in `error.details.excluded`). Omitted `sources` resolve to every eligible source and are stored in the job's spec; a `resumeFrom` create takes any omitted `sources`, `chats`, `noise` and `bodies` from the job it resumes, and an omitted `noise` or `bodies` matches an explicit `false`. A window that ends ahead of the server clock is read only once it has ended. These checks and the default source resolution run before the idempotency key is looked up, so repeating a create after the server's flag or sources changed can be refused; `GET` the job by its ID instead. Harvest items are gated when collected; bundle, query, read and export do not serve this profile, and `POST /context/read` reads an item's full text by its `id`. Bundle, query, read and export of a non-IM job return `CONTEXT_PROFILE_UNSUPPORTED` (400); a specification that plans no collection units returns `CONTEXT_NO_UNITS` (400).

## Evidence and recovery

Each page stores the complete decoded upstream response, projected items, source hash and provenance together with its checkpoint and discovered units in one transaction. This is a decoded response record, not the original wire bytes. A failed transaction cannot advance the checkpoint alone.

Normal interruption can resume from committed checkpoints. An uncommitted page may be read again. A changed source adapter version rejects an old cursor instead of interpreting it with new logic. Explicit cancellation and an account identity change fence late worker results; refreshing credentials for the same identity does not create a different principal.

The account identity and the existing owner authentication remain the access boundary. This foundation does not introduce a new credential, enable the legacy API, or change OLAP permissions.

## Reading coverage

`completed` describes the job state, not complete coverage of a month. Interpret `coverage` separately:

- `complete_within_declared_scope`: the source supplied evidence for the entire declared scope.
- `partial`: a known gap, exhausted budget or failed unit prevents full coverage.
- `unknown`: the source cannot establish the denominator, retention boundary or completeness.

An exhausted cursor alone cannot prove a complete month. Empty or missing evidence is unknown. Keep coverage evidence attached to any derived summary and distinguish partial results from an empty source.

## MCP workflow

The bundled plugin maps five tools to the same service; each accepts an optional `accountId`:

- `context_collection_capabilities`: inspect source support and limits.
- `collect_context`: pass the collection specification as `spec`; returns `{jobId,state}` immediately without waiting or retrying. Retain the idempotency key for the same intent.
- `get_context_collection`: pass `jobId` for one state snapshot.
- `cancel_context_collection`: pass `jobId` to cancel local work.
- `read_context_collection`: pass `jobId` and a strict `request` object. Its `mode` is `bundle`, `query`, `read` or `export`; all other fields match that mode's HTTP input described above.

For example, a bounded bundle read uses:

```json
{
  "name": "read_context_collection",
  "arguments": {
    "accountId": "4",
    "jobId": "REPLACE_WITH_JOB_UUID",
    "request": { "mode": "bundle", "maxItems": 100, "maxBytes": 65536 }
  }
}
```

Continue with `request.cursor` set to the returned `continuation`, keeping account, job, mode and filters unchanged. Empty pages can still have a continuation. The plugin preserves partial coverage, withheld states and recoverable errors; it does not interpret missing or changed content as a successful read.

`lark-connector_MCP_READ_ONLY=1` permits these operations because they do not write Lark data. Collection and cancellation do change persistent local job state, and their MCP annotations therefore use `readOnlyHint:false`. Generic POST and `reply_to_message` remain blocked. Query and read use dedicated read-only POST routes; no tool accepts an arbitrary path, method or header.

## IM source boundaries

The IM source scans all enumerable positions in explicitly selected chats, within the collection budget, rather than stopping at the local history limit. It follows discovered threads beyond the normal thread-preview limit. Old roots may lead to replies created inside the requested window; complete thread and quoted-message context can include items marked `outOfWindow`.

The selection window uses message creation time. Editing an older message inside the window does not independently cause it to be collected. Every new job performs a cold enumeration; idempotency and checkpoint recovery do not provide a cheap change feed or cross-job cache.

Mentions use the current user's mention-history search, with cursor paging. A mention in a chat outside the explicit selection brings in that hit and its thread context, not the entire chat. Retention limits, unavailable positions, missing replies, stalled cursors and unknown snapshot boundaries remain visible in coverage evidence.

Plain text and supported rich text preserve adjacent inline text and paragraph breaks. Encrypted, recalled, burned, ephemeral or otherwise restricted content is withheld. Card, merged-forward and media bodies remain unsupported placeholders. Source access and message versions must be revalidated before returning saved text; an absent response or timeout is `unknown`, and a permission error is not evidence of deletion.

Every actual upstream request consumes the request budget. Collector reads skip the ordinary asynchronous raw archive: their complete decoded pages are instead committed atomically with the collection checkpoint. Existing non-collector Gateway calls retain their original archive behavior.
