# Archive: point-in-time records

This directory holds finished lane plans, real-data verifications, phase status tables and acceptance records. They capture the conclusions and evidence **at the time**.
Current docs, code comments and lessons cite them to explain "why it is done this way", but they are no longer updated with the code:

- Source paths, commit hashes, commands and test counts in them refer to the base commit at the time of writing. Paths renamed or moved later are not updated;
  to find the current code, search the repository by name.
- Current design and rules live in the topic docs under `docs/` and in `docs/lessons/`. When a conclusion here conflicts with a current doc, the current doc wins.
- New one-off records (lane plans, verification reports, acceptance checklists, audit snapshots) go straight here when done, not in the top level of `docs/` and not in the CLAUDE.md doc index;
  add a row to the table below at the same time (`scripts/test/test_doc_paths.py` checks it).

| File | Contents |
| --- | --- |
| `web-visual-noise-6183.md` | 2026-11-07 web approval/status color inventory, neutral semantic surfaces, before/after browser screenshot method, local verification and no-deployment boundary (#6183) |
| `persona-only-277-acceptance.md` | Persona-only source naming, versioned local verification, row-lock comparison, preserved application identities and approval-gated cutover limits (#277) |
| `persona-native-6152-acceptance.md` | 2026-11-07 Apple 原生改名验收：全平台编译、单测数量、离线签名、递增构建号升级、phone/watch 配对与未迁移的偏好和 Shortcut（#6152） |
| `ios-perf-5730-final.md` | 2026-11-07 iOS 性能收尾：逐页 cadence/最差 hitch/最长帧、六批已合并修复、67 项主要场景测试清单、未达标与未测边界（#5730） |
| `agents-grouping-5766.md` | 2026-11-06 iPhone Agents 首批分组入口和长列表上限：400-Agent 受控读数、测试入口、零差异说明及未完成的 8000-Agent 边界（#5766） |
| `agents-list-5929-local-acceptance.md` | 2026-11-06 bounded Agents API: runtime/personal-agent callers, synthetic 8,000-session before/after latency, bytes, CPU and memory, local test output and the pending production-deployment boundary (#5929) |
| `codex-project-hierarchy-verification.md` | 2026-11-06 Codex project and task hierarchy verification, including source isolation, parent metadata, grouped tasks and client acceptance (#5620) |
| `english-documentation-acceptance-20261106.md` | 2026-11-06 English documentation acceptance: language rules, bilingual README, all docs translations, merged PRs, link checks, screenshots and disclosed test limits (#5054, #5056) |
| `lanes/live-updates.md` | 2026-10-31 live updates interrupting user actions: three reproductions, root cause, how it was measured, and performance numbers before and after the fix |
| `lanes/msg-render-plan.md` | 2026-10-29 diagnosis and rework plan for the message read and render pipeline (compared against Ovis) |
| `lanes/msg-render-verification.md` | 2026-10-29 real-corpus verification of the message pipeline lane (phases 2–4) |
| `lanes/session-verify.md` | 2026-10-31 real-data verification of session reads and message sending, with per-hop latency |
| `android-p0-status.md` | 2026-10-31 item-by-item status of the first Android release's P0 (source paths predate the package rename to `persona`) |
| `android-p0-acceptance.md` | Android P0 round-4 joint acceptance checklist |
| `ios-p0-verification.md` | 2026-10-31 iOS API acceptance record for the P0 hub changes |
| `hub-contracts-report.md` | 2026-10-31 hub contract check |
| `node-link-update-validation.md` | 2026-11-04 acceptance of Persona Link updating the resident personad over node RPC: verification, idle check, atomic replace, rollback and version confirmation (#1158) |
| `android-capsule-2124.md` | 2026-11-05 first version of the Android floating capsule: cross-app overlay, sending to a session, capsule content contributed by extensions, acceptance evidence and follow-ups (#2124) |
| `android-capsule-compact-2124.md` | 2026-11-05 Android capsule shrunk to a fixed small size; expanding it leaves only three entries: Unread / main chat / Inbox (#2124) |
| `android-capsule-completion-2124.md` | 2026-11-05 Android capsule completion hint: shown briefly (about 4 seconds) when a session finishes, tapping goes straight there, does not interrupt manual cards (#2124) |
| `android-capsule-drafts-2124.md` | 2026-11-05 Android capsule drafts survive the completion hint and auto-collapse (#2124) |
| `extension-model-web-1854.md` | 2026-11-05 web acceptance of the extension model: contribution toggles, dependency-chain disabling, bundle install / uninstall confirmation (#1854) |
| `extension-model-apple-acceptance.md` | 2026-11-05 Apple acceptance of extension model settings: contributions, bundles and declaration management, iOS build and test results (#1861) |
| `extension-notification-apple-acceptance.md` | 2026-11-06 Apple acceptance of the extension notification phase: category registration, tap and foreground / background actions, dark and light UI, and the system placements #1862 has not finished yet |
| `assistant-rich-ui-predeployment-2026-11-06.md` | 2026-11-06 main chat rich UI: real-model pre-deployment loop on the unified production source, earlier failure records, the SDK looks that did not pass, and the production pause boundary (#2017) |
| `apple-html-mac-normal-20261106.md` | 2026-11-06 Mac normal matrix for the latest Apple Coordinator: 13 methods / 21 expanded cases, termination protection, verbatim-extraction fingerprints, and the unfinished infinite-loop / real OS termination boundary (#2025) |
| `apple-rich-ui-shots-20261106.md` | 2026-11-06 Apple rich UI empty states and screenshot wrap-up: 60 shots across five styles in dark and light, real Mac app body screenshots, line-height fix and failure records (#2024) |
| `plan-telegram.md` | 2026-11-03 plan for the Telegram connector (gotd, step-by-step login, Inbox, `tg` CLI); since 2026-11-05 it is an extension (#1704; from 0.1.3 the code lives in `nwverse/persona-extensions`, #2057), not in this repository |
| `codex-project-hierarchy-verification.md` | 2026-11-06 verification of Codex project grouping and the task hierarchy (Desktop projects by canonical path and Git common dir, parent / shared-root metadata through node, manager and APIs, Android collapse in all styles) (#5620) |
