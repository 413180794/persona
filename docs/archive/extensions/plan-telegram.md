# Plan: Telegram connector (the user's own account)

> Completed 2026-11-05: Telegram connector moved out of open-source repository into telegram extension (#1704), initially private extension marketplace, since 0.1.3 in `nwverse/persona-extensions` (#2057).
> Connector ID unchanged; login, Inbox, knowledge continue. Code, tg CLI (now `personad tool --cli telegram`), telegram skill all in extension.
> Historical design record only; paths `connector/telegram`, `personad-telegram`, personad tg no longer exist here.

Status: design agreed, work started 2026-11-03. User decided own account (MTProto user client, not bot), message text may go to AI and knowledge / Inbox like Lark; assistant can search / summarize, with settings switches. First version **read-only**. Framework `docs/plans/plan-connectors.md` (Connector, Registry, gate); only Telegram-specific parts here.
After completion, merge design into `docs/extensions/connectors.md` or plan's Telegram section and delete this document.

## Library: gotd/td (pure Go), not TDLib

| | gotd/td | TDLib |
| --- | --- | --- |
| Language | Pure Go, `CGO_ENABLED=0` cross-compiles, same four-platform personad code | C++ requires cgo or tdjson dynamic library; platform builds require `publish.sh` / auto-update changes |
| State | Own auth-key storage / update cursors | Built-in local SQLite, difficult to encrypt into Postgres |
| Maintenance | Active Telegram layer tracking; tgtest in-memory service, auth flow, updates state machine, floodwait | Official, most complete |
| Size | Adds ~35 MB to personad (linux-amd64 measured 39 → 74 MB, gzip 14.5 → 23.7 MB, mainly generated Telegram types), so separate program below | Additional tens-of-MB library |

Choose gotd/td: one Go dependency, no release-flow changes; own session storage encrypts naturally into Postgres. Use raw `tg.Client` API, auth, `tgerr.FloodWait` only.

## Architecture: separate `personad-telegram` (option B, decided 2026-11-03)

Gotd adds 35 MB, so Telegram stays out of personad in separate program, keeping core size unchanged.

- **Code**: `server/internal/connector/telegram` uses gotd; `server/cmd/personad-telegram`, same module / build. Hub sends no Telegram config; credentials / session in `connector_secrets` accessed via secrets.*. Personad does not import it; test requires no gotd in go list -deps ./cmd/personad.
- **Hub integration**: enabled → launch `personad-telegram` beside personad using existing bidirectional stdin / stdout JSON-lines RPC (`internal/rpc`). Generic subprocess connector adapter `connector/proc` converts status / login / archive / Inbox / read-only tools / events to RPC; `connector/serve` exposes any Go connector over stdio. Reverse encrypted storage: child calls hub `secrets.load / save / delete`; plaintext sessions only both processes' memory, never child disk. Exit restarts with backoff; disable stops. Future heavy connectors can reuse.
- **Version alignment**: before launch, `personad-telegram version -v` must match personad version / build; otherwise no launch, status `personad-telegram` and Hub Versions Differ.
- **Hub only**: nodes do not need it; `deploy.sh` hub only, bundled in Docker hub image too.
- **Release / self-update**:
  - `publish.sh` builds `personad-telegram-<os>-<arch>.gz` for four platforms, ~10 MB each under 25 MiB, same privacy / size checks.
  - `latest.json` `programs: {"personad-telegram": {"<os>-<arch>": {name, size, sha256, binary_sha256}}}`; old hub ignores, updates personad only.
  - Hub downloads / verifies `version -v` / replaces both together, retains each `.prev`; failed startup rolls both back.
- **Deployment**: `deploy.sh` and main-chat `deploy-hub.sh` add program on hub, written as .new.

## Credentials: user enters everything in the app (requested 2026-11-03: do not embed Telegram API credentials)

- **api_id / api_hash**: not hub.json, binary, or build config. **First login step** requests them, with guide: “Sign in to my.telegram.org with your Telegram account, create an app under API development tools, enter api_id and api_hash here.”
- **Session (auth key + data center)**: gotd `session.Storage` connected to hub. Credentials / session one JSON encrypted with `secret.key` AES-GCM (same `sealKey` master as launcher / mail keys), Postgres `connector_secrets` (T1); plaintext only both processes' memory.
- **Display**: api_id visible, api_hash masked except last 4; status via example api_id <API ID> · api_hash ••••<suffix>. Neither appears in logs / errors / responses; connector errors redact first.
- **Login**, Settings › Connectors › Telegram: API credentials → phone number → code on other Telegram devices / SMS → two-step password if enabled. First validate credentials through `help.getConfig`; invalid stays with api_id or api_hash Is Incorrect. Hub then forwards `auth.sendCode` → `auth.signIn` → `SESSION_PASSWORD_NEEDED` `account.getPassword` / SRP. Intermediate `phone_code_hash` memory only; restart begins at phone. Password discarded after memory use.
- **Logout**: `auth.logOut` removes upstream session, then `connector_secrets` row deletes credentials / session. Logout / login to change credentials.
- **Off by default**: hubs with personad-telegram register disabled connector via T1 addConnectorOff. Enable in settings, then login.
- Hub directly connects Telegram data centers (TCP 443 / web.telegram.org tested reachable 2026-11-03). Proxy for inaccessible networks later, not this version.

## What to read

- **Chats**: direct, basic groups, supergroups. **Exclude** broadcast channels, bot DMs, Saved Messages (uncertainties below). Secret chats are end-to-end / device-only, unavailable via API; explain clearly.
- **Initial sync**: recently active `messages.getDialogs` ordered by latest message; each last 30 days, max 500 messages. No earlier backfill initially; future knowledge backfill if needed.
- **Incremental**: per-chat largest archived message ID in `Archive.Changes` cursor map chat → max ID. `getDialogs` checks newer latest IDs, then messages.getHistory(min_id=cursor). Updates only kick Events `ScopeChats` / `ScopeInbox`, not cursor tracking. Per-supergroup pts tracking complex; ID fetch simpler, avoids disconnect gaps.
- **Content**: text verbatim; replies prefix quoted original start (“Reply to ‘…’:”); forwards “Forwarded from Name”; photos / files / voice / stickers placeholders such as [Image], [File Name · Size]. No first-version downloads. Edits / deletion not tracked; append-only like Lark.
- **Knowledge IDs**: chats/telegram-<u|g|c><numeric id> for users / basic groups / supergroups, not mutable usernames or phone numbers. People/telegram-<user id>, display names.
- **Rate limits** (#958, user explicitly requires respect): one connector-wide request gate (`connector/telegram/gate.go`, gotd middleware) for all RPC including login / credential checks / gotd config:
  - One request at a time; random 0.5–1-second gap after previous completion, like user's Telethon skill. Archive / Inbox / assistant reads queue.
  - `FLOOD_WAIT_X` blocks every request for X seconds plus 0.5–1 seconds. Never retry; end current round, archive already read data. Later archive / Inbox / tool calls return Telegram Rate Limited, Retry in N Seconds before request. Repeated immediate limit abandons next round too.
  - Signed-in `rate_limited` state shows start, required duration, remaining, restore time in user timezone. Yellow dot / gray explanation, not error. Hub reads only ok connectors, no archive / assistant Telegram tools during limit.
  - One tool page: `telegram_messages` max 50, `telegram_search` max 30. Resolve names from last chat listing, Telegram only if absent, at most three requests per call.
  - State in subprocess memory; redeployment restart forgets, first repeated flood wait records again.
  - **Updates signal new data, processed in rounds** (#1024): kick archive first (actual hub archive at due time, default 10 minutes), then refresh Inbox via `getDialogs`, max 100 per folder, plus context messages. At least one minute between rounds (`newsEvery`), intervening updates coalesced. Any connector FLOOD_WAIT during / between rounds doubles interval up to 16 minutes; successful rounds halve to minimum 1. Previously every 3 seconds of accumulated updates refreshed, listing twice every 4–5 seconds on many-group accounts, triggering roughly one wait per minute (~65/hour, 2026-11-03).

## Inbox: what needs the user

- Direct chats: incoming, unread / unreplied, after `read_inbox_max_id` and latest not mine.
- Groups: mentions / replies to mine, unread_mentions_count. Muted groups / archived folder only knowledge, not Inbox.
- Read in Telegram phone / computer and not Needs you → `done_by=read`; replied → `done_by=replied`.
- **No replies in first read-only version**: `CanReply=false`; `OpenURL` tg://resolve?domain=<username> or `https://t.me/…`; Reply in Telegram delegates to system. Future `telegram_send` write must gate with recipient / full-content card.

## Assistant read-only tools

| Tool | Purpose |
| --- | --- |
| `telegram_chats` | Recent name / type / unread / last time and text prefix |
| `telegram_messages` | One chat, earlier pagination, truncated |
| `telegram_search` | Global `messages.searchGlobal` or chat `messages.search` |

Descriptions `server/internal/prompts/mcp/telegram/`, golden adds Telegram tool list. Archived data also searchable with kb_*.

## Chat page (#1054)

User requested richer GitHub / Telegram UI modeled on Lark chat. Telegram Inbox opens native page instead of plain text (`apple/Persona/Views/Inbox/ConnectorThread/`): incoming left with avatar / group name, mine right, time at 5-minute gaps / date changes; quote taps original, paging backward if not loaded.

- **Data**: threads capability (`docs/extensions/connectors.md`, checked with Ovis, shared GitHub). One `messages.getHistory` per page; missing quoted messages fetched together once with messages.getMessages. Initial latest 30, older on scroll.
- **Media**: avatars / images on demand as displayed. Hub `/media` disk cache + app cache prevent repeated Telegram fetch; each 512 KB chunk gated.
  - Avatar ref a.<chat>.<photo id>, no file reference.
  - Message ref m.<chat>.<message id>.f|t, location memory; after restart reread message for fresh file reference.
  - Photos largest size with long edge≤1280; static sticker WebP, animated / video thumbnails. Voice duration only, files name / size only.
  - Fetch from image's data center (#1272, measurements #1255). Avatar `UserProfilePhoto.dc_id` / `ChatPhoto.dc_id` remembered from pages / dialogs; `Photo.dc_id` / `Document.dc_id` too. Connect lazily per DC (`dc.go` `dcPools`, gotd `Client.DC`, direct MTProto). Connection / key generation / auth import outside gate; only auth export gated. One connection per DC, parallel across DCs; requests still gated. Unknown DC after restart, account DC, or relocated FILE_MIGRATE use account connection / gotd migration. No connection / request during limit. Previously cross-DC images took 2 requests, initial wrong-DC redirect; first ~4-second DC setup held entire gate.
  - Timing (#1255): one subprocess log per fetch, time / DC only, no ref / ID / name / content. Example:
    `Telegram avatar: 4.70 s; from DC 4 (account on DC 1); 2 request(s), queued 0.01 s, in Telegram 0.80 s; not sent on; new connection to DC 4 in 4.20 s outside the gate, authorization transfer 3.90 s (export 0.35 s)`.
    From = fetched DC; sent on = FILE_MIGRATE on account connection; new connection = first access this process, outside-gate duration, export / import auth transfer. `gate.do` records queue / call, gotd `OnTransfer` auth transfer, middleware export; `traceLog` gotd Logger enabled only timed requests, takes DC from two gotd records, prints nothing itself.
- **Rate limit**: pages / media use #965 gate; immediate Telegram Rate Limited, Retry in N Seconds, displayed above page or fallback hub-collected text.
- **Members** (#1228): source below title, groups / channels include count, “Telegram Group · 128 Members”, “Telegram Channel · 1,234 Subscribers”; DM Telegram only. Prefer existing objects, no extra request: `getDialogs` basic `Chat.participants_count` always, supergroups / channels sometimes. Missing count fetched once on first page via Info `getFullChannel` / `getFullChat`, same `tg get-chat`, cached 12 hours by `membersFor` including failures. After page / quote requests; limit drops count only, not page. No limit-time / older-page / DM queries; unknown shows source alone.
- **Replies**: bottom composer, long-press Reply quote; telegram_send. Explicit user Send bypasses `connector_gate` / cards (#1190); gate AI writes only.
- **Polish** (#1118, user decided after screenshots):
  - Content-width bubbles, max 78% page (`ThreadLayout.bubbleShare`); mine right, incoming same width rules; quote matches bubble via WidestColumn.
  - Time bottom-right HH:mm secondary, edits “Edited 09:42”; follows final text line, wraps right if needed (`StampedText` transparent time placeholder at text end, visible overlay bottom-right).
  - Textless image / video / sticker time on semitransparent `StampCapsule` bottom-right.
  - Upward scroll fades below summary bar; mask only this scroll area, shared Inbox summary unchanged.
  - Incoming sequence avatar by final bubble, bottom-aligned like Telegram; group sender name above first bubble.

## tg CLI and telegram skill (#988)

User requested feishu-cli-inspired command design and skill for flexible Agent Telegram operations only after successful hub integration.

- **Personad tg …** (`server/internal/tgcli`, tg on assistant PATH) goes only through hub: local node socket `cli.call` → hub `mcp.call` → Telegram connector, all #965-gated. CLI has no Telegram connection / token. **Node verifies assistant identity** (`node/clicall.go`): peer PID via Linux `SO_PEERCRED` / Mac `LOCAL_PEERPID` must be Persona-launched assistant or descendant, e.g. Bash. Unverifiable rejected so Agents cannot read private chats. Node forwards telegram_-prefixed calls only.
- **Read**: tg chats / unread / messages (`next_before`, `--since` / `--until`) / search / message / chat info / members. **Write**: send / reply / forward / react / read, connector actions `telegram_send` / `telegram_forward` / `telegram_react` / `telegram_mark_read`, existing `connector_gate` default phone card with chat / full text / audience, no extra gate. Write target only ID / @username already seen by read / Inbox, memory-only resolution, no Telegram lookup; shown chat is actual target.
- Tools Manifest.Unlisted, excluded from model list (assistant retains three read tools), called by name. Descriptions / params `prompts/mcp/telegram/`, implementations connector/telegram/cli.go. Reads JSON snake_case / user-timezone RFC 3339, same caps, one page.
- **Output** like feishu-cli: stdout one JSON line, `--format text` for humans; errors stderr `{error, kind, retry_after}`. Exit 0 success,1 error,2 usage,3 unavailable (logged out / off / no hub / nonassistant),4 limit,5 user / rule denial.
- **Skill**: `assistantskills/builtin/telegram/SKILL.md` installed only when hub Telegram ok or rate_limited, enabled / signed in. Every `assistant.prepare` connectors tells node to install / remove skill / tg through assistantskills.Requires. Logout / disable removes before next message.
- Subprocess `actions.list` / `actions.describe` / `actions.do` added (`docs/protocol/connector-protocol.md`), checked with Ovis 2026-11-03.

## Settings › Connectors › Telegram

- State: Off / Signed Out / Signed In · Display Name (@username) · api_id <API ID> · api_hash ••••<suffix> / Rate Limited / Error.
- Login stepwise generic forms: credentials + application guide → phone → code → password. Hub declares fields, app renders, reusable for other connectors. Api_hash / password secret, masked input.
- **Sync Telegram** off by default; disable stops connection / knowledge / Inbox / assistant tools, preserves login.
- **Delete Stored Data**: `telegram-*` knowledge chats / people, Telegram Inbox, cursor; second confirmation.
- **Add to Knowledge / Add to Inbox** (#989, capability-driven generic page), on by default. Knowledge off stops Changes / archival requests, retains Inbox / assistant. Delete Stored Knowledge deletes only `telegram-*` chats / people / cursors and persists knowledge off. Inbox off stops new items, existing untouched.
- **Sign Out** deletes credentials / session; required to change credentials.
- Inbox rows generic connector block like GitHub, official `Theme.Brand.telegram` resource.
- Read-only nonreplyable items show Reply in Telegram using `open_url` `tg://` / `t.me`; same button as Messages Reply in Messages via `InboxActionBar.openTitle`, no reply composer.
- Generic app `ConnectorSignInSections` / `ConnectorSignInModel`, `HubConnector.signsInHere` from login capability; no Telegram-specific page logic. `ConnectorSignInTests` `FakeHub`; `TelegramConnectorTests` mock `MOCK_TELEGRAM=2fa` flow credentials → phone → wrong code → code → password → signed in → logout. Also `MOCK_TELEGRAM=off|signed_out|signed_in`, MOCK_TELEGRAM_INBOX=1.

## API changes (`server/internal/connector`, checked with Ovis 2026-11-03)

1. **Stepwise login**, optional Login capability: `LoginStep(ctx, step, values) (Prompt, error)`, `Logout(ctx) error`. Prompt{Step, Fields []Field, Hint, Error, Done, Account}; Field{Name, Label, Secret, Placeholder}; empty step starts. User-correctable Invalid Code goes in `Prompt.Error`, retaining step for reentry; only unexpected failures return error. Planned one-step Credentials is simply one-step Login, no separate interface. Sessions / tokens exclusively Secrets, never Prompt / Status / logs. Hub `GET /api/v1/connectors` adds login fields per item; `POST /api/v1/connectors/{id}/login {step, values}`, POST …/logout.
2. **Encrypted storage** Secrets{Load,Save,Delete}: hub supplies per-ID implementation, new `connector_secrets(connector, sealed, updated_at)` table, one account per connector, `sealKey` encryption; injected at construction.
3. **Enable switch** in Registry: `SetEnabled(id, bool)`, `StateDisabled`, `StatusOf` returns disabled; existing state-filtered tools / Inbox / archive unchanged. Start gives each connector context; disable cancels Run / Watch, enable restarts. Hub settings persistence; Infos adds enabled; `PUT /api/v1/connectors/{id}/settings {"enabled"}`.
4. **Delete data** on hub, owning knowledge / Inbox / cursors: `SetEnabled(false)` first to prevent concurrent imports; delete `<id>-*` knowledge chats / people, `Source=<id>` Inbox, cursors/<id>.json. `DELETE /api/v1/connectors/{id}/data`.
- No overlap with Ovis migration M1 #529 Chats / Items, M6 Status.Guide, M3 `InboxReplier`; later merger rebases.

## Optional: existing Telegram service on a computer (not yet)

Some users already run account-hosting Telethon / TDLib CLI or service. Like iMessage, node could be local connector (`docs/plans/plan-connectors.md`, section 2 option 3), reading local service commands; hub registers connector while credentials / sessions stay on computer, e.g. keychain, not hub.
- Benefit: no second login / Telegram device session; reuse authorized service.
- Cost: service-specific commands / output require adapters, limited exposed reads, offline computer has no data, service owns rate limits.
- Not now; finish personad-telegram first. Demand later adds telegram local source via iMessage-style `connector.*` RPC.

## Tests

- No real accounts: small api interface for code send / sign-in / password / chats / history / search / logout, fake implementation with synthetic chats / messages, names Example User. Try gotd tgtest in-memory MTProto login to verify actual encoding; fixtures only if unsuitable.
- Encryption: assert DB row excludes plaintext auth key; logout deletes row.
- Normal privacy scan; logs only session IDs, counts, error types, never body / phone / code / password.
- User performs real-account login in app; afterward hub checks counts only (chats / rows / Inbox).
- **Compare real data shapes**: user's Mac has account-hosting read-only Telethon CLI through Unix socket. Use only status / dialogs / resolve / messages / search, `--limit`≤200, one request at a time, 0.5–1-second gaps, full FloodWait, no polling / bulk fetch. **Never send commands.** Compare only structure: chat types, groups / supergroups, replies, mentions, mute / archive, attachment kinds. Record only structure; real messages / people / group names / phone / IDs never repository, fixtures, PR, issues, logs. Gotd cannot reuse that session; separate authorization.

## Steps

| Step | Content |
| --- | --- |
| T1 | Framework Login, Secrets / `connector_secrets`, `SetEnabled` / disabled, hub login / logout / enable APIs, checked with Ovis |
| T2a | Subprocess connector: `connector/proc`, `connector/serve`, RPC / version alignment / restart backoff / enable lifecycle, fake subprocess tests |
| T2b | `personad-telegram` gotd client, credentials → phone → code → password, hub-encrypted credentials / session, logout, status exposes only api_id; no-gotd personad test |
| T2c | `deploy.sh` / `publish.sh` build, scan, size, programs manifest; coordinated self-update / rollback, Docker, `deploy-hub.sh.new` |
| T3 | T3a incremental knowledge archive + live kick; T3b read tools / prompts; T3c delete stored knowledge / Inbox / cursors |
| T4 | Inbox |
| T5 | App login / switches / deletion / logout, Inbox brand, mock hub, two styles × dark / light screenshots |

## User decisions (2026-11-03)

1. **api_id / api_hash** entered in first app-login step (Credentials above), never hub.json / program / build config.
2. **Initial sync**: last 30 days per chat, max 500 messages.
3. **Chat types**: direct / groups including supergroups; no channels, bots, Saved Messages. Muted / archived knowledge only, not Inbox.
4. **Account risk**: user understands extra Telegram Devices session / rate limits; encrypted session, read-only use.
5. **Size**: option B separate program (Architecture above).
