# Platform: auth, accounts, events, settings, host ops

Paths are relative to `/agent/v1`. Calls use the `ol` helper from SKILL.md.

Repository CI runs Test and Release (Server, macOS, Windows) (`.github/workflows/test-and-release.yml`) daily at 02:00 Asia/Shanghai (18:00 UTC) on the default branch, or manually via Run workflow, with no pull-request trigger. Manual runs use the selected ref; select `main` for normal releases. All three jobs use Node.js 24. A single Linux job first installs the independent desktop dependencies, typechecks, runs the full desktop Vitest suite, and builds shared JavaScript, icons and static assets once. It archives `desktop/out` as `desktop-common.tar.gz` and stages it in a draft GitHub Release named `lark-connector · YYYY-MM-DD HH:mm:ss（北京时间）`, with tag `build-YYYYMMDD-HHMMSS-<github.run_id>`. The timestamp comes from the original workflow run creation time, so a rerun on another day retains its tag. Linux exports that tag as a job output for both native jobs; reading the run metadata requires `actions: read`. The root Vitest configuration excludes `desktop/`; desktop tests run as explicit steps in the same Linux job and must not be omitted when validating both packages.

After desktop common checks pass, the same Linux job installs the root dependencies and runs the server quality checks and complete PostgreSQL suite without sharding, builds once, checks the bundle budget, then packs and uploads the checked server output and both Linux dependency bundles to that same draft. Each dated release is self-contained: dependencies are included even when an older release has the same lock hash. macOS then Windows install their own packaging dependencies and download the same shared desktop archive from the authenticated Release. Each runs `npm run test:native -- --expected-platform=darwin|win32` in its actual Electron runtime (native icon decoding, sandboxed preload, isolated renderer, IPC and navigation), then `npm run pack:mac` or `npm run pack:win` without rebuilding shared code or repeating Vitest/typechecks. Native smoke tests use a temporary user-data directory and loopback HTTP server. Local `dist:mac` and `dist:win` still build before packaging; `pack:*` requires an existing `out/`.

`npm run test:postgres` runs up to four isolated Vitest workers (four on a two-core runner), with separate throwaway databases. It creates a fresh Node compilation cache alongside the run's protocol/database fixtures and removes it in the same cleanup; this is bytecode reuse within one run, not a shared module instance or persistent Actions/download cache. Server shutdown and handover subprocesses inherit that cache. `--maxWorkers=2` overrides concurrency; `NODE_DISABLE_COMPILE_CACHE=1` disables Node's cache for comparison. The timing reporter prints the slowest 15 files and cumulative phase times to the log and `GITHUB_STEP_SUMMARY`; overlapping worker times must not be interpreted as wall time. No test artifacts are uploaded. The latest solo two-core run passed the full suite in 738.98s (about 18.9% below the original local baseline). This is not a verified 20-minute GitHub run; see `docs/performance/20261024-test-runtime.md`.

macOS adds ARM64 and x64 DMG/ZIP installers, available blockmaps and `latest-mac.yml`; Windows adds its EXE, ZIP, blockmap and `latest.yml`. All stages call `scripts/build-release.mjs stage|server|mac|windows` (no root npm install is needed by this Node-only script on native runners). Finalization checks both Mac architectures, Windows, the shared build and both server dependency bundles, validates the server manifest's commit and bundle names, downloads its bundles and verifies size/SHA-256, then uploads `build-complete.json` and publishes the prerelease without making it Latest. Initial failures leave a draft; retries reuse it. Already published dated assets are preserved on reruns, rather than replaced beneath active downloads. Historical releases/drafts are not deleted.

Only after the complete dated release is published does Windows mirror its verified server bundles and then `latest.json` into `server-builds`, labelled “Server auto-update compatibility channel”. An older run cannot overwrite a compatibility manifest with a newer commit time. A mirror failure leaves the complete dated release available and the existing channel usable where its previous manifest remains; rerun Windows to finish the mirror. The compatibility channel retains its existing limited bundle retention, while dated releases retain their own artifacts.

Default server update discovery (`lark-connector_UPDATE_TAG=server-builds`) now paginates releases, ignores drafts and dates without `build-complete.json`/`latest.json`, and chooses the greatest date/run-ID tag. With no completed dated build it falls back to `server-builds`; other explicit tags remain pinned. New servers download from the selected dated release; old servers continue reading the mirrored manifest. API failures are reported rather than silently selecting an old build (a missing list endpoint may fall back for compatibility). The manifest schema and downloaded size/SHA-256 checks remain unchanged. This changes no Agent API route, field or header.

Release notes link to the workflow and source commit. Shared builds and installers use Release assets, never Actions artifacts. The workflow does not restore or save npm, Electron or electron-builder caches; each hosted job downloads its own dependencies/tools. Job-local temporary files disappear with the runner. Existing Actions artifacts/caches are not migrated or deleted. The desktop app still reads its configured server's `/desktop-updates/`, not the private GitHub Release directly; installer versions remain semantic versions independently of date-based release titles.

Portable updater, process-lock, key-generation and signature-verification tests run in the Linux desktop suite, along with POSIX publishing/install script and permission-bit tests. The POSIX cases remain gated off when developers run Vitest on Windows; lock fixtures use Node child processes. Validate workflow edits with `actionlint` as well as repository quality gates; YAML parsing alone does not validate expression contexts. Build and Pack Docker Images remains independent and manual-only. Runs are serialized without cancelling an active release. This changes CI/release operations only, with no Agent API changes.

Pure model/protocol/static-render and local-function-fake domain tests are grouped in `test/**/unit-*.test.ts(x)` entries, which import `.cases.ts(x)` modules. Run the containing entry (optionally with `-t` and the original case-module name); running a `.cases` path directly does not collect tests. Each module is imported inside a named suite. Keep module mocks, DOM environments, lifecycle hooks and mutable global fixtures in separately isolated test files. `test/scripts/grouped-tests.test.ts` checks that every case module is referenced exactly once and imports only the basic test/assertion API plus local `vi.fn`/`vi.mocked` calls. Module mocks, spies and timer/global replacements remain forbidden in these groups. Grouped tests also pass a shuffled-order validation; do not remove this boundary just to reduce file count.

Linux CI verification uses `npm run test:postgres` with an isolated disposable PostgreSQL instance. Process lifecycle tests clone the prepared empty schema into separate databases; they do not share writable data. Large binary round trips compare every byte with native `Buffer.equals`, avoiding generic object deep-comparison overhead. Document functional tests use 1000 blocks, above the 400-block deferred-render threshold, while dedicated scale tests retain 2000/3000/5000-block fixtures. UI tests wait for observable asynchronous rendering and provide browser APIs missing from jsdom. Keep these assertions and isolation boundaries when optimizing tests; local timings do not predict hosted-runner duration. Calendar past-week invalidation tracks completion generations separately from wall-clock freshness, so a failed invalidation in the same millisecond as a successful one remains unsettled. Meeting-room avatar rate-limit tests inject the ticket clock: exhausted-budget assertions run without elapsed time, and advancing 50 ms verifies that exactly one preflight token is replenished. These fixes do not change Agent routes, fields, permissions, or headers.

Server self-updates are an owner settings feature (Settings → Server / 服务器设置, owner-only `/api/server/update`), not part of `/agent/v1`: the agent API cannot check, apply or configure them, so point the user to that page for the switches. With shell access to the deployment host, an agent sets up the deployment side (supervisor entry point, `lark-connector_UPDATE_TOKEN`, build directory) and verifies it with `.agents/skills/server-auto-update` (`scripts/check-deployment.sh`), after every deployment. Automatic updates install completed dated builds (or the `server-builds` compatibility fallback) through the supervisor's seamless switch; the off-by-default "远端没有更新包时在本机编译" switch also builds the branch on the server when the channel has no newer package. Build cleanup only removes marked source-build workspaces; unrelated files in a custom build directory are preserved. Cancellation prevents new build commands and waits for an interrupted command to exit before cleanup. Details: `deploy/README.md`, "Automatic updates".

On Windows Git Bash/MSYS, the helper sends inline JSON through curl's standard input pipe without a temporary file, preserving UTF-8 Chinese, emoji and newlines. Other hosts retain the direct curl invocation. JSON values beginning with `@` remain data; the separate `@file.json` body argument retains its file-input meaning. HTTP errors keep curl's nonzero exit status and response body.

- ⚠ marks a write other people can see, or one that cannot be undone. Confirm it first.
- _(unverified)_ marks a write the code says was never run against the real Lark gateway. Read it back after use.

## Contents

Message sender signatures preserve image-only and Unicode-icon-only preview parts even when their text is empty, including multiple leading icons. Empty text-only parts and published OLK key text remain hidden. This is a client rendering fix with no Agent API changes.

The client startup JavaScript budget is 2,000,000 uncompressed bytes (2000 kB), covering the entry, shell and their static imports. The nightly bundle gate continues to enforce this limit before publishing.

1. Token and endpoints
2. Accounts
3. Identity and sync health
4. Errors
5. Resource URLs
6. Live events (`/events`)
7. Re-login (QR)
8. Status, do-not-disturb, signature
9. Translation settings
10. Account, notification, privacy and display settings
11. FLiNG Trainer switches (`/settings/local`)
12. Emoji, stickers, devices, file safety
13. Host maintenance
14. Owner-only routes

## 1. Token and endpoints

- The agent API is the "legacy agent API". Current builds keep it **off** unless the host sets `LARK_AGENT_LEGACY=token`:
  - Off: the agent server does not listen (connection refused), or answers every request with 404 `AGENT_LEGACY_DISABLED`.
  - On (`token`): at least one agent token is mandatory, loopback included; the server refuses to start without one:
    - `lark-connector_AGENT_TOKEN` — full power: every route, including writes that change Lark state (send, settings, joins).
    - `lark-connector_AGENT_READ_TOKEN` — read-only: `GET`/`HEAD`, the `/events` WebSocket, and an explicit allowlist of read-only POSTs (`/context/harvest`, `/context/read`, `/context/jobs` and `/context/jobs/:jobId/{cancel,query,read}`, `/search/page`, `/search/mention-history`, `/documents/reader/export`, `/documents/share/export`, `/documents/base/attachments/export`). The POSTs only use POST because bodies/cursors are large; the context job routes change local job state only, and the export routes start a server-side export task of content the token can read anyway. Three GETs need the full token: `/links/open` and `/conversations/F/qr-code` act as the user, and `/maintenance/export` dumps the selected account's fixed 21 tables.
    - Both may be set together but their values must differ. A read-only setup can set only the read token, but then write tools (e.g. MCP `reply_to_message`, meeting voice) cannot authenticate — use the full token for those clients.
    - Read-only agents (harvest/context collection/search/doc reads) should be configured with the read token; a leaked read token cannot send, grant, sign in or edit anything. It still reads everything the account can, and reads leave the usual traces (a pins GET clears the unread-pin mark), so keep it as secret as the full token.
  - Turning it on is a deployment change; see the README section [Agent 接口（旧，默认关闭）](../../../../README.md#agent-接口旧默认关闭). Tell the user what is needed; do not change the host configuration yourself.
  - The listener must stay host-only. Ports come from `LARK_AGENT_PORT` (agent) and `LARK_SERVER_PORT` (website, local default 3333); the helper uses `lark-connector_AGENT_BASE`.
  - OLAP agent credentials are separate and do not work here.
- Older builds have no switch. There the token is required only when the agent API listens on something other than loopback (compose listens on `0.0.0.0`).
- Once a token is configured, every request needs it, `/health` included. It has nothing to do with owner auth.
- Send it as `Authorization: Bearer …` or `x-lark-connector-agent-token`. A query parameter is not accepted.
- 401 has two codes:

  | Code | `recoverable` | Meaning | What to do |
  | --- | --- | --- | --- |
  | `AGENT_AUTH_REQUIRED` | true | Token missing or wrong | Fix the token |
  | `AUTH_SESSION_EXPIRED` | false | The Lark Web session died | The user must re-scan (section 7). Do not start a login unasked |

- 403 `AGENT_READ_FORBIDDEN` (`recoverable:false`): the request carried the read-only token but hit a route outside its scope (a non-GET outside the allowlist, or one of the three GETs above). It is refused before the body is read or the handler runs, and the refusal is written to the security audit. Do not retry with the same token; switch that client to the full token or drop the write.

- `GET /` lists every route (`{role:"agent",routes:[{method,path}]}`). It is the source of truth when the docs disagree.
- JSON bodies are capped at 11 MiB.
- Raw-byte upload routes have their own limits:
  - Chat files: 100 MB.
  - Drive and Base attachments: 50 MB.
  - Calendar, task, docx media, comment image and approval files: 20 MB.
- Lark gateway calls time out after 30 s, or 60 s for image uploads. The server itself has no request timeout.

The bundled MCP adapter limits successful JSON responses to 16 MiB and backend error bodies to 64 KiB, checking the actual stream even without a trustworthy `Content-Length`. It cancels oversized responses and reports `CONTENT_TOO_LARGE` without truncated content. Use a narrower query or `read_document` with `segment=first`; check backend health/logs for oversized error bodies. A reply may already have succeeded before its response exceeds the budget: reconcile history before any manual retry. HTTP routes and `ol` retain their existing limits. Text attachments decode directly when valid UTF-8; binary content formats are unchanged.

## 2. Accounts

One instance can hold up to 5 Lark accounts. The MCP `connection_status` already returns the account inventory; reuse it for selection. Unless `raw=true`, MCP `connection_status` and `list_accounts` omit account avatars, and `connection_status.me` keeps only `accountId`, `userId`, `displayName`, `enName`, `alias`, `tenantId` and `tenantName`. Call `list_accounts` only if that inventory is unavailable, needs refreshing, or is requested.

Concurrent MCP JSON GETs to the exact same URL and selected account share only their in-flight HTTP request and body decoding. They do not share across clients or accounts, and neither success nor failure is cached after completion. Starting a write clears the earlier read group; writes and search POSTs are never coalesced or retried. HTTP routes, response limits, and per-request timeouts are unchanged.

- `GET /accounts` returns `{accounts:[{id,name,userId,tenantName,primary,signedIn,active,connection,unreadCount,avatarUrl,quarantined?}],activeId}`. It is an instance route and does not check the account.
- Choosing an account:
  - Send `x-lark-connector-account: N` on every request (`lark-connector_ACCOUNT=N ol …`), or `?ol_account=N` on any route. The header wins.
  - N matches `^[1-9][0-9]{0,8}$`.
  - Requests that name no account go to the primary account `1`.
- Errors:
  - 400 `INVALID_ACCOUNT_ID`: malformed id, or a repeated parameter.
  - 409 `ACCOUNT_UNAVAILABLE`: unknown, quarantined or never-existing id. Also carries the header `x-lark-connector-account-unavailable: 1`.
  - 503 `ACCOUNT_RESETTING` with `Retry-After: 2`.
  - None of these fall back to another account.
- Accounts may share a display name, so confirm `id` and `userId` before a write.
- Adding or removing accounts is owner-only (`/api/accounts*`).
- Feature differences:
  - Primary account only: meeting launch, voice participants, the meeting room and the incoming-call poll.
  - Added accounts only: `GET /meetings/:id/insights`. On the primary account it always fails (see `meetings.md`).

## 3. Identity and sync health

| Call | Returns | Notes |
| --- | --- | --- |
| `GET /health` | `{ok:true,role:"agent"}` | Instance-level. A malformed account header still gets 400 |
| `GET /me` | `userId, accountId, displayName, enName, tenantName, signature{parts}, workStatus, avatarUrl, medalUrl` | 404 `ACCOUNT_NOT_SYNCED` until the first sync finishes. Check `userId` against `/accounts` |
| `GET /sync/status` | `state` (`idle`, `connecting`, `reconciling`, `live`, `degraded`, `failed`), `mode` (`frontier`, `polling`, `stopped`), `backlog`, `failures`, `database`, `rawArchive`, `eventLoop` | Healthy is `live` plus `frontier`. `polling` means events lag about 60 s |
| `GET /auth/session` | `authenticated, cookieCount, expiresAt, gatewayHost, savedAt, state` | `state` stays `PB_VALIDATING` whenever a session exists, so it is not a health signal |

Frontier WebSocket 握手期限为 30 秒；超时关闭走现有失败通知与重连退避。主动停止或被新连接替换的旧连接不会因迟到错误触发重连。这不是恢复 SLA，仍应以 `GET /sync/status` 的 `live / frontier` 确认实时连接状态。接口字段和鉴权不变。

## 4. Errors

实时包处理失败后，恢复先读取已提交游标之后的历史分页，再按 SID 顺序重试保留的实时包；补偿读取失败不会提前提交该实时包的较新游标。单轮最多读取 100 页，未完成的健康 pipe 会继续调度。游标过期的 pipe 保留 `SID_TIMEOUT_FULL_RECONCILIATION_REQUIRED` 错误和原游标，不在同一恢复轮次持续重试；其它 pipe 仍继续分页并重试失败的尾部消息。即使健康 pipe 已追平，整体状态仍可能为 `degraded`，不能据此声称所有历史缺口已恢复。没有新增 Agent 路由、字段或权限。

实时补偿读取或写入游标遇到 PostgreSQL 连接池错误 `timeout exceeded when trying to connect`（含 ORM 包装的 `cause`）时，按现有退避间隔自动重试，从 1 秒起、最长 30 秒。积压触发的 Frontier 暂停会等待补偿完成和缓冲排空后重连；停止服务会取消重试。此修复不改变通用 SQL 重试策略，401/403 权限拒绝也不会因此自动重试。通过 `GET /sync/status` 的 `live / frontier` 及持续推进的 `lastPacketAt` 确认恢复；`backlog: 0` 不代表补偿已完成。接口路径、字段和鉴权要求不变。

Three shapes are in use; read `body.error.code` and `body.code`:

- A: `{error:{code,message,recoverable,traceId}}`, from most routes.
- B: Fastify's `{statusCode,error,message[,code]}`. Used for:
  - Unknown routes (404).
  - Body parse errors (400, 413, 415).
  - Service errors with a status (the code is at the top level).
  - Thrown errors and zod `.parse` failures. These come back as **500**, and the message is the issue list.
- C: a bare `{error:"..."}`, from a few routes (authorized apps, dynamic emoji, mention user groups).

Other patterns:

- `message: "Gateway HTTP 4xx for <COMMAND>"` is Lark refusing the call, passed through without a code.
- Upstream rejections in settings come back as 422 `*_REJECTED` with `recoverable:false`.

## 5. Resource URLs

Responses carry UI paths that start with `/api` (added accounts also get `?ol_account=N`). The agent port has no `/api`. Rewrite `/api` to `/agent/v1` for these prefixes:

- `/media/`
- `/documents/media/`
- `/local-media/`
- `/documents/whiteboard/`
- `/moments/videos/`
- `/message-url-preview-image`
- `/documents/plantuml/`

Exception: `/api/mail/assets/*` exists only on the owner surface. Use `GET /mail/compose/images/:fileKey` instead.

## 6. Live events (`/events`)

`GET /events` is a WebSocket.

- The token must go in a header.
- The server pings every 20 s; clients must answer the pong, which the Node WebSocket does automatically.
- There is no replay and no cursor. A restart closes the socket.
- `ol GET /events` returns 404 because it is not a WebSocket upgrade. Use the helper instead:

```bash
ol events --feed FEED_ID --types message.upserted --others --seconds 600   # NDJSON on stdout
```

Options:

- `--feed ID`: keep one chat.
- `--types a,b`: keep only these event types.
- `--others`: drop your own sends.
- `--max N`: exit after N events.
- `--seconds S`: exit after S seconds.

The helper reconnects by itself and warns on stderr that events from the gap are lost.

Event types:

- `message.upserted {feedId,messageId,message?,replacesMessageId?}`. Fires for new messages, edits, thread reply-count changes and sender-name backfills.
  - `message` is sometimes missing; fetch it with `GET /messages/ID`.
  - Your own sends carry `replacesMessageId` and `message.isMine:true`.
- Others: `message.recalled`, `message.deleted`, `reaction.updated{messageId}`, `message.read-state`, `message.cot.updated`, `conversation.upserted`, `conversations.upserted`, `pins.updated`, `topics.updated`, `calendar.reminder`, `todo.reminder`, `urgent.changed`, `auth.state`, `sync.upstream{state,mode,since}`, `ai-knowledge.updated`, `meeting.incoming-call`, `push.refresh`, `cache.refreshed`.
- Traffic is about 8 events a second, so always filter.

**Waiting for a reply:**

1. Note the `position` of your sent message.
2. Run `ol events --feed F --types message.upserted --others`.
3. Ignore root-message updates caused by thread reply counts: compare `position` or `createdAt`.
4. After a gap or instead of listening, poll `GET /conversations/F/messages?afterPosition=POSITION`.

## 7. Re-login (QR), only when the user asks

1. ⚠ Starting a login cancels every login in progress, including one the owner is doing in a browser. Check that nobody is mid-login.
2. `POST /auth/attempts` returns 201 `{attemptId,state,qrAvailable,approvalUrlAvailable,expiresAt?}`.
3. Poll `GET /auth/attempts/ID` until `qrAvailable`. You can also stream SSE with `ol GET /auth/attempts/ID/events -N -m 330`.
4. Run `ol GET /auth/attempts/ID/qr -o /tmp/ol-qr.png` and hand the PNG to the user. It returns 404 with an empty body until the QR exists.
5. The states run `QR_CREATING → QR_WAITING_SCAN → QR_SCANNED → ACCOUNT_AUTHENTICATED → (DEVICE_REQUIRED → DEVICE_WAITING_APPROVAL → DEVICE_APPROVED) → SESSION_CAPTURING → PB_VALIDATING → READY`.
   - On `DEVICE_WAITING_APPROVAL`, give the user `approvalUrl` or `mobileApprovalUrl`.
   - `POST …/device-challenge` makes a fresh device link.
6. If the attempt ends `FAILED` (with `errorCode`), `EXPIRED` or `CANCELLED`, call `POST /auth/attempts/ID/restart`. The timeout is 5 min. `DELETE /auth/attempts/ID` returns 204.
7. ⚠ An added account must be re-scanned by the same Lark user. A different person wipes that account's schema and data directory.

The website keeps the current page visible when Lark rejects a session and shows “飞书登录已失效，请重新登录” with a button using the existing clear-session/sign-in flow. Its account-scoped `/api/events` connection heartbeat carries confirmed expiry, including background gateway calls and Frontier ticket HTTP authentication refusals. Network disconnects, timeouts, 429/5xx, plain 403 and an invalid Frontier ticket alone are not proof of expiry. Gateway HTTP 401, policy-blocked 403, packet status 401 and explicit `errors.proto` codes 4302/4303 require re-authentication; API calls surface the existing `AUTH_SESSION_EXPIRED` code. Expiry is tied to the saved session epoch, so an old request cannot invalidate a later sign-in. This adds no agent route or agent event type; `/auth/session` still describes stored credentials, not upstream health.

⚠ `DELETE /auth/session` clears the stored session (204). Someone must scan again afterwards.

## 8. Status, do-not-disturb, signature (colleagues see all of these)

| Job | Call | Body and notes |
| --- | --- | --- |
| List statuses | `GET /settings/custom-statuses?locale=zh-CN` | `{canCreate,current?,statuses:[{id,title,active,doNotDisturb,endsAt,iconKey,…}]}` |
| ⚠ Set a status for a preset duration | `POST /settings/custom-status` | `{id,duration,endsAt}`, where `duration` is 1 (30 min), 2 (1 h), 3 (2 h), 4 (4 h) or 5 (to end of day). You compute `endsAt` (ISO, future, ≤ 24 h); the server does not check it against `duration`. _(unverified)_ |
| ⚠ Set a status until a time | `POST /settings/custom-status/until` | `{id,endsAt}`, up to 366 days |
| Clear a status | `POST /settings/custom-statuses/ID/clear` | 422 `CUSTOM_STATUS_REJECTED` |
| Create, edit or delete a status | `POST /settings/custom-statuses` `{title≤30,iconKey?,doNotDisturb?,note?≤800,settings?}`, `PATCH`/`DELETE /settings/custom-statuses/ID` | Check `canCreate` first |
| ⚠ Do-not-disturb | `POST /settings/do-not-disturb` | `{minutes:0..10080}`; 0 clears. Returns `{until?}` |
| ⚠ Signature | `PUT /me/signature` | `{text}`: at most 140 code points after newlines collapse (2000 raw), else 400 `SIGNATURE_TOO_LONG`. Read `/me` before and after |
| Private signatures (部分人可见) | `GET /me/private-signatures` | Signatures that only some chats or people see |
| ⚠ Set a private signature | `POST /me/private-signatures {text, chatIds?≤50, userIds?≤50, id?}` | At least one chat or person. Pass `id` to update an existing one. Same length rule as the public signature |
| ⚠ Remove a private signature | `DELETE /me/private-signatures/ID` |  |
| What a chat's members show you | `GET /conversations/FEED_ID/member-signatures` | `items[{userId,directoryKey?,publicSignature?,privateSignature?}]`. `directoryKey` is the key the member wrote into the chat's OLK public key directory, or the one last read from their row when that row has since been deleted or edited by someone else |
| Who has a key in a chat's OLK directory | `GET /conversations/FEED_ID/removeddirectory/members` | `{userIds[]}`: members whose row in the chat's OLK public key directory holds a usable key, sorted; ids only, no keys. Reads the directory's 60-second cache, not a member pull. Empty when the chat has no directory or it cannot be read. The web client draws an "lark-connector" tag beside these members |

Before changing the signature, check `GET /settings/local`. If `autoUnreadSignature` or `dynamicSignature` is on, a background job overwrites the text.

The web privacy settings detect valid legacy `olk1:` keys in the signed-in account's public and private signatures even without a configured vault. “Convert to invisible format” re-reads the published signatures, converts each key in place and preserves the text and audience. Private signatures scoped only to teams or other non-chat/non-user targets are reported as unsupported; the existing write gate is not widened. Invalid tokens are left alone. On a partial failure, refresh and retry only the remaining visible keys.

Signature writes refresh the local account/profile projection and notify open clients. Private writes do not replace the public signature. The browser drops its OLK member-signature cache after a successful write; signature/presence pushes also invalidate the relevant queries. Older in-flight roster/profile reads and signature previews cannot restore invalidated data. Without a push, other users' OLK signatures remain cached for at most 60 seconds; the server member-signature endpoint forces a member pull and directly reads private signatures. A private signature containing old user-written words is separate published content, not a stale cache: format migration intentionally preserves those words.

**Dynamic signature scripts.** For writing, testing and installing a script, use the `dynamic-signature` skill. The routes:

| Call | Notes |
| --- | --- |
| `GET /dynamic-signature` | `{enabled, script, triggers}` |
| ⚠ `PUT /dynamic-signature` | Any of `{enabled, script, triggers}`. Enabling turns the unread-count signature off and runs the script, which writes the public signature. Enabling without a script returns 400 |
| `POST /dynamic-signature/test {script}` | Dry run against live data; writes nothing |
| ⚠ `POST /dynamic-signature/run` | Runs it now (202) |
| `GET /dynamic-signature/status` | Last run outcome |

## 9. Translation settings

| Call | Notes |
| --- | --- |
| `GET /settings/translate` | Returns `{target,languages[{key,name}],autoTranslate,scopes[{key,enabled}],displayRule?}` |
| `PATCH /settings/translate` | Send one of these per call: `{target:"zh"}`, `{autoTranslate:bool}`, or `{scope:{key,enabled}}` where `key` is messages, docBody, docComments, web, docs, email, meetings, moments or chatNames |
| `PATCH /settings/translate/display-rule` | `{rule:"onlyTranslation"\|"withOriginal"}` |
| `GET`/`POST /settings/translate/not-translate` | `{language,notTranslate:bool}` |
| `GET`/`PATCH /settings/typing-translate` | `{key:"enabled"\|"lang"\|"sendContent",value}` |

Message translation itself lives in `messaging.md`.

## 10. Account, notification, privacy and display settings

- `GET /settings/account` returns the notification types, special-focus and badge style, plus privacy (`addFriendWays`, `findMeWays`). It includes the phone number and email.
- `PATCH /settings/account` changes one key per call:
  - `{key:"acceptSmsPhoneUrgent"|"notifyWorkStatus"|"offDuringCalls"|"showMutedBadgeInNavigation",value:bool}`
  - `{key:"badgeStyle",value:"dot"|"number"}`
  - `{key:"addFriendWay",way,value}`
  - `{key:"findMeWay",id,value}`
- `PATCH /settings/notifications/messenger` takes one of:
  - `{key:"scope",value:"all"|"partial"|"off"}`
  - `{key:"type",type:"mention"|"mentionAll"|"userP2pChat"|"specialFocus",value}`
  - `{key:"specialFocus",notice,value}`

  The buzz, groupChat, p2pChat and reaction types are readable but cannot be changed.

- Privacy:
  - `GET /settings/privacy/extras`
  - `PATCH /settings/privacy/who-can-chat {value:"all"|"allExceptDocs"|"contacts"}`
  - `PATCH /settings/privacy/timezone-display {mode:"device"|"custom"|"hidden",timezone?}` (IANA name)
  - `GET /settings/privacy/blocklist?cursor=` (decimal string). Unblock through `people.md`.
- UI preferences, rarely useful to agents:
  - `GET`/`PATCH /settings/display-prefs`
  - `/settings/correction` (check `available`)
  - `/settings/moments-red-dot`

## 11. FLiNG Trainer switches (`/settings/local`)

`GET /settings/local` returns the full object, including any user signature script, plus `standIn` and `standInConfig` (the stand-in switch and its config such as `agent`, `model`, `prefix` and `ownerOnly`). `PUT` keeps the stored stand-in values whatever the body says, so an agent cannot change them.

The stand-in `agent` preset can be `traex`, `claude` or `codex`. Codex runs through `codex exec`; the host can set `lark-connector_STAND_IN_CODEX_BIN` to an absolute executable path. Codex guests require CLI 0.162.1 or newer and ignore user config and exec rules while retaining CLI authentication. Only owner web settings can select the preset or grant tools. The owner-only `/api/stand-in/status` includes Codex availability; it has no `/agent/v1` counterpart. See `docs/stand-in-mode.md` for guest limitations.

`PUT /settings/local` replaces the **whole** object:

- Required: antiRecall, autoRecall, autoRecallDelaySeconds (0–86400), autoUnreadSignature, autoUnreadSignatureIncludeMuted, autoUnreadSignatureTemplate, braveClimbCodec, disableReadReceipts, disableWatermark, momentsAntiDelete, language, showEditHistory, time24Hour, trainerBmBeans, trainerVacation.
- Fields with defaults are **silently reset** if omitted: dynamicSignature, dynamicSignatureScript (≤64 KiB), dynamicSignatureTriggers (≤20), includeMutedChats, theme.

Only these fields change server behavior:

- `autoRecall` and `autoRecallDelaySeconds`: the send gate and auto-recall.
- `braveClimbCodec`: the send gate.
- `autoUnreadSignature` and `dynamicSignature`: background signature rewrites that colleagues see.
- `language`: Lark content language.

The others only affect the web UI. For example, `disableReadReceipts` does not stop an agent's own `read-position` calls.

In the web composer, an active Brave Climb conversation keeps More tools and encoding switches accessible. File, video, voice-message, location and other direct-send tools are unavailable, and scheduled sending is hidden. Pasted or dropped non-image files are rejected; oversized or excess images cannot fall back to unencoded standalone messages. Spreadsheet images enter the draft and use its encoding path. These composer restrictions do not change agent routes or automatically encode agent requests.

Brave Climb is a reversible encoding, not encryption. The web UI says so where it is turned on: under the FLiNG Trainer switch (with a link to the Privacy section's encryption settings), in a toast when a pinned trainer shortcut turns it on, and in a strip above the composer while a conversation uses it (with a button that switches the conversation to OLK encryption). The sender's own decoded Brave Climb messages carry a small "Encoded, not encrypted" link to the encryption settings; others' messages do not. This is UI only and does not change agent routes.

Send gates:

- `403 AUTO_RECALL_DISABLED`: the request used `autoRecall:true` while `autoRecall` is off.
- `403 BRAVE_CLIMB_CODEC_DISABLED`: the text (or a segment between image placeholders) is a whole `**<payload>**` Brave Climb envelope while `braveClimbCodec` is off. A Brave Climb image upload hits the same gate.
- `messaging.md` lists which send routes check which gate.

To turn a switch on (only with the user's consent):

1. `ol GET /settings/local > /tmp/ol-local.json`
2. Edit just that field.
3. `ol PUT /settings/local @/tmp/ol-local.json`
4. Check the response, then retry the send.

⚠ Writing `dynamicSignatureScript` makes the host run that JavaScript.

## 12. Emoji, stickers, devices, file safety

| Call | Use |
| --- | --- |
| `GET /resources/emojis?keys=` (≤100) | Reaction and emoji keys such as `OK` or `THUMBSUP`; returns `{items:[{key,label,imageUrl,skinKeys}]}` |
| `GET /resources/stickers` | Sticker `key`, `stickerId`, `setId` for sending |
| `GET /settings/stickers`, `GET /settings/sticker-store?cursor=` | My sticker sets and the store |
| `POST /settings/sticker-sets {setId}`, `DELETE /settings/sticker-sets/ID`, `PUT /settings/sticker-sets/order {setIds}`, `POST /settings/stickers/delete {key}` | Manage sticker sets. Deleting cannot be undone |
| `GET /resources/dynamic-emojis`, `…/:key/video` | Animated emoji (webm) |
| `GET /settings/devices?locale=zh-CN` | `{devices:[{id,current,online,platform,canSignOut}]}` |
| ⚠ `POST /settings/devices/logout {deviceId}` | Ends a real session elsewhere. 409 `CURRENT_DEVICE` or `DEVICE_NOT_LISTED` |
| `GET /resources/files/:resourceKey/risk`, `…/protection`, `GET /resources/content-protection` | Download and watermark policy |
| `GET /event-center/ongoing-meetings` | Meetings in progress |
| ⚠ `POST /links/open {url}` (or `GET /links/open?url=`, which redirects) | Opens an http(s) Lark-connected web app with this account's session. It may approve the app's OAuth on the user's behalf. Full token only, GET included. Returns `{url, authenticated}`; the url can carry a login ticket, so treat it as a credential and use it only for the user's request. 502 `LINK_LOGIN_FAILED` |
| `GET /settings/about`, `GET /settings/universal?keys=a,b` (≤50), `GET /account/admin-console`, `GET /resources/authorized-apps/:appId` | Misc reads |

## 13. Host maintenance: never run these unless the user asks

Host-only storage commands (no Agent API changes): `npm run storage:maintain` previews log rotation; `-- --docker` also previews older `lark-connector:*` tags, retaining the newest three image IDs and all container-referenced IDs, including stopped containers. `--prune-build-cache` explicitly includes a 4 GB budget for the selected Docker builder's shared unused cache, including other projects. Only `--apply` executes the plan. Logs rotate at 50 MiB, retain five gzip copies and preserve the live inode; copytruncate can lose concurrent writes between copy and truncate. Only this tool's archive names are removed. PostgreSQL, volumes, media, blobs and retention settings are untouched. Errors exit nonzero with a runId in JSON logs. On macOS `npm run setup:storage-maintenance` previews an hourly LaunchAgent; add `-- --apply` and the selected Docker flags to install from the permanent checkout. `deploy:local` now checks for 2 GiB available before staging, without automatically cleaning anything. See `deploy/README.md` for commands, paths, Docker Desktop limitations and stale-lock recovery.

| Call | Effect |
| --- | --- |
| `GET /maintenance/database/size` | Safe; returns `{sizeBytes}` |
| `GET /maintenance/database/storage` | Read-only but slow |
| ⚠ `GET /maintenance/export` | Streams the selected account's fixed 21-table logical export from one read-only snapshot. Full token only; resource and completion rules below |
| `POST /maintenance/backups` | `pg_dump` of the whole database (all accounts). Use `-o FILE -m 3600` |
| `POST /maintenance/media-backups` | tar.gz of media and blobs |
| ⚠ `POST /maintenance/database/vacuum` | `VACUUM FULL` on `messages`; locks the table for its whole run |
| ⚠ `POST /maintenance/database/rebuild` | Stops live sync and replays raw events. 409 before live sync has started. Body: `{reset?: boolean, resumeFromEventId?: number, throughEventId?: number}`. Default `reset: true` truncates and rebuilds all projections. To resume without truncation, pass `reset: false` and `resumeFromEventId` (integer >= 0; `throughEventId` optional). Risk: partial replay with wrong range can leave inconsistent projections or stale unread states; `reset: false` synchronizes conversation previews for affected feeds on completion. |
| ⚠ `DELETE /maintenance/media-cache` | Deletes the media cache. Media that has expired upstream is gone for good |

### 逻辑数据库导出

`GET /maintenance/export` 的结构仍是 `{exportedAt,schemaVersion:3,tables:{...}}`，二进制字段仍编码为 `{base64,encoding:"base64"}`，包含 `raw_ingest_events` 和 `raw_archive_segments`。它导出所选账号 schema 内定义的固定 21 表；只读令牌仍返回 403。`POST /maintenance/backups` 的 `pg_dump` 则仍覆盖全数据库所有账号。没有新增必填请求头；Agent 响应保持未压缩，网页 `/api/maintenance/export` 按原有 `Accept-Encoding` 协商流式 Brotli/gzip。

每个服务进程的所有账号共享一个导出名额，包含初始化阶段；重复请求返回 503 `{statusCode:503,error:"Service Unavailable",message:"A database export is already running"}`，不带 `Retry-After`。不要立即循环重试。`HEAD` 只返回头部，不查询数据库或占用名额。

服务端使用只读 Repeatable Read 快照，普通表每批最多 16 行、两张 raw 表每批 1 行。二进制分块转为 base64，内存边界仍包含当前批次及最大单行，不是固定 64 KiB 上限。数据库语句限时 30 秒，客户端等待查询限时 35 秒；HTTP 下载连续 60 秒既没有 socket/drain 进展、也没有未受 HTTP 背压阻塞的源数据进展，或总时长达到 30 分钟会被终止。压缩器缓冲期间的正常源输入计为进展；HTTP 正在等待消费者读取时，源输入不会延长空闲期限。慢读期间快照会占用连接并延后 PostgreSQL 清理旧版本，最长受 30 分钟约束。

下载断开会取消查询并释放连接和名额。首个游标及首批查询在发送响应前预检；之后出错或超时会截断流，不能把收到 HTTP 200 当作导出成功。只在用户要求时调用，使用 `ol GET /maintenance/export -o backup.json -m 1900` 等直接保存方式；先完整接收并确认 JSON 完整，再把文件当作有效导出。不要把整库响应写进聊天输出或一次性读进内存。

Telemetry routes (`POST /diagnostics/*`) are UI-only. `GET /api/desktop/lark-session` and `GET /api/desktop/applink?url=…` are owner-only desktop endpoints and are not exposed on the Agent server. Do not call them with an agent token.

账号 runtime 关闭时，先停止保留策略定时清理与历史补拉并等待在途工作退出，再关闭依赖。准备中的定时清理、排队清理和后续补拉批次不会在停止后启动。清理完成当前删除批次及必要的会话边界更新后退出，边界只依据已删除消息计算；不再启动后续删除批次、会话或媒体条目，已开始的媒体条目会完成。已执行的工作会被等待，不强制中断数据库操作；退出耗时仍取决于当前在途操作。此生命周期修复不新增 Agent 停止接口，也不改变主账号的服务接线。

## 14. Retention policy, preview, and backfill

The owner settings page opens the enable-confirmation dialog immediately while its read-only preview is calculated. Confirmation stays disabled until that request finishes; cancellation aborts the browser request and discards late responses without enabling retention. After confirmation, saving cannot be cancelled through the dialog. A proposed policy affecting zero conversations returns the same empty preview without loading message protection cursors, message counts or attachment statistics. Numeric controls and protection choices align with the left edge of their settings descriptions at desktop and narrow widths; choices can wrap and units stay beside their inputs. Day inputs display only their unit; select labels and options display the interpolated full duration. Agent routes, request/response fields and confirmation/grace semantics are unchanged.

| Call | Effect |
| --- | --- |
| `GET /retention/config` | Returns current `RetentionPolicyConfig` (enabled, rules, globalDefaultDays, gracePeriodDays, protectedWindowDays (integer days or `null` for permanent protection), protectedContextCount, attachmentRetentionDays) |
| `PUT /retention/config` | Updates policy config. `protectedWindowDays` accepts positive integer or `null` (permanent). Enabling retention or shortening retention days requires `confirm: true` in body. Without `confirm: true`, returns HTTP 409 `CONFIRM_REQUIRED` with `preview` report |
| `POST /retention/preview` | Dry-run preview of candidate messages and estimated disk space (returns `RetentionCleanPreviewReport` with optional `estimate: true`, `partial: true`, `countedFeeds`, and `targetFeeds` fields) |
| `POST /retention/clean/apply` | Execute manual retention cleanup. Requires body: `{confirm: true}`. Returns `RetentionCleanExecutionResult` |
| `POST /retention/fetch` | Trigger manual bounded history backfill. Body: `{feedIds?: string[], match?: RuleMatch, temporary?: boolean}`. Temporary view marks messages beyond retention cutoff with `retention_temporary_until_ms`. Mutually exclusive with `HistoryBackfillService` (returns 409 if in progress) |
| `GET /retention/fetch` | Returns current retention fetch progress and status |

Policy changes preserve the previous window and pause cleanup during the configured grace period, including first activation. Reducing protected-message windows, context counts, attachment retention, or grace also requires confirmation. A confirmation response previews the proposed policy; it does not apply it or start the grace clock.

Attachment retention is measured from message creation and cannot be shorter than the message window. Null follows message retention. Detached attachment metadata persists until its deadline; shared or unidentifiable files are preserved. Deleted message IDs remain suppressed during ordinary synchronization and projection rebuilds, including gaps around protected messages.

Temporary fetch bypasses only the local retention boundary for that request; upstream access limits still apply. Only newly restored expired messages receive a temporary expiry, leaving existing protected messages unchanged. Fetch stops with item error `RETENTION_FETCH_NO_PROGRESS` if repeated upstream batches do not reduce missing positions.

`GET /retention/fetch` 返回 `{running, items, activeFeedId?, error?}`。`running` 包含目标会话准备阶段，该阶段重复 `POST /retention/fetch` 同样返回 409 `RETENTION_FETCH_IN_PROGRESS`。任务初始化或会话补拉异常记录在可选 `error` 中，即使尚未创建会话进度也能查询；会话级 `items[].error` 仍保留。新一轮清空旧进度和错误；没有匹配会话时触发结果为 `{started:false, feedCount:0}`。不要把 `running:false` 或空 `items` 单独当作成功，需同时检查任务及条目错误。网页保留策略设置也显示任务错误，触发请求失败后会立即刷新任务状态，不必等待空闲轮询。

## 15. Owner-only routes (not on the agent API)

- `/api/owner-auth/*`
- `/api/accounts*` writes
- `/api/outbox/:clientId`
- VC call routes
- Web push and desktop updates
- `/api/mail/assets/*`

If a task needs one of these, tell the user to use the website.

MCP JSON text blocks use compact JSON while preserving the same fields; parse them as JSON rather than depending on whitespace or a `structuredContent` copy (`read_document`, `read_minutes` and `harvest_context` keep metadata-only `structuredContent`; list and search tools answer a Markdown table plus a JSON paging line unless `raw=true`). MCP errors are `{error:{code,message,hint?,status?,recoverable,retryAfterSeconds?,traceId?}}`, classified like `removedctx` errors (`recoverable:false` for argument errors and rejected 4xx). Text attachment contents remain verbatim. `connection_status` reads instance health and account inventory concurrently, then reads identity and sync state for the selected account; explicit account failures never fall back to another account. No new HTTP routes or tool arguments are introduced.

Frontend message metadata readers preserve the server contract validation and discard unknown fields; server schemas remain the API contract. Shared UI constants and helpers are loaded without schema initialization so the initial chat shell does not load Zod. No agent routes, fields, or limits change.

The browser's own-contact-link dialog and profile-only details are split from the initial shell. Opening the dialog still uses the existing account-scoped reads and explicit reset confirmation; a chunk-load failure closes it and reports an error so a later open can retry. The external-contact banner retains its synchronous add-contact component. No Agent route, field, or permission changes.

The shell's calendar day links format their fixed local `yyyy-MM-dd` key without loading the calendar's general date formatter. UTC conversion is not used, invalid dates remain rejected, and no Agent route or date parameter contract changes.

MCP image attachments retain their exact byte range when encoded as base64, including views into a larger buffer. Other binary attachments are written to a private temp file (0700 directory, 0600 file) and returned as a `resource_link`; text over 8,000 estimated tokens is written the same way. Attachment size limits are unchanged.

The browser product rail exposes a standalone Bitable home at `/bitable`, loaded on first visit. Opening a listed document uses the existing `/documents/read?url=...` reader; the Documents product remains available. This UI entry adds no Agent API route, tool, field, or limit.

On narrow mobile screens, More opens a bottom sheet with this client''s question on their behalf through the legacy token.

- Negotiate both sides: the Hub advertises `interrupts.question`; the agent includes `question` in `olap.hello.capabilities.interrupts`. Existing `tool_approval` negotiation remains independent. An undeclared interrupt kind fails the run as a protocol error.
- Send `interrupt.requested` with `kind:"question"`, `interruptId` (1–128 characters), `title` (1–200), optional `detail` (up to 4000), optional `expiresAt`, and 1–5 `questions`. Normal run/event sequence fields still apply.
- Each question has a unique `id` (1–64 characters, excluding `__proto__`), `text` (1–1000), optional `options` (1–20 unique labels, each 1–200 characters, with optional descriptions up to 500), `multiSelect` (default false), and `allowOther` (default true). Without options, the answer is free text.
- At most one interrupt is pending per run; IDs cannot be reused within a run. The run enters `waiting_input`. The Hub limits the wait to ten minutes, or the agent's earlier deadline.
- The owner answers in the run card. The owner-session route is `POST /api/olap/v0/runs/:runId/interrupts/:interruptId` with `{decision:"answer",answers:{questionId:value}}`, or `{decision:"deny"}` to decline. Encode the interrupt ID as a URL path segment. Every question requires an answer: a string for single choice/free text, or a non-empty array for multiple choice. Own text is trimmed and limited to 4000 characters; multiple choice requires distinct values and allows at most one own-text value when `allowOther` is true.
- Answers that do not fit return 400 `OLAP_INTERRUPT_ANSWER_INVALID` with `details.problem` and, where applicable, `details.questionId`. A resolved/expired interrupt returns 409 `OLAP_INTERRUPT_NOT_PENDING`. The Hub is authoritative for expiry.
- The agent receives `run.interrupt.resolve` with `decision:"answer"` and `answers`, or `deny`/`expired` without answers. Acknowledge commands normally; unacknowledged commands are replayed on reconnect only to agents declaring that interrupt kind. The internal stored kind is not sent on the wire.
- Answers appear in the owner-visible `interrupt.resolved` history and stored delivery command, but are excluded from the audit log. No legacy agent route is added or changed by question interrupts.

## OLAP owner input during a run

The Hub advertises `runs.input`; participating agents declare `capabilities.input:true` in hello. The owner-session UI can then add instructions to the same run, independently of answering an interrupt. This does not add a legacy `/agent/v1` route or authorize an agent to impersonate owner input.

- Owner route: `POST /api/olap/v0/runs/:runId/inputs` with `{requestId,text}` returns `{input:{inputId,runId,text,createdAt}}`. `requestId` is 8–128 characters from `[A-Za-z0-9._:-]`; text is trimmed and must contain 1–20000 characters.
- Input is accepted only in `running` or `waiting_input`, with at most 20 additions per run. It does not resolve a pending question or tool approval. `OlapRun.acceptsInput` tells the UI whether the current run/agent combination supports additions; absent means unsupported.
- Idempotency is per run and requestId: the same trimmed text returns the original input, including after completion and record compaction. Different text returns 409 `OLAP_REQUEST_ID_CONFLICT`. Retry an uncertain submission with its original requestId.
- Other failures: 404 `OLAP_RUN_NOT_FOUND`; 409 `OLAP_RUN_NOT_ACTIVE` with `details.status`, `OLAP_INPUT_UNSUPPORTED`, or `OLAP_INPUT_LIMIT_REACHED`. Invalid text/requestId returns 400 `OLAP_INVALID_INPUT`.
- Capability comes from the current hello, or the last hello seen by this Hub process while the agent is away. After a Hub restart, support is unknown until another hello. A reconnect without `input` prevents new additions and withholds queued input commands; declaring support again allows delivery if the run is still active.
- The agent receives `run.input {commandId,runId,inputId,text}`. Apply each inputId once and acknowledge its command; unacknowledged commands are resent on hello. Input can be merged into the current turn or processed immediately afterward within the same run. Ending the run makes undelivered commands obsolete; acceptance by the Hub is not proof the agent consumed the input.
- Owner additions appear as `input` records with `source:"owner"`, and remain in `snapshot.inputs` after compaction. Bounded input records retain the private retry metadata; it is not exposed in event responses. Audit entries contain the action and inputId, not the text.
- The thread composer preserves text when the run ends before the addition is accepted; the owner can then send it as a follow-up. It does not automatically start another run.

## OLAP agent-started runs

When hello advertises `runs.agent`, send the WebSocket RPC `olap.runs.create` with `{requestId,title,threadId?}`. The title is 1–200 characters; the result is `{runId,threadId}`. Older Hubs return method-not-found. This does not add an `/agent/v1` route.

- Runs start as private, `running`, with `trigger:"agent"`; no `run.start` command is sent. Continue using normal events, interrupts, owner input, cancellation, and completion.
- Omit threadId to start a standalone thread. An existing thread must belong to this registration; its feed and latest run are inherited as feedId and parentRunId. This does not send a Feishu message.
- Reuse requestId only to retry the same title/thread combination. Idempotency is per registration, including after completion; different parameters return invalid params with `data.reason:"request_id_conflict"`.
- Refusals use RPC error -32005 with `data.reason`: `busy`, `thread_busy`, `thread_not_found`, `rate_limited` (with retryAfterMs), or `disabled`. The registration must be active, allow private runs, and belong to an open account. Agent starts share the owner's run rate limit.
- The owner UI shows the title and agent marker. New runs can raise a toast and per-thread unread dot while conversation OLAP sync is mounted; unread state is local to the browser/account, not server delivery confirmation.

## OLAP output artifacts

When the Hub advertises `artifacts.v1`, upload raw bytes with `PUT /olap/v0/runs/:runId/artifacts?name=<URL-encoded name>`, `Authorization: Bearer <OLAP credential>`, and the file's Content-Type. Credentials are checked before reading the body. The run must belong to this registration and be running, waiting_input, or cancelling.

- A 201 response contains `{artifactId,name,mime,size,sha256}`. Use the returned name/mime/size unchanged in an `artifact` event `{artifactId,name,mime,size}` to place the file in the timeline. Upload alone does not publish an event; unknown or mismatched metadata is an invalid event.
- Limits: 20 MiB per file, 20 files and 50 MiB per run, four concurrent uploads per registration, and a one-minute idle timeout. Empty files, executable/installer types and extensions (including .js), and declared raster images without recognized image bytes are refused. Names are sanitized. Read the negotiated limits from hello.
- Owner download is `GET /api/olap/v0/runs/:runId/artifacts/:artifactId` with an owner session scoped to the account, not an OLAP or legacy agent token. PNG/JPEG/GIF/WebP/AVIF may display inline; all other types are octet-stream attachments. Append `?download=1` to force download. Responses use nosniff, sandbox CSP, private caching, and SHA-256 ETags.
- On run compaction (14 days after completion), bytes are removed, artifact rows become expired, and downloads return 410. Snapshot metadata remains. These files are not included in the media backup archive; download anything that needs durable retention.
- The UI displays image thumbnails and downloadable file chips in the process and attachment strip. Thumbnails load the original image. There is no aggregate per-account disk quota beyond the per-run limits and run rate limit.

## OLK encryption onboarding in the web client

公钥检查期间只显示检查状态，不展示发布按钮。检查请求失败时显示重新检查入口，不把失败解释为公钥缺失；重试读到当前公钥后直接开启加密，检查成功且返回未发布时才提供发布按钮。底层目录读取及签名优先级保持原有契约，不新增已发布缓存。

The group composer's encryption entry checks the current server vault and the effective key visible in that group (directory first, then private/public signature). It guides missing identity creation or vault unlock using the existing vault controls, then offers publication to the group's OLK public key directory. Successful publication enables encryption automatically; a failure keeps the step open for retry. An explicit signature fallback reuses the existing group-scoped signature publisher, and enables encryption only after the key visibility check succeeds. Closing the guide or switching conversations does not enable encryption.

Any group member can delete or edit any row of the OLK public key directory. A read gives only the keys the rows read now give (reused for up to 60 seconds); no key read before is kept to stand in for a deleted or edited row, so until that member publishes again they are reached through their signature public key, if any, and otherwise left out of the recipients. Reads never write the directory: when one finds this account's own row deleted or edited by someone else, `/events` carries `{type:"olk.directory.notice", feedId, kind:"ownMissing", reason:"deleted"|"modified"}` and the page offers “Publish again”, which is the same directory publish as above. No route was added.

Settings → Privacy → Message encryption offers “Publish my public key to multiple groups.” It lists up to 100 recent group-scope conversations, filtered to group chats, and publishes to the selected groups sequentially through the existing directory API. Each row reports success or failure; retry skips successful rows. This does not turn on encryption in the selected composers. The current vault must be unlocked; creation, restoration, owner verification, signatures, and the encryption protocol retain their existing behavior.

These are browser UI workflows only. No `/agent/v1` route, input, output, or gate changes; do not treat the web publication endpoint as an agent API.

## Desktop app links

The desktop Lark window resolves official app links on navigation, redirects, and new-window requests. Web URL links open their HTTP(S) target; web apps and mini programs with an H5 catalogue entry open that entry. Supported lark-connector routes open in the main window. Unresolved links use their landing page to hand off to the installed official client, allowing only `lark:`, `feishu:`, `feishu-open:`, and `x-lark:` external schemes. This requires both the updated desktop build and server. It adds no `/agent/v1` capability and does not authorize agents to launch external applications.

### OLK 密钥库恢复码（owner 网页）

消息加密设置支持“生成恢复码”和“用恢复码解锁并重设口令”，锁屏提供“忘记口令？用恢复码”。生成要求库已解锁及主人重新验证；码只显示一次，勾选已保存并在 10 分钟内确认才替换旧恢复码。未确认、取消或重启不改变原有封装。恢复必须同时设置至少 12 字符的新口令；保留原 DEK、身份、群链与发送状态，禁用自动解锁标志，不读取或改写旧自动解锁文件。恢复码持续有效到确认替换，独立快照中的旧凭据不会被追溯撤销。

密钥库主记录仍是原 v1 格式，恢复码封装单独存放并以 DEK 承诺值核对；旧版本服务可直接读写主记录，换回后恢复码仍有效。恢复码不再对应当前密钥时，设置页提示已失效需重新生成。完整备份不含恢复码，旧完整备份仍用备份时口令导入。恢复失败最多允许每账号 15 分钟 5 次，成功口令解锁不会清除此预算。限制保存在进程内，服务重启会重置。

这是主人 Web API 的功能，没有新增或变更 agent 路由、字段、权限门禁或错误码。不要使用 `/agent/v1` 尝试生成或恢复，也不要让主人把恢复码或口令发到聊天、日志或工单。主人需要自行在界面完成。

## 资源使用边界

慢盘优化覆盖数据库、归档、服务端与浏览器缓存。用户、成员、偏好、已读状态、图片解密元数据和运行状态未变化时跳过数据行重写；条件 UPSERT 仍可能获取行锁并产生相关 WAL。日历和朋友圈仍保留读取/观察时间，未变的大 JSON 复用已有值。日历可见性改为单条账号范围内的更新，保留事件读取时间。上下文采集身份未变化时仅做数据库读取；身份切换和任务检查继续校验持久化 epoch。会话活跃时间批量写入，维护不重叠且关闭时排空。

消息和上下文缓存只更新元数据或有效期时复用相同的大正文，保留精确时间、版本和空值语义。上下文批量读取每批最多 200 个目标，版本过滤在 SQL 内完成，访问时间批量更新。搜索索引只重写变化行；重建先用 keyset 分页取最多 500 个 ID，再在现有逐消息 upsert 队列内一次读取当前所需消息字段和会话名，避免旧快照覆盖并发编辑，最后仍删除孤儿投影。队列由使用同一 `DatabaseService` 对象的 repository 共享，不保护跨进程或外部 SQL。相同反应、重复已读推送和相同主题回复数跳过行重写。会话尾部及最高已存位置按最多 200 个 feed 一批读取，复用现有索引并保留大整数、删除和临时消息语义。代理重连、权限检查、上下文覆盖汇总与 OLK 搜索使用必要字段投影，实际读取正文时继续走原有检查。

媒体保留队列在当前账号 schema 为 `retention_media.resource_key IS NOT NULL` 建立部分 hash 索引，减少每个候选的资源键引用扫描；逐文件即时引用检查、删除保护和保留规则不变。仅带非空 `localPath` 的记录追加路径和 raw 引用检查，相关路径字段未新增索引。新索引会占用空间并增加追加及 VACUUM 的 WAL，测量取舍见性能报告，不能称为所有读写均降低。启动迁移只读校验正确且可用的既有索引；匹配但失效的索引原子重建，失败恢复旧索引，定义冲突或意外同名对象保留并 deferred。锁等待 500 ms、语句超时 5 秒，锁超时、死锁和语句超时留待后续启动重试；用户主动取消及其他未识别异常仍抛出，原会话超时设置会恢复。

上下文响应缓存的维护延迟合并至 250 ms，累计 32 次写入或 8 MiB 时提前执行，账号配额较小时降低阈值；数据写入本身仍等待数据库完成。服务端读取缓存最多同时执行 2 个文件写入/删除，启动恢复最多 4 个读者；同键保留最后待执行操作，尚未落盘的淘汰项直接取消。磁盘镜像配额为 400 个键，两个在途写入可短暂达到 401；删除失败保留跟踪并限制后续新增镜像，正常关闭等待队列排空。它仍是尽力持久化的可丢弃缓存。

页面磁盘缓存的 `.json` 文件内部也可能是 gzip：8 KiB 至 64 MiB 的记录在已有写入队列内异步低级别压缩，仅更小时采用；旧文本 JSON 继续兼容，解压输出限制为 64 MiB。不要假定扩展名代表明文格式。正文、账号和精确刷新时间保持原值，压缩或磁盘失败保留内存响应。

raw 写入保留原有持久化、重试和字节背压，增加 8192 行背压阈值以覆盖零字节事件；已并发发起的调用可能越过阈值，不是绝对内存上限。归档分页限制元数据读取，重复分段加载共享读取与解压，最多跟踪 8 个共享加载键；该数不是不同分段解压的并发上限。重连游标、消息身份补全、重放身份与搜索预览使用批量或窄字段读取。清理只拉取必要消息元数据，仍按原有保护、锁与停止规则执行；JSON 属性提取仍可能在数据库内解 TOAST。

同一 `DatabaseService` 对象下的归档实例共享 FIFO 读与清理租约：元数据查询至整页正文收集完成持共享读租约，读者可并发；清理每批最多 16 段独占，批次之间放行已排队读者。等待中的清理收到停止信号可立即退队，已开始的 DELETE 仍完成。重建每页最多取 500 个事件，可能选择最多 500 段元数据；整页慢读会延后清理及后续归档读，没有固定耗时保证。该协调不覆盖外部 SQL 或其他 `DatabaseService` 对象，不增加数据库查询或磁盘操作，也不保护页外读取。

raw 保留任务保持原六小时周期和保留期限，维护不重叠，关闭等待在途清理；降级轮询保留原一分钟周期并将阻塞期间的刷新合并为一次立即后继。保留清理的首尾重算在同一 SQL 快照中最多取两行，媒体引用仍在每个文件删除前即时检查。停止信号在 raw 每批最多 5,000 行、归档每批最多 16 段及替身每个后续目录候选之间检查；已开始的批次或目录和记录提交仍需完成，没有固定停机耗时保证。账号启动以三秒锁等待上限移除未使用的认证活跃时间索引，繁忙时后续启动重试，保留会话和其他认证索引。

消息删除保留带 `runDir` 或处于 queued/running 的替身记录，局部投影重建保留全部替身记录，使目录路径及消息 claim 不丢失。这些记录继续出现在 owner-only 最近运行中，沿用基于创建时间的 14 天、500 条保留规则、10 分钟保护及六小时维护周期。目录删除失败保留记录供重试，其他目录继续处理；不扫描既有孤儿目录，也不修复首次创建目录到持久化路径之间的既有崩溃窗口。

浏览器聊天内容变化仍按原批次保存；内容未变时最多每 5 分钟刷新一次持久化时间戳，因此重启后记录年龄可能多出不足 5 分钟。草稿和编辑会话跳过相同快照；Wiki 宽度合并拖动写入，结束、隐藏和卸载时保存。SW 媒体索引每个登录缓存最多一个写入在途，期间只保留后续刷新需求。

SW 图片实体存储在整个 worker 内最多同时运行 2 个任务，运行与等待合计最多 128 项，接纳 Blob 的总预算为媒体缓存容量与 20 MiB 的较小值；超预算跳过缓存保存，响应不等待落盘，后续请求可重试。等待项过期或登录失效时取消，活动任务直到结束才释放预算；旧登录的延迟写入不能覆盖恢复后同名登录的新实体或 TTL，旧缓存删除未完成时直接返回网络响应。索引与登录指针读写不属于该并发限制；网络接收、页面响应及 Cache API 内部副本不属于该字节预算，因此它不是总内存上限。每登录 150 MiB、每图 10 MiB 的原有默认持久缓存限制，以及 TTL、LRU 和账号隔离继续适用。

图片仍读取实际字节以验证格式，避免每次读取旁路 JSON；原图使用同目录临时文件与原子重命名，替换期间可读旧的完整缓存，同路径保存串行，没有新增全局原图写入并发限制。缩略图每次命中更新内存访问顺序，最多跟踪 4,096 个路径；持久化触摸间隔为 5 分钟与保留期四分之一中的较小值。每个 `MediaService` 实例最多 8 个在途 `utimes`，当前按账号创建实例；满载直接跳过且不排队，后续访问可重试，独立在途集合不受访问记录淘汰或清空影响。

可由字节识别格式的图片不再新写元数据旁文件，旧旁文件和非图片解密结果保持兼容。Brave Climb 文件检查先读 45 字节尾部，匹配候选仍通过同一文件句柄完整读取和校验，保留原图和动画字节。通知设置按实际查询成功时间去重落盘，不通过缓存命中延长七天回退期限；OLK 无关撤回和旧时间戳触摸不改写存储。

OLK 解密缓存每账号仍限制为 2,000 条、8 MiB，预算包含密文、IV、UTF-16 字符串及索引键估计开销，不代表浏览器实际磁盘占用或总内存。保存、追加消息 ID、删除及 LRU 淘汰维护同一总量，当前条目也可淘汰；新条目本身过大则不写条目或 totals。旧记录在首次实际保存、替换、追加新消息 ID 或删除存在条目时扫描重算总量，不重写未变化条目；普通读取、空删除、重复 ID 和仅更新时间的触摸不触发迁移。库名仍为 `removeddecrypt-cache.v3:<account>`，IndexedDB 版本升至 2，关闭旧连接并拒绝旧页面以版本 1 重开。旧页面缓存失败按未命中重新解密，加载新版页面后恢复新缓存；不因此请求主人清空浏览器数据。

Windows 桌面通知的头像首次只写文件，之后成功更新时间最多每 60 秒一次；每次通知仍更新最多 64 项的内存访问顺序并检查热头像文件存在性，外部删除、写失败或时钟回退可即时修复。新写入、恢复或裁剪重试时才读取目录，已知活跃文件不再逐个读取属性，仍保留 64 个头像文件。失败不进入限频窗口，裁剪失败不丢弃当前头像；其他平台仍使用 `NativeImage`。这是内部文件 I/O 优化，不增加通知或 Agent 接口。

AI 工作台的 lane-session 正向轮询在每个挂载实例内最多一个请求在途；期间多次唤醒只保留一个立即后继，完成后仍按活跃 2 秒、空闲 15 秒安排下一次轮询。卸载后不再启动请求或定时器，切换 host/pane 后忽略旧结果。此规则不强制取消已开始的请求，也不限制独立的历史翻页请求。

Agent Hub 的既有 `read_file` 能力对非负安全整数 `maxBytes` 及小于 2 GiB 的非空普通文件，只读取文件大小与 `maxBytes + 1` 中较小者，借额外一个字节判断截断；默认返回上限仍为 200,000 字节，单次底层读取最多 64 KiB。特殊文件、零大小或更大文件，以及旧接口接受的非整数等参数保留原完整读取回退。UTF-8 截断和路径检查语义不变，因此该优化不是所有输入的磁盘或内存硬上限，也没有新增 `/agent/v1` 调用或参数。

Agent Hub `SessionStore` 的 `set`/`clear` 先持久化候选状态，成功后才发布内存状态；相同值不产生文件操作，写入失败后的同值重试仍会执行。普通目标文件保留原权限模式，经同目录临时文件写完后原子重命名，失败保留旧文件和内存并尝试清理临时文件。已有或悬空符号链接继续指向原目标，只读权限及路径解析错误保持失败行为。设备、FIFO 等非普通目标沿用直接写入，避免替换特殊对象，内存仍仅在成功后发布；原子替换保证只适用于普通文件，没有新增 fsync、崩溃持久性保证或并发队列。

最终本机/NAS 对照使用相同合成 fixture 和候选 bundle，各版本每场景两轮共 40 个测量样本；均为暖缓存，NAS 还有 WRITE_BACK NVMe 缓存。缓存记录字节减少约 93.5%，但两平台缓存持久化耗时均上升，本机媒体保存也变慢；缓存计时包含请求和关闭排空，不是普通请求延迟。消息与会话尾部总 SQL 次数不变，分别减少行/载荷重写与计划扫描行。仅作各平台内部的描述性比较，不能由文件操作数、WAL 或共享缓冲区命中推导设备 I/O、统计显著性或端到端加速；完整环境、样本和取舍见性能报告。

数据库启动只在统计对象首次准备、重建或非空表缺失统计时分析消息；正常重启不再重建相同的 append-only 触发器或存储参数，配置偏离仍会修复。以上不改变 Agent 路由、请求字段、鉴权、错误码或必填头，也不授权清理数据、重建投影或降低 PostgreSQL 持久性。操作次数测试和限制见 `docs/performance/20261030-slow-disk.md`；Git 检出统一 LF，以满足 Windows 上同样的格式检查要求。

网页的头像成功缓存最多记录 5000 个 URL，头像后台预取共享最多 6 个并发；消息图片后台预取最多 3 个并发，媒体 warm 请求同时最多 1 个。图片预取和 warm 请求都有 15 秒超时；后台页面暂停启动新预取，回到页面后可继续。离开消息窗口取消属于该窗口的 warm 请求和待处理队列，已开始的图片预取仍受超时约束。浏览器可以淘汰缓存，预热失败不代表媒体永久不可用；显示图片和 Agent 下载仍走各自现有接口与权限检查。

SW 媒体缓存增量维护索引字节总量，容量、TTL 和 LRU 不变；该计数不代表浏览器实际磁盘占用。

服务端内容寻址 blob 写入仍先保存调用方字节快照，再通过临时文件和原子重命名落盘。raw 队列只保留独立字节快照；全新消息跳过变更比较的重复哈希，已有消息比较和归档段读取直接使用现有字节视图，减少临时复制。入队背压、重试、归档校验和读取缓存保持原有语义，存储格式与保留策略不变。缩略图清理的目录属性读取最多 8 个并发，扫描超时后不再启动后续条目，仍保留清理容量、写入保护和原有错误处理。没有增加清理权限，也不允许 Agent 因性能优化而主动执行维护或删数据。

消息视图只在当前 `viewsForRows` 调用内复用已读取的父消息并去重运行时投影；没有跨请求缓存，也不改变分页、召回内容或 Agent 接口约定。

网页表格滚动复用合并格、冻结格和相关行列计算；工作表及视口依赖变化时重新计算，虚拟窗口、overscan 和编辑接口不变。

构建时最多同时压缩 4 个资产，仍生成相同质量的 Brotli 和 gzip 文件。SSR 构建不再复制 public 目录，公共文件仍由 client 产物提供，SSR 自身的 assets 和导入保留。`shadcn` 仅在构建 CSS 时使用，属于开发依赖；发布流程先安装完整构建依赖并构建，再省略开发依赖。网页预热、表格渲染、blob、归档和构建优化没有改变 Agent 路由、参数、响应结构、鉴权或请求头要求。

### 实例登录恢复

这是宿主机维护命令，不是 `/api` 或 `/agent/v1` 路由。只有 owner 明确要求恢复实例登录时才执行；先保留并验证可恢复的数据库备份和数据目录。命令要求服务已停止；在实例项目目录、配置好同一数据库的 `lark-connector_DATABASE_URL` 后运行 `npm run owner:reset-password`，构建产物可用 `node dist/backend/server/owner-reset-password-main.js`。只接受宿主机回环 PostgreSQL 地址和交互式 TTY，密码隐藏输入两次，不接受密码参数或管道。

默认保留仅密码或用户名+密码模式；TOTP/Passkey 必须加 `--mode password` 明确改为仅密码，或加 `--mode password_totp` 保留已有 TOTP 并要求新密码。不会删除 TOTP 密钥或 Passkey 记录。命令在同一事务中启用认证、更新哈希、将凭据改为数据库管理、递增 revision、删除所有 Owner 会话；旧环境变量不能覆盖新密码。

运行中的服务不会感知库外改动，所以不做在线刷新：命令在输入密码之前检查服务进程锁（按 `lark-connector_DATA_DIR`）、实例写锁和数据库上的其它连接，任何一项表明服务可能在运行就拒绝，要求先停止服务；不能擅自把停止服务视为已获授权，也不能擅自加 `--allow-other-connections`（只在 owner 确认列出的连接不是 lark-connector 服务时使用）。命令从检测到结束一直持有写锁，期间服务无法启动；两个重置命令互斥。服务重启后从数据库加载会话，旧会话已全部删除，内存里的待处理挑战和令牌随旧进程消失。

登录页“忘记密码？”区分实例登录恢复和加密消息密钥库口令。重置登录密码不能解锁或恢复 OLK 密钥库；忘记密钥库口令时由主人在网页用恢复码解锁（见上文“OLK 密钥库恢复码”），没有恢复码时只能从备份恢复。命令不接触 OLK 密钥库、身份或自动解锁文件。Agent API 的路径、请求字段和权限契约不变。

Unified-password follow-up tests clone a run-local encrypted initial record into independent memory stores and a fresh locked vault service. Only fixture creation/enabling is reused; real unlocks, password changes, recovery and rollback still run with production cryptographic parameters. Dedicated creation/enabling tests continue to build their own vaults. The fixture isolation test checks that mutations and unlocking do not leak between instances.

The real-PostgreSQL owner-reset integration suite has a local 90-second test/cleanup limit because cases boot complete services and run real password hashing. The global test timeout is unchanged. Its setup tracks pending database/app creation, rejects results after Vitest cancellation, and drains creation before closing services and dropping databases. Keep that ordering: a Vitest timeout does not stop the underlying async startup. Do not suppress PostgreSQL error events to mask leaked fixture connections.

Test stability: `test/server/support/async-test-resources.ts` tracks asynchronous fixture creation, registers disposal before observing cancellation, and drains pending creation before disposing in reverse order. The heartbeat, built-app security-audit and file-watch fixtures give service preparation/cleanup their own 90-second watchdog; business assertions retain the global 20-second watchdog. A failed service close must prevent dropping its database. Never treat a timeout as cancellation of an underlying promise, retry a failed assertion to hide flakiness, or ignore PostgreSQL error events.

Context round-split coverage retains all four budget schedules, item limits, fault injection and cursor/content invariants. Each independent budget schedule is a separate case; a source's immutable unbounded baseline is computed once and reused. Known-defect checks remain aggregated because the defect need not appear in every schedule. On cancellation the harness stops between harvests, restores its virtual clock, and drains before another case or database cleanup starts. Large binary equality uses `Buffer.equals`, retaining full byte and zero-copy checks without generic object traversal. To check ordering independence, run the affected entry files with `npm run test:postgres -- --sequence.shuffle --sequence.seed=20261025`; this supplements, rather than replaces, a full isolated-worker run.

浏览器刷新恢复：读取 `localStorage` 本身抛出异常时，编辑 store 仍能初始化并注册编辑保护。没有草稿文字、富文本标题或编辑模式、图片附件、待恢复内容、回复目标、已启用发送模式、刷新 hold、待处理编辑请求或编辑会话时，允许刷新恢复；存在任一项则留在当前页面。清空内容或结束编辑后可重试。若 `sessionStorage` 也不可用，自动 chunk 恢复仍因无法保存循环保护而拒绝，手动恢复可在上述安全检查通过后执行。可用存储下仍保留未变化草稿和编辑快照跳过写盘的优化。此行为不新增或改变 Agent API。

反向代理转发空请求：没有 `Content-Type` 的 chunked 空 POST 可正常进入原有路由；非空且未声明类型的请求仍返回 415 `FST_ERR_CTP_INVALID_MEDIA_TYPE`。这不是通用二进制解析器，也不放宽显式未知类型、请求体大小、JSON 解析或鉴权校验。发送 JSON 时仍必须声明 `application/json`。

非 WebKit 的窄屏布局（不支持 `-webkit-touch-callout: none`，宽度最多 767px）将顶部和底部安全区下限降至 12px，仍通过 `max()` 保留更大的设备安全区。消息首页底栏使用共享导航高度，避免切换产品页面时跳动。后置的 600–767px 且高度至少 501px 的平板规则仍优先，使用侧栏；iOS WebKit 分支不变。该修改仅涉及页面布局，不改变 Agent API。

话题详情（聊天侧栏、会话详情话题页签和话题 feed）中的非系统消息复用 `MessageContextMenu`，操作仍遵循现有消息能力与权限判断。回复状态写入消息所属会话，不能使用话题 feed 背后残留的选中会话。系统消息保持无菜单。PWA 的静态 manifest 保留 `orientation: any`，不根据 UA 注入 `portrait`；强制竖屏会连用户主动横屏一起禁止，不能当作遵循系统旋转锁的通用修复。上述行为不改变 Agent API。

OLAP Key 面板使用可收缩的单列 grid、可换行长文本和有宽度限制的 agent 名称，避免窄面板裁切提问操作、耗时或发送按钮。标题栏尺寸只作用于该面板，线程运行分为卡片，输入框贴底；共享运行步骤行保留标题并让命令占剩余宽度。此改动不涉及 Agent 路由、数据字段或运行权限。

`GET /retention/fetch` 的 `items[].error` 可为 `RETENTION_FETCH_CHAT_UNREADABLE`：上游明确返回 4008（当前账号不在会话中），此项 `done:false`，页面显示已跳过，其他会话继续；这不是任务级失败，也不表示历史补拉完整。复用 `history-backfill:` / `retention-fetch:` 游标中的不可读取标记，消息尾位置变化或七天标记到期后允许重试。失败不推进补拉游标、不加入群聊、不删除本地消息；其他 400、认证或网络错误仍保留任务及条目错误，日志包含 runId、feedId 和上游错误码。

### Storage overview (website only)

Settings → Storage & Retention loads overview independently of policy settings. The owner-authenticated `GET /api/maintenance/storage-overview?range=30|180|all` is not registered on `/agent/v1`; invalid range returns 400 `INVALID_STORAGE_RANGE`. It reports `measuredAt`, `timezone`, service database/data/estimated total bytes, filesystem total/available bytes and free percentage, and message buckets with `startDate`, `endDate`, `count`, interval and total. HTTP responses are `no-store`; one shared server measurement is cached for 60 seconds across accounts and range switches. Measurement failures propagate and the page offers retry; they do not block retention settings or enable cleanup.

Service usage is `pg_database_size(current_database())` plus allocated files/directories beneath the service data root, deduplicating hardlinks and ignoring symlinks. It includes all local accounts. Database size excludes PostgreSQL WAL. The data-directory measurement includes whatever files are actually beneath that directory; the combined estimate can overlap if PostgreSQL itself is stored there. Only filesystem free/used percentages use the data filesystem capacity; they do not attribute database bytes to that filesystem. Available space is `bavail`, not reserved blocks or RAM.

Message distribution counts all account schemas' stored, non-deleted messages with positive creation timestamps before the next local midnight. It uses creation time, not ingestion time or daily growth, in Asia/Singapore. Default 30 days includes today and fills empty dates with zero; 180 days includes today and groups only those dates into Monday-based weeks; all groups all stored history into weeks. First/last weeks may be partial. Cleanup, message synchronization and account deletion can change historical bucket counts. No snapshots, retention changes, backfill, deletions or Agent capabilities are added.

The storage overview keeps both numeric cards. The space bar shows only used and available bytes of the data filesystem; the database/file estimate is separate because PostgreSQL may reside elsewhere or overlap the data directory. All message ranges fit the full time span into the chart viewport (no initial horizontal clipping of recent weeks); each day/week remains its own bucket. Positive counts have a minimum visible 1 px height, while tooltips retain exact counts. This is website presentation only and changes no Agent API.

维护日志轮转拒绝符号链接、硬链接及非普通文件，并对同一文件句柄执行压缩和截断。copy-truncate 仍可能丢失复制期间追加的日志，不用于无损审计。维护默认预览，只有显式 `--apply` 才修改日志或镜像；不得仅为读取概览自动执行维护。
