# Android P0 round 4 joint acceptance checklist

## What you need to prepare first

The coordinator and the environment owners prepare the following. This checklist only uses placeholders; real addresses, accounts, machine names, credentials, and raw business content stay in configuration or private acceptance records outside the repo.

| Item | What to prepare |
| --- | --- |
| A controllable hub environment | `<acceptance hub>`, `<separate database>`, `<acceptance commit>` and the matching node; an isolated environment that can be stopped, restarted, and made to drop responses, with redacted logs. Fault injection runs only in this environment. |
| hub host firewall | Allow inbound `<TLS port>` and confirm the hub is actually listening; with containers, also check the port publishing rules. See [Public direct connection](../../operations/network.md). |
| Router and IPv6 | The ONT / router allows inbound `<TLS port>` to `<hub IPv6 address>`; confirm the home broadband has public IPv6 and the phone can reach it over cellular. Reachability from the same Wi-Fi alone doesn't count as a result. |
| DDNS and TLS identity | Prepare `<DDNS domain>`, `<DNS credentials file>`, `<pairing fingerprint>`; configure `public.ddns` or an external DDNS, and put the HTTPS domain candidate in `public.advertise`; adding a candidate alone does not update DNS. Prepare a reversible certificate / prefix change plan. |
| Computers to connect | Configure SSH and `machines` on the hub for `<connected computer>`; confirm directory permissions, herdr, node, Claude Code / Codex / an internal Codex variant, and launchers work; wrapper launchers declare `agent_type` in `machines[].launchers`. See [Configuration](../../operations/config.md). |
| Lark gateway | Enable the agent API on `<Lark gateway instance>`, fill `base_url` and `token` in the hub's `lark`; prepare a `<test chat>` that can send and receive, a `<test thread>` with enough history, messages that can be edited / recalled, and @me items. See [Server](../../server/server.md) and [Lark](../../extensions/feishu.md). |
| Mail | The hub has `<test mail account>` connected and can send and receive test mail; prepare plain text / HTML, multiple UIDs, changed targets, same-name attachments, different MIME types, no-viewer and interrupted-download cases, all content redacted. |
| iOS push and compatible devices | The hub has `apns.json` and the `<APNs key file>` and `<push environment>` it references; prepare the existing iOS App and an iPhone that can receive pushes, to check coexistence with old clients and notification policy. Android still uses only the persistent WS. |
| Android test phones | Prepare `<device id>`, `<ROM / version>`, `<system API>`; cover Android 16, 17 and different Chinese OEM ROMs, with camera, photos, file picker, and attachment viewers. Prepare `<SIM id>` for three carriers, Wi-Fi and cellular, without relying on Google services. |
| Background and notification conditions | Be able to change the notification master switch / channels, lock screen details, elevated notification permissions, local network permission, battery optimization, auto-start, background restrictions, network during doze, and recents settings; prepare unplugged overnight conditions. Record every settings combination. |
| Multiple devices and re-pairing | At least two Android phones and one existing iPhone online at the same time; prepare `<pairing A>`, `<pairing B>`; be able to revoke a device and re-pair, to verify device and hub generation isolation. |
| MCP requests | Prepare `<test MCP server>` that can trigger form and URL elicitation, including required / invalid / sensitive fields; Claude has picked up the new hooks after restart. Prepare an old client that doesn't declare elicitation capability for comparison. |
| Test material and release builds | Prepare a redacted long session with 650 commands, Claude / Codex records with and without native turn durations, images / history that can exceed the cache budget, and `<installed APK>`, `<upgrade APK>` with the same package name and signature, plus a package that fails verification for comparison. Signing keys stay outside the repo. |
| Recording and judging criteria | The coordinator first fills `<performance thresholds>`, `<battery thresholds>`, `<survival requirements>`, `<recovery time limits>`, with explicit timing points and sampling tools; prepare a `<raw records directory>` outside the repo. Items without agreed thresholds can only be recorded, not judged as performance passes. |

This checklist corresponds to the current baseline commit `f77a76eca205168a93015cefb26825374013ddb7` of the [status table](android-p0-status.md), dated 2026-10-31. This round only wrote the checklist; **no build, test, install, App, screenshot, or real hub was run; none of the items below were measured**. During actual acceptance, record the new App / hub / node commits and build versions; don't treat this checklist as a pass record.

## Common recording rules

Every record uses anonymous ids for devices, accounts, machines, and pairings. The repo keeps only redacted conclusions and private evidence ids; never paste addresses, real messages, attachment content, mail addresses, tokens, SSH details, raw certificates, or URLs with credentials. It's fine to keep whether a field is present, result codes, consistency of redacted IDs, and reviewed summaries.

Performance, battery, latency, connection frequency, throughput, concurrency, and survival data are grouped separately by "ROM / OS version × carrier / network × foreground / background / power-saving setting × scenario", with **at least 10 independent samples per group**. Cold / warm start, success / failure, and different networks are not mixed. Overnight runs: at least 10 nights per group, each with a planned 8-hour locked, unplugged observation. These are sampling plans; all results are currently "not measured". Functional steps can be run case by case, but if they cite a duration or success-rate baseline, the sampling requirements apply too.

Every numeric metric records `n`, unit, `min / p50 / p90 / max / mean`, and the raw sample index. Percentiles always take the `ceil(p × n)`-th item of the ascending samples; mean is the arithmetic mean; missing measurements are not recorded as zero. Failed, timed-out, manually aborted, and still-alive-at-end samples are each labeled; record total attempts, valid count, and failure count; nothing is silently dropped. A connection still open at the end of observation is recorded as "not dropped as of end of observation"; the observation length must not be taken as the real lifetime or idle timeout.

Timing uses alignable event times and a monotonic clock, with explicit start and end points; when clocks across devices can't be aligned, don't report cross-device end-to-end durations. Configured timeouts, heartbeat intervals, budget caps, and SLAs are only configuration, not measured results. With fewer than 10 samples, write only "not fully measured, n = <sample count>"; it can't serve as a baseline.

| Per-group, per-metric record template | Initial value |
| --- | --- |
| Acceptance item / scenario / environment / commit / settings combination | `<to fill>` |
| Metric / unit / start and end points / sampling tool | `<to fill>` |
| Sample count n / attempts / failures / raw evidence ids | Not measured |
| min / p50 / p90 / max / mean | Not measured |
| Pass criteria / result / anomalies and next steps | `<pre-agreed criteria>` / not measured / `<to fill>` |

V01–V10 below map one-to-one to "On-device real-environment acceptance" in the status table. The former A05 (mail attachment budget and write-on-exit) has been completed in code with unit tests; the matching sub-items in V02 / V07 / V08 can only be closed after re-verification on a device. After verifying, first fill in each row's evidence and remaining issues; rows marked "Done" also need real records. Only when the whole corresponding scope passes can "Device-only verification" be changed to "Done". Upstream limitations, deferred implementation, and product confirmations each stay as they are; one passing environment doesn't close them.

## V01: Pairing, public network path, and connection recovery

Status table rows: 1.2, 1.3, 1.4, 1.14, 16.1–16.4, N45, N47, N48, N57, N61–N63, N69, A1-network. Current result: **not measured**.

**Preconditions**: TLS, firewall, inbound IPv6, DDNS, and the machine list are ready; the Android 17 device can deny / restore local network permission; also have a local compatibility entry point and a controllable endpoint with a mismatched identity.

**Steps**:

1. Pair by scanning, by manual entry, and by opening a pairing deep link; check the staged progress, candidate addresses, certificate, and hub identity. Deny local network permission, then grant it from Settings and return. For temporary codes, do a valid redemption, a reuse, and an expired one.
2. Turn off Wi-Fi, pair over cellular public IPv6, open an ordinary chat and a Lark window, and do WS streaming, HTTP history paging, sending, and replying. Switch Wi-Fi / cellular and cause brief network drops; watch the shared title status and the diagnostics page.
3. Delay the catch-up and reply reconciliation of an opened window; check that Updating stays until reconciliation completes. Past the configured wait limit, check that after leaving the wait, the unreconciled-window marker and the read gate remain.
4. In the isolated environment, change the IPv6 prefix and confirm DDNS updates and the domain candidate is found again; simulate DNS failures, refused connections, no IPv6, certificate / hub identity mismatch, and certificate rotation, and check the error categories and recovery entry points.
5. Revoke the current device token, trigger HTTP and WS auth separately, and confirm that after 401 it stops reconnecting and goes to re-pairing. Check that credentials are never sent in plaintext over the internet, and the local compatibility entry point still works.

**Pass criteria**: the correct endpoint completes real send and receive; IPv6 reachability and domain recovery have separate evidence; mismatched identity refuses the connection; restoring permission lets it continue. All pages share the current route and connection state, and unreconciled cache can't report read. After 401 there's no endless reconnecting; business errors give reconcile, terminal, or re-pair actions according to code / delivered / retryable. Recovery times must meet the pre-filled criteria.

**Data to record**: anonymous network type, candidate path category, per-stage results, certificate match conclusion, error code / recovery action, reconnect and window-verified order. Pairing duration, probe RTT, network switch to WS usable, network switch to HTTP usable, and window reconciliation duration each need at least 10 samples per group, with n and the five statistics; currently not measured.

**Fill in after verifying**: all the rows above. Only when cellular public send / receive, paging, and network switching all pass does A1-network's real acceptance gap close. Permission, identity, or reconnect failures stay in their specific rows; don't only change the summary row.

## V02: Long sessions, turn durations, and cache budgets

Status table rows: 1.6, 7.5–7.12, 7.15, N14, N35, A1-engineering, A1-chat, A1-long-session, UX3.3-failure, UX3.3-search, UX3.3-task, UX3.3-reader. Current result: **not measured**.

**Preconditions**: a real session with 650 commands; long lists of all three kinds (ordinary Agent / assistant / Lark), with images and long tool output; Claude / Codex samples with native duration markers and controls without markers; record the actual cache budget configuration used.

**Steps**:

1. Clear the disposable cache and the retained cache of this acceptance scope separately, then do cold / warm opens; record tap-to-cached-first-screen and tap-to-reconciled-first-screen times, without mixing the two.
2. Page back through history, expand / collapse fold_work, scroll fast, leave the latest end and wait for new messages, then return to latest. Check freezing, reading anchor, image layout, streamed final text, failure / no-result notices, task jumps, search return, and copy-all.
3. Check work-row / status-row durations against the raw records; only accept explicit associations with Claude turn_duration or Codex task_complete. Hide the duration when the marker is missing, the association is missing, or the association hasn't loaded yet; a new turn must not reuse an old value.
4. Open more sessions step by step and load images / Inbox media and mail attachments until this run's configured budget is exceeded; check LRU eviction of unused data, protection of in-use data, actual image decode sizes, and recovery after restarting in the same scope.
5. Measure the actual size of items JSON, images, Inbox media, and SQLite / WAL separately; separate protected from evictable data, and list the mail attachment subdirectory on its own so it isn't missed. The SQLite physical file has no hard cap, so JSON eviction can't be claimed to free the same amount of disk.

**Pass criteria**: paging is continuous with no duplicate final text; reading position and freezing aren't pushed away by new streams; expand / search / task return are reachable; durations match the native records and turns. Evictable data respects the configured budget, in-use data is never deleted by mistake, and an insufficient budget gives an understandable result. First screen, dropped frames, memory, and storage metrics are judged against pre-agreed criteria; the default budget still needs this round's data to support it.

**Data to record**: session type, command / row / image scale, cache state, budget configuration, protected objects, eviction order, and native duration source / anonymous association. At least 10 samples per group; first-screen time, scroll dropped frames or frame time, peak PSS, each cache type and database size each recorded with n and the five statistics; currently not measured. Turn durations recorded by the tools themselves are not client performance measurements.

**Fill in after verifying**: the rows above, focusing on data for 1.6, N14, N35, and A1-long-session; a single scroll or static association check can't close A1-long-session.

## V03: Background connection, battery, and Chinese OEM systems

Status table rows: 1.4, N55, N64, N70, N72, A1-background, UX3.7-onboarding. Current result: **not measured**.

**Preconditions**: devices with different ROMs, three carriers, viewable device connection records and system battery data; system permissions, battery optimization, auto-start, and background settings recorded; confirm that disconnects caused by hub restarts can be attributed separately.

**Steps**:

1. Tap each vendor jump in the preparation checklist one by one and confirm it lands on the right page; items that can't be queried stay as manual confirmations, and "tapped it" can't be written as "the system allowed it".
2. Observe idle connections and heartbeat negotiation ACKs separately for Wi-Fi / cellular and foreground / background; add network switching, Doze, lock screen, and network-during-sleep restrictions. Stock Doze is only one of these; also test the vendor's actual policy.
3. Do at least 10 nights per group of locked, unplugged overnight observation, 8 hours planned per night; keep start / end battery, observation length, every disconnect, recovery, process exit, and hub restart record.
4. Swipe away from recents, stop from the system's "Active apps", force-stop, and reboot the phone, each separately; check service state, stop notice, and user-initiated recovery. Tell user stops apart from system reclaims; don't assume an auto-recovery the system forbids will happen.

**Pass criteria**: system settings state is trustworthy and failed jumps can go back; foreground and background heartbeats actually take effect and are observed. Recovery after natural reclaim / reboot and after an explicit user stop follows system constraints without duplicate connections. Battery, survival, and recovery meet pre-agreed criteria; if a carrier or ROM causes lost notifications / long disconnects, record it as failed or pending; receiving ACKs alone doesn't make it pass.

**Data to record**: for each settings combination, connection established / dropped times and reasons, actual heartbeats sent and received, exit reasons, battery and actual observation length. Battery per hour, disconnect count, recovery time, and observable survival time need at least 10 samples per group, with n and the five statistics; currently not measured. Samples that never disconnected note the observation cutoff; a timeout isn't taken as the idle timeout.

**Fill in after verifying**: the rows above; N55 and A1-background must have real records per ROM / carrier. N72's reports and connection diagnostics only help; lock screen visibility still requires V04.

## V04: Real requests, answering from notifications, and Live Updates

Status table rows: 2.11, 5.3, 7.30–7.32, 13.8, 17.1–17.5, N10, N16–N18, N50, N56, N58, N72, A1-notifications, UX3.1-requests, UX3.4-presentation, UX3.4-extension, UX3.8-platform. Current result: **not measured**.

**Preconditions**: real Agents and MCP can trigger every kind of request, and notification permission / channels can be toggled; Android 16, 17, and at least one control device that doesn't support notification promotion; use redacted content for actual operations.

**Steps**:

1. Trigger permission, plan, single-choice / multi-question, screen cards, and the assistant's ask_user; go straight from the Agents card to the full page and open the session. After entering a reason, check that Allow is disabled; after entering plan feedback, check that Approve is disabled; the full-text gate allows "allow all" only inside the App.
2. Receive requests with the screen locked: Allow / Approve unlock first; Deny / send back use RemoteInput; check options for single-choice / screen cards. Tap repeatedly on purpose, handle it on the computer first, let the request expire, or change the screen prompt; check disabling, withdrawal, 410 / prompt_changed, and the terminal fallback.
3. Start an MCP form covering strings, numbers, booleans, single / multiple choice, validation failures, and sensitive fields; submit, decline, and cancel separately. In URL mode, check the full URL / host; it must not continue if not opened or opening fails. Only continue after actually opening the given link and returning from the web page; a prompt to open doesn't mean authorization succeeded.
4. Check that the request history for this pairing is read-only and can't be answered again; this entry point doesn't count as covering past-session scope. After sending the answer back, go to the computer and confirm the Agent really continued; don't just look at "Your choice was sent back".
5. Set done=long / always / off separately and trigger short / long tasks, seen, muted, currently viewing in the foreground, and assistant completion; check notification policy and cold-start routing. Watch that foreground work running to completion produces exactly one system confirmation haptic, background / duplicate completions don't add more, and turning off system haptics is respected.
6. Toggle Live Updates: on by default on the device; when off, the ordinary persistent notification remains; it degrades when promotion permission is denied / unsupported. On Android 16 check BigText, on 17 and above check MetricStyle; lock screen hides details by default, and after tightening privacy, old details are withdrawn or refreshed. Scheduled tests must manually log ringing, screen wake, and whether it's visible.

**Pass criteria**: each request type's three-state result, deadline, and safety gates are correct; answers are not auto-resent; the real Agent continues or refuses as chosen; passwords / sensitive answers and URL-open credentials aren't written to disk. Notification policy, privacy, routing, and haptics match device preferences. It degrades normally when the system doesn't promote. When upstream lacks tool association / config source, it shows "Not provided" and doesn't fabricate information.

**Data to record**: anonymous request / operation ID mapping, request type, answer entry point, unlock requirement, whether the reason / feedback arrived, evidence the Agent continued, repeated tap count, withdrawal and expiry results; record permissions, ROM, API, style, switches, and manual visibility. Notification visible latency and answer-to-Agent-continues time need at least 10 samples per group, with n and the five statistics; currently not measured.

**Fill in after verifying**: the rows above; A1-notifications closes only after a real lock screen → Agent continues run. UX3.4-extension keeps U02, UX3.1-requests keeps A01; N72 can't close the visibility gap on the strength of received reports alone.

## V05: Offline sending, attachment drafts, and assistant handoff

Status table rows: 1.12, 1.13, 2.2, 2.5, 7.20, 7.21, N5, N6, N12, N49, UX3.1-drafts, UX3.9-assistant. Current result: **not measured**.

**Preconditions**: two pairings, with and without a main chat; the computer's input box can hold a draft or open a dialog; system photos, camera, files, and external share sources are available; the network can be cut and the App process killed during upload / send.

**Steps**:

1. Send a message while the computer has a draft, once cancelling and once confirming the clear before sending; cause a lost receipt after sending, and check the original client_msg_id, the unknown result, and manual handling; it must not count as success or be silently resent.
2. With no main chat and offline, send the first message from the assistant home, and also go through share, Agents, knowledge base, Lark summarize / draft reply, and mail AI handoff; check that it is persisted to the queue first, switches to Persona, then binds to the main chat once online.
3. Pick photos, take a photo, pick files, and insert a path in the middle at the cursor, covering both the ordinary and the assistant home input boxes. Already-uploaded attachments are queued with the message; attachments not yet uploaded keep the draft when offline and prompt to retry once online.
4. Save a draft with attachments, kill the process, and reopen; revoke the source URI permission, delete the source file, have the session replaced / history rewritten, then reopen and check the invalid entries and the old-version notice. Keep editing during sending, and confirm the receipt only clears the matching version.
5. Share content from various sources and kill the source process; confirm the copied scoped queue still works; check that share target ordering, muting, and seen semantics match Agents.

**Pass criteria**: definitely-not-sent, rejected, and unknown results are told apart correctly; the original ID can be reconciled, and the same content isn't sent twice because a page reopened or the process recovered. Every main chat handoff can queue offline; attachments recover or clearly say they're invalid; failures keep the input; new edits aren't cleared by old receipts.

**Data to record**: entry point, pairing / session anonymous ids, original ID consistency, outbox state transitions, upload state, URI permission changes, draft version, actual send count, and recovery result. If upload / recovery times are reported, at least 10 samples per group with n and the five statistics; currently not measured.

**Fill in after verifying**: the rows above, noting coverage of with / without main chat, ordinary / home attachments, and share sources separately; an ordinary chat send passing can't substitute for the knowledge base or the assistant's first message.

## V06: Session operations, launch receipts, and the back stack

Status table rows: 5.4, 5.5, 5.8, 5.11, 5.13, 5.14, 6.4, 9.3, UX3.3-state; also fills in 1.1. Current result: **not measured**.

**Preconditions**: real connected computers and all kinds of launchers; the hub's client_ops can be queried; prepare chat / request / terminal back stacks for the same session across several tabs; be able to control request cancellation, lost receipts, and hub restarts.

**Steps**:

1. Pin, mute, rename, do space operations, and hand to Persona; check that other lists, badges, notifications, and the Inbox update. Check mute exceptions against the hub's rules.
2. Launch Claude / Codex / an internal Codex variant separately; check the selectable agent_type, the actual launcher, and session ownership. When an old response has no agent_type, keep the Claude-compatible behavior, and don't show Claude-only modes / effort for other types.
3. Drop the response after creating a session, reopen the form, kill the App, restart the hub, and query the receipt with the original operation ID. Replaying the same ID with the same parameters creates no new session; reusing the ID with changed parameters should be rejected; pending / unknown doesn't create duplicates.
4. For resume, reconnect, and close, cause cancellation and lost receipts separately; reopen the App and tap again; check that the one-time operation log is still there and is reconciled read-only, not triggered again. The record is released only after an explicit rejection or successful reconciliation.
5. After confirming close or reconciling a successful close, go through each tab's back stack and make sure the session's chat / request / terminal routes are all removed, requests and notifications withdrawn, and the list and Inbox consistent.

**Pass criteria**: the number of real side effects matches the number of operations, and there's a reconcile entry point when the result is unknown; no duplicate launch / close / resume / reconnect. After closing, going back never enters a removed session; mute / badge links are correct; missing old fields stay compatible.

**Data to record**: anonymous machine / session and launcher type, operation ID and parameter summary consistency, receipt state, records before and after restart, actual sessions added / closed, back stack check conclusion. If launch and reconciliation times are cited, at least 10 samples per group with n and the five statistics; currently not measured.

**Fill in after verifying**: the rows above; 5.13 / 6.4 add start receipt evidence, 5.11 / UX3.3-state add evidence of cancellation and cross-restart reconciliation, 9.3 / 1.1 add the linked results.

## V07: Lark windows, mail replies, and attachments

Status table rows: 9.8, 10.1–10.3, 12.1, N15, N39–N42, UX3.5-list, UX3.6-drafts, UX3.6-threads, UX3.6-read. Current result: **not measured**.

**Preconditions**: the Lark gateway and mail are configured; @me items, edits / recalls, history gaps, threads, and new mail can be created; prepare same-name attachments with different content, and usable and unusable viewers. Source read state follows read-on-visible (confirmed by the user 2026-10-31): when opened from the Inbox, messages actually seen on the first screen are reported read too.

**Steps**:

1. Open a Lark chat from an Inbox item as the starting point; check that the cache is drawn first, the divider, and backward / forward paging for ordinary chats. Add / edit / recall while offline; after reconnecting, check authoritative replacement of the full window, gaps returning to latest, and handling of non-contiguous pages.
2. Obscure, background, initial positioning, programmatic scrolling, and unreconciled cache each separately; confirm none of them report source read. Once the gate is met, check the real source result. When opened from the Inbox, messages on the first screen after the gate is met must also be reported read; "done" only affects Persona.
3. Page back in a thread, then tap "↓ Latest replies" to refetch the latest page; verify window replacement and replies. Explicitly don't test after-contiguous paging, which doesn't exist, and don't stitch threads using ordinary chat cursors.
4. For Lark direct replies, thread replies, AI-drafted replies, and mail replies, separately save a draft, cut the network, lose the receipt, and restart the App / hub; confirm the original client_id, persistent receipt / echo reconciliation, and that unknown results aren't resent.
5. Open plain text / HTML mail, multiple UIDs, and links; explicitly toggle read / unread and check the source. While editing a reply, add new mail or change recipients; check that a UID / version conflict returns `mail_reply_target_changed`, keeps the draft, shows the latest target, and asks for confirmation again; the offline queue keeps the original target.
6. Download and open same-name attachments with the system separately; check stable IDs, complete content, and progress. In an isolated authenticated HTTP client, verify Range / Content-Range, invalid ranges, and rejection without auth. Interrupt an App download and tap retry; check that the partial file is never opened and the failure notice appears. Delay directory creation / renames before and after leaving the pairing, and re-verify the former A05 fix (late writes rejected, directory not recreated); without a viewer, keep the downloaded notice. The current UI retries by downloading from the start; the transfer API supporting offset doesn't mean the UI supports resumable download.

**Pass criteria**: windows have no fake continuity and no duplicate messages; edits / recalls update by completeness; read state meets the gate and read-on-visible semantics; unknown replies are reconciled only by the original ID, with no duplicate sends. When the mail target changes, nothing is sent to either the old or new target until the user confirms again; attachment auth, Range, content, scope, and failure recovery are correct.

**Data to record**: window start and end and cursor direction, sync_state, verified, read reports / actual source changes, original client_id and actual send count; mail UID / version changes, conflict code, whether confirmed again; attachment anonymous ID, size, content verification conclusion, Range status code, and retry result. If paging, reconnect catch-up, and download times are cited, at least 10 samples per group with n and the five statistics; currently not measured.

**Fill in after verifying**: the rows above; 12.1 records body, reply conflicts, and attachments separately; the semantics of UX3.6-read are confirmed, so fill in evidence once measured and passing; UX3.6-threads keeps U01.

## V08: Multiple devices, iOS compatibility, and cleanup on leaving

Status table rows: 1.3, N4, N9, N13, N65, N69, UX3.4-extension, UX3.8-devices, UX3.8-platform. Current result: **not measured**.

**Preconditions**: two Android phones and the existing iOS connected to a controllable hub at the same time, with different phone tool permissions per device; pairings A / B can be switched; uploads, reading position writes, reply callbacks, and shell saves can be delayed.

**Steps**:

1. Register tools on each, switch the foreground, and revoke calendar permission / tool switches; check routing by device capability and online state. Calendar writes must be confirmed on the target phone; Android doesn't override iOS-only tools.
2. Trigger an ordinary interaction and an elicitation at the same time; check that the Android that declares the capability gets the new type, and the iOS that doesn't declare it doesn't. With only old clients, it falls back to the terminal; ordinary streaming, questions, approvals, push, and sending messages still work.
3. Set Live Updates, lock screen details, and done preferences separately on the two Android phones, read them after restart, and check they don't leak across devices. Revoke one device and check how its connection, tools, and push registration are handled, while the other one and iOS keep working.
4. On pairing A, leave text / attachment drafts, images, reply / operation logs, Lark anchors, shell position, and pending callbacks; after disconnect / revoke / 401, re-pair to B, then release the old callbacks and restart the App. Check that the old data is cleared, directories aren't recreated late, and B shows none of A's drafts / routes / state.

**Pass criteria**: device registration, revocation, and permission updates are accurate, and writes are confirmed; the old iOS works normally throughout. After leaving, scoped data and compatibility records don't leak into the new generation, old instances can't restore deleted directories, and each device keeps its own notification preferences.

**Data to record**: anonymous device / pairing generation, platform / capability set, tool registration, revocation and where calls actually land, whether new types are filtered, results of ordinary iOS flows; file categories before and after cleanup, late callback results, and a comparison of preferences on both ends. If cleanup / revocation latency is cited, at least 10 samples per group with n and the five statistics; currently not measured.

**Fill in after verifying**: the rows above; N4 / N65 add cleanup and late-write evidence, N13 adds multi-device routing, UX3.4-extension adds the old iOS compatibility conclusion and still keeps U02.

## V09: On-device APK update

Status table rows: N71. Current result: **not measured**.

**Preconditions**: the installed version and a higher version have the same package name and signature, and the hub hosts a matching manifest; prepare hash-mismatch, signature-mismatch, user-cancel, and network-interruption controls; be able to toggle the install-unknown-apps permission.

**Steps**:

1. Check for updates in the App, verify the version and notes, and download over the current trusted route; deny the unknown-sources permission, then allow it from system Settings and retry.
2. Inject a hash mismatch and a signature mismatch separately, and confirm the old version is kept on failure; a valid package brings up the system confirmation; cancel and confirm the install separately.
3. Open after upgrading and check pairing, text / attachment drafts, reading position, outbox, replies, and one-time operation logs, and watch the persistent service recover; re-verify on each ROM.

**Pass criteria**: failure / cancel doesn't damage old data or bypass the system install confirmation; after a normal upgrade, real send and receive continue and the persistent service recovers. A system-initiated stop / restriction must have a recovery entry point; not recovering can't be recorded as an update pass.

**Data to record**: anonymous device, package name / signature consistency conclusion, version, manifest and package verification results, permission / confirm / cancel state, data category checks, and service state. Download / install / recovery times need at least 10 samples per group with n and the five statistics; currently not measured.

**Fill in after verifying**: N71's per-ROM results; it changes to done only after same-signature upgrade, failure controls, data retention, and service recovery all pass.

## V10: Visuals, accessibility, and the overall P0 record

Status table rows: 22.1, 22.7, 22.8, 22.10, 23.1, N2, N54, A1-dev-acceptance, A2-scope, A2-acceptance; other issues found are filled into the feature rows they belong to. Current result: **not measured**.

**Preconditions**: the coordinator arranges a unified UI acceptance and prepares the key page and state matrix and the prototype (approved 2026-10-31); have debug / Release artifacts matching the acceptance commit and the existing token baseline record. This round's documentation lane doesn't build, install, or take screenshots.

**Steps**:

1. Check the four tabs, pairing, chat / assistant, full request page, Inbox / mail / threads, settings, and system notifications, covering dark and light mode, dynamic color, default / large / accessibility font sizes, and narrow screens. Confirm button labels are single-line, actions are reachable, and the system splash screen matches the brand.
2. With TalkBack, check that counts read the full number, icons have understandable names, and decorations don't get in the way; test keyboard appearance, predictive back, overlay dismissal, web page dragging and interacting with the chat behind it, and that the reduce-animations setting takes effect.
3. In debug, check each fixed-state entry point one by one; check that the manifest / dex of the existing Release APK contain no debug Activity, fixed-state fixtures, or reachable debug entry points, and log the artifact version and check evidence. Jumping straight there in debug only proves the entry point; it can't replace real fault recovery.
4. Check the token adoption baseline and this run's per-file delta records. It's currently a standalone script, and it's not assumed that Gradle / CI has run it automatically; the coordinator arranges the check separately and logs the result. Dynamic color is still verified on actual pages.
5. Log, row by row using the status table's original numbers, the applicable scenarios, devices, real hub / account results, and evidence, and check that V01–V09 are complete. Uncovered, failed, insufficient-sample, and restricted items are each kept as they are.

**Pass criteria**: the listed pages are operable and screen-readable under the agreed matrix; Release has no debug entry points, and the token delta has a clear result. Every Android P0 has real-environment evidence or a clear not-passed conclusion; A2-acceptance closes only when all applicable P0s pass. User approval, past-session scope, and the Swift exit still have to be handled separately; a visual pass doesn't replace them.

**Data to record**: the page / state / theme / font size / device matrix and issue list, TalkBack / keyboard / back results, Release check evidence, token baseline version, and each P0's pass / fail / not-tested reason. If rendering or launch performance is reported, reuse V02's requirement of at least 10 samples per group and the five statistics; currently not measured.

**Fill in after verifying**: the rows above; N54 is updated separately based on the approval record, N2's Swift exit stays a gap, A1-dev-acceptance adds the actual check records, and A2-acceptance is updated once all thresholds are met. Row numbers and row order in the table stay unchanged.
