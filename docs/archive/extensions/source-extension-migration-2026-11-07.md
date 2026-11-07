# Server-rendered source extensions: migration and acceptance record

Record date: 2026-11-07. Scope: [#5956](https://github.com/nwverse/persona/issues/5956).

This is a one-off implementation/acceptance snapshot, not release instructions. The maintained contracts are [extensions.md](../../extensions/extensions.md) and the [stream schema](../../../schema/extension/README.md).

## Acceptance status and release boundary

- The canonical schema, server JSX/hooks SDK, lossless native tree, isolated Deno runtime, manager page sessions and authenticated app transport are merged.
- Web and Android consume the final transport. Both client PRs contain inspected screenshots made from actual sandboxed showcase source and synthetic data. They do not execute extension JavaScript or HTML.
- **Both complete, unreleased source profiles are accepted and merged:** [showcase source PR 308](https://github.com/nwverse/persona-extensions/pull/308) and [Hacker News source PR 309](https://github.com/nwverse/persona-extensions/pull/309). Earlier client-acceptance wiring was not treated as complete source packaging.
- No production deployment, live configuration change, extension version bump or market release is part of this work.
- **Production activation remains blocked** by [kernel enforcement gate #5975](https://github.com/nwverse/persona/issues/5975). Development resource fallback is not kernel resource acceptance.
- Apple implementation belongs to [handoff #6176](https://github.com/nwverse/persona/issues/6176), outside this acceptance environment.

## Architecture actually implemented

One shared, explicitly installed and verified Deno executable hosts **one separately sandboxed process/isolate per extension**, not one shared JavaScript PID. This topology preserves independent OS, resource, credential and lifecycle boundaries. The pinned engine used for development acceptance was Deno 2.9.6.

The SDK derives fixed native components from the canonical schema and supports server-side state, effects, fetch and interval hooks. Services and headless work remain available when a UI page is hidden; UI-only subscriptions/polling suspend. Hook/fetch abstractions are separate from transport, but client-side component execution is **not implemented**.

The manager fits and validates native snapshots, computes keyed atomic deltas and invokes callbacks only after saved-action and permission checks. The hub authenticates and forwards; it does not run extension business logic. Frames target the exact authenticated connection and do not enter global broadcast/replay.

Recovery is snapshot-first. An open retry preserves its original ID and complete payload and reuses its owner-bound allocation; changed payloads and live-connection theft are rejected. Only a correlated structured `expired` response allows a fresh allocation. An `inactive` response is not permission to reopen.

Every entered or ambiguous callback consumes exactly one wire revision, including empty deltas. Receipt replay precedes stale checks and cannot repeat the callback or its effects. Native tree recovery reaches the receipt version before presentation effects. Receipt-history eviction is bounded without becoming a lifetime action limit. Source render and wire revisions are independent.

System widgets and similar surfaces remain fitted snapshots. Older clients use owner-bound full-View HTTP sessions and opaque checkpoints; those checkpoints cannot resume as app-WebSocket sessions. User-facing result effects are presentation only, not a second source of tree state or authority.

## Merged implementation evidence

All PR numbers in this table refer to `nwverse/persona`.

| Area | PR | Evidence and scope |
| --- | --- | --- |
| Canonical stream schema and generated contracts | [6034](https://github.com/nwverse/persona/pull/6034) | Strict field/presence/numeric validation and generated platform codecs |
| Server JSX/hooks SDK | [6040](https://github.com/nwverse/persona/pull/6040) | Fixed component authoring and transport-independent hooks |
| Lossless keyed native tree | [6089](https://github.com/nwverse/persona/pull/6089) | Atomic topology updates; optional false/zero/empty values preserved |
| SDK boundary repairs | [6123](https://github.com/nwverse/persona/pull/6123) | Traversal/budget bounds, fetch/effect identity and component validation |
| Program transport and SDK distribution | [6137](https://github.com/nwverse/persona/pull/6137), [6140](https://github.com/nwverse/persona/pull/6140) | Bounded RPC lifecycle and complete official SDK distribution |
| Confined validator, Deno and explicit development resources | [6145](https://github.com/nwverse/persona/pull/6145) | Disposable parser process, sealed artifact, registered egress, production refusal and real development engine tests |
| Manager sessions and native app WebSocket | [6174](https://github.com/nwverse/persona/pull/6174) | Real process page bridge, owner/session epochs, callbacks and receipt fencing |
| Authenticated hello capability | [6198](https://github.com/nwverse/persona/pull/6198) | Manager transport support advertised without bypassing client/profile/authorization gates |
| Lost-open recovery and structured errors | [6261](https://github.com/nwverse/persona/pull/6261) | Concurrent/reconnected open identity, bounded allocations and typed recovery codes |
| Legacy source context and canonical callback tree | [6309](https://github.com/nwverse/persona/pull/6309) | Old-client in-page navigation receives the requested UI version; private tree keys and final HTTP downgrade remain separate |
| Web final native transport | [6275](https://github.com/nwverse/persona/pull/6275) | Actual source-network acceptance and 12 attached light/dark browser screenshots |
| Android final native transport | [6297](https://github.com/nwverse/persona/pull/6297) | Actual source-network acceptance and 12 attached native/host-permission graphics |

Backend runtime merge: `c90c2f58c32d67bda58542114884439241131138`; page bridge: `c9f289c2a90295ffea2eec152d6d71c899d77d80`; recovery: `fc9ed400652375b5c58929c190bd9f85a75ed8d2`.

### Legacy compatibility repair discovered during source acceptance

Actual complete showcase packaging exposed a host bug missed by snapshot-only tests: HTTP mount replaced the requested UI version with 6, so source callbacks selected native push/pop; the old response correctly stripped those effects but never changed pages. A partial fix that downgraded the internal callback tree also failed because required keys were removed. Neither failure is reported as passing.

PR 6309 merged as `cc94331b81f34d055127967a243e9ec15dfd37bc`. It preserves the actual source context and separately fits the private callback tree to the canonical version, then downgrades only the final HTTP response. Native session semantics, ownership, permissions and unsupported-effect gates remain unchanged.

The initial repair passed 1,853 production test/subtest cases (extmgr 691, extwire 1, hub 1,161), with no failures and six explicit skips: three source build/pack tests requiring unavailable kernel validator enforcement, and three manual/recorded push tests lacking their opt-in inputs. Page race checks passed 36 cases without failures/skips and vet passed. A subsequent pure projection regression passed independently; final current-main integration passed all 36 page regressions. Actual complete showcase archive acceptance on frozen repair `6a49688ce8effd6b5e6998511c71e295fdc2f378` passed 63 checks, including legacy versions 1/2 navigation/search/filter/pagination/detail/edit/save/back and stale checkpoint rejection. Current-main integration did not change the accepted extension/runtime/hub/SDK paths.

### Web acceptance

PR 6275 merged as `4699fbc2f0c1fe367bca673637986213ee31f9aa`. Its integrated source head `cb5bbfa720eedd6c04330b52288cace020212377` passed 4,865 tests in 513 files, TypeScript checks and production build. The existing bundle-size warning was not reported as a new failure.

The isolated fixture installed showcase through the real confined validator, pinned Deno, manager and app connection. It exercised native controls, list/search/filter/More, navigation/edit/save/pop, delayed invalidation, receipt results, reconnect/lost-open recovery and legacy HTTP fallback. Screenshots show narrow/wide layouts and light/dark native rendering. The attached images were inspected, not merely generated.

Cold application offline gating and development StrictMode behavior were not bypassed to fabricate capability negotiation. Browser acceptance does not establish Apple rendering or kernel resource enforcement.

### Android acceptance

PR 6297 merged as `e312a3addfa962ced2661983caeccd3161947614`.

The initially integrated head `f19a56230aacf424884b50111f2077b879ef25cf` passed:

| Suite | Tests | Failures/errors/skips |
| --- | ---: | ---: |
| Contracts | 223 | 0 |
| Hub | 338 | 0 |
| Design system | 143 | 0 |
| Settings | 535 | 0 |
| Total | 1,239 | 0 |

Seven tests used actual source-network fixtures. Debug Kotlin compilation passed. The final manager-routing integration `6d553d831813ad13e0659b60529fac1d9aa4d578` additionally passed all 227 contracts tests, 62 targeted controller/native/manager-routing regressions and Debug compilation. The full initial suite was not mislabeled as rerun on that later head; routing-only integration retained the same drawing.

Acceptance covered real source installation, list/controls/pagination, detail editing/save/pop, asynchronous invalidation, reconnect, lost opens, bounded cleanup, unsupported-profile HTTP fallback and host approval/denial. A native local confirmation is followed by an explicit manager-owned host decision. Hidden, retired, expired or mismatched page permissions cannot authorize a write.

The 12 attached and inspected images cover Glass/Brief/Board in light/dark, including actual host permission dialogs. They are **Robolectric JVM native graphics, not emulator or physical-device acceptance**. System bars, IME and physical-device performance remain unverified.

An initial full-hub run failed because independent resident port discovery consumed requests intended for local mock servers. The failure was retained and attributed with a separate listener. Running JVM-only mocks and their clients together in a private user/network namespace passed all 338 hub tests, without filtering requests, weakening assertions or changing the resident service. Actual database-backed source acceptance ran separately with real connectivity. The global token ratchet retains 13 unchanged pre-existing findings; no exemption or baseline increase was added.

## Complete source-profile evidence

### Showcase

[Source PR 308](https://github.com/nwverse/persona-extensions/pull/308) merged as `dd9401dde8ee5f664bc4de037292a9eae421cfae`, closing source issue 303. The full default Program retains the native navigation page, overview card, six widget families and both shortcuts. A bundle-owned `componentPage` Renderer drives independent page hooks over one shared synthetic store. All 232 strings per locale are preserved in English, Simplified Chinese and Japanese.

Node and Bun each passed 43 tests; both JSX compiler modes and five negative programs passed their expected checks. The officially copied SDK passed 88 tests, 13 negative fixtures and portable Node checks. The original Go suite passed 12 tests with unchanged goldens. Independent current-main verification passed source tests/types, deterministic package rebuild and the full public CI script. The clean publication branch has the exact verified content tree; old drafts were superseded, not force-pushed or merged. Its hosted `check` passed; `release` was skipped.

Actual installed archive SHA-256: `deeef0dd53bea80d129bcae23ab54652007913f5c20d96a6b6783915f9af9072`; bundle: `28d44461dcc8e64981d6ba0e78bf8a6b627b3caa9fa91ff4cf14b15245866e44` (63,679 bytes). Rebuilding on the final source content reproduced both digests. Real acceptance passed the 63 checks described above, including every system contribution and old-client navigation. The actual old production registry rejected install and upgrade of the source archive before version checks, preserving Go records/executable bytes and a subsequent real render.

Offline refers to the contained runtime code and dependency-free activation, not native image availability. Synthetic images use credential-free HTTPS and an optional presentation-media configuration; default assets become available when their repository files merge. One deliberately missing image exercises native failure display. No extension network grant was added for those client-drawn images.

### Hacker News

[Source PR 309](https://github.com/nwverse/persona-extensions/pull/309) merged as `9f8bc30a9e88903233f2ab555b312a4697ef37eb`, closing source issue 304. It contains the complete default Program and shared read-only connector/tool/service cache, native pages and six snapshot families. Node and Bun each passed 33 tests, both JSX compiler/negative modes passed, and the SDK passed 88 tests plus 13 negative fixtures. Fourteen original Go top-level tests, race checks and full public CI passed. Independent integrated verification passed types/tests/package build and full public CI on `12f60d0e12db75ed376d2d4392300ebdadb4e08c`; its source/SDK files are identical to the actually accepted content.

A source-owned cached-resume regression was reproduced: publishing a fresh wrapper for unchanged data changed the renderer version and invalidated a displayed callback. The repair retains equivalent cached results but still publishes changed items, timestamps, missed/error states and new request identities. Bundled Program and actual legacy HTTP regressions verify this distinction.

Actual installed archive SHA-256: `7f9c98ae41b66d47f868a57426a8219b688f6de598e45d8842e5446ddac384ab`; bundle: `1a74579a2738000cd794d2e8e9f193d7d45c996fe6819fc73d67c21982b361e5` (129,695 bytes). The final rebuild reproduced the bundle, but its archive digest differs because tar metadata is not deterministic. Comparing every regular archive member confirms identical file contents; a rebuilt archive is not mislabeled as the originally installed package.

Actual isolated Deno/manager acceptance verified digest-consented preview/install/default Program restart, native callback/delta/full-snapshot/dedup/stale/reconnect, legacy UI versions 1/3/5 full Views/opaque checkpoints/stale rejection/feed/story/back, all system families, connector registration and one shared service cache. Deterministic interaction tests use the **exact Program factory with synthetic read-only fetch data inside the real sandbox**. They are not a claim that the production feed returned all stories.

Separate sanctioned public-broker evidence recorded HTTP 200, a 500-ID positive-integer feed shape and a matching item's field types, without storing real authors/articles/IDs in public evidence. One actual default Program run loaded only two stories; intermittent bounded upstream timeouts are retained. A complete 30-story live feed or continuous upstream availability is **not verified**. Historical hub `717177649d` rendered the working Go 0.1.0 installation, rejected source preview with 400, and retained Go. Production refusal for unavailable controllers and invalid development opt-in was separately checked; owned hubs/databases were cleaned up.

## Actual extension inventory

Inventory counts **distinct real extension IDs**, not every manifest found in cloned repositories. SDK templates, schema examples, test data, build outputs and duplicate worktrees are excluded. One previously classified third repository was an application authoring checkout containing two SDK templates and one schema example, not a third extension market. Those three fixtures are not migrated-package counts. A smaller private repository copy contained IDs already present in the six-package private set.

| Actual set | Total | Executable | Declarative |
| --- | ---: | ---: | ---: |
| Public extensions | 14 | 11 | 3 |
| Private extensions, identities withheld | 6 | 5 | 1 |
| Total | 20 | 16 | 4 |

The two representative source profiles are accepted locally and through the isolated runtime; their released process packages remain unchanged. **14 other executable packages remain to migrate**. Four declarative packages do not require a TypeScript executable rewrite; their manifests, configuration and snapshot compatibility must still be retained. This work does not claim all extensions have been converted or released as source packages.

### Public migration checklist

| Extension | Current released runtime | Next migration work |
| --- | --- | --- |
| showcase | Process, version 0.1.3 unchanged | Complete source profile accepted; source PR 308 merged; release/rollout remains gated |
| hackernews | Process, version 0.1.0 unchanged | Complete connector/tools/service/page source profile accepted; source PR 309 merged; release/rollout remains gated |
| claude-bypass-allow | Process | Preserve host permission behavior and native settings UI |
| github | Process | Preserve connector, scopes and native UI |
| homeassistant | Process | Preserve connector, authorization and native UI |
| larkagentx | Process | Preserve connector/service behavior |
| linuxdo | Process | Preserve connector/parser/cache and native UI |
| markets | Process | Preserve connector/service and native UI |
| sentry | Process | Preserve error-report contribution and bounded delivery |
| telegram | Process | Preserve assistant/channel-facing connector behavior |
| v2ex | Process | Preserve connector/service and native UI |
| mnemo | Declarative | Retain MCP contribution; no executable rewrite |
| starter-kit | Declarative | Retain prompts, skills, themes and triage templates |
| starter-pack | Declarative | Retain package/configuration compatibility |

### Private migration checklist

Private identities, versions, repositories, paths, service names, configuration and data are intentionally absent. Stable labels are local to this record, not published extension IDs.

| Package | Runtime | Generic contribution scope to preserve |
| --- | --- | --- |
| Private extension 1 | Process | Assistant contribution |
| Private extension 2 | Declarative | Assistant configuration; no executable rewrite |
| Private extension 3 | Process | Agent sources, generic API, assistant, connectors, MCP and UI |
| Private extension 4 | Process | Assistant and connectors |
| Private extension 5 | Process | Usage contribution and UI |
| Private extension 6 | Process | Assistant, events and UI |

### Required checks for each remaining executable migration

1. Claim its existing issue before work and keep its released package working until an explicit supported source upgrade is available.
2. Synchronize the SDK with the official generator; preserve the complete default `Program`, namespace handlers, connector/tools/service behavior and all contribution metadata.
3. Build a contained import-free artifact with its verified digest. Keep per-extension authoring dependencies/lockfiles outside repository roots; do not introduce runtime imports, subprocesses or unapproved filesystem/network access.
4. Verify exact grants, credential isolation, registered egress/redirect denial, request/body/deadline bounds, revocation and independent cleanup. Use synthetic fixtures and redact real upstream data.
5. Verify state/effect/fetch/interval behavior, stable native keys, stale/replayed actions, navigation/results, visibility and resource termination. Snapshot-only surfaces must not acquire a page runtime on the client.
6. Install the complete package through the real validator/engine/manager. Test the older-client full-View path and older-hub rejection/upgrade safety; a temporary wrapper or unit test is not installation acceptance.
7. Exercise Web and Android native behavior with actual source and inspect light/dark screenshots. Coordinate Apple separately. Do not treat JVM screenshots as device verification.
8. Record exact build/test versions, commands, results, failed/unexecuted runs and resource policy. Run documentation/privacy and commit-identity checks before public publication. Merge through the privacy hook, without rewriting published history. Both source publications used fresh current-main branches; old drafts 306/307 were closed without merging or force-pushing. This does not claim old public references were deleted.
9. Privileged or node-local process capabilities unsupported by the source runtime must stay on the existing path or receive a separate supported design; do not bypass isolation to complete a port.
10. A release, market update or production deployment needs separate authorization and must wait for gate 5975.

## Outstanding production and platform gates

| Gate | Status | Required follow-up |
| --- | --- | --- |
| Kernel memory/swap/CPU/pids enforcement | **UNAVAILABLE; production blocked** | Complete issue 5975 on a dedicated supported Linux host, with fallback disabled and neighboring extension survival |
| Apple native rendering/session/fallback | Handoff, not accepted here | Issue 6176, implementation and screenshots on the appropriate Apple environment |
| Android emulator/device behavior | Not verified | System bars, IME, lifecycle and physical-device performance |
| Source execution on nodes or non-Linux hosts | Unsupported by this implementation | No unsandboxed or executable-JS fallback; separate platform work required |
| Extension releases/production deployment | Not performed or authorized | Explicit maintainer decision after the production gate |

Development acceptance requires **both** the Linux `persona_dev_resources` build tag and explicit `PERSONA_DEV_SOURCE_RESOURCES=1` for an isolated manager. Normal builds reject opt-in; tagged builds default off. Namespace, artifact, egress, credentials and native write gates are unchanged. CPU rlimits, V8 old-space limits and sampled RSS termination are not hard kernel CPU/memory quotas; measured RSS overshoot is retained in runtime evidence.

Persona repository CI was unavailable/queued during the backend/client stages; local verification, not unexecuted CI, supported their merges. The fresh showcase and Hacker News source PRs each passed their own hosted `check` job; `release` was skipped. Passing local commands are recorded in the individual PRs. Raw logs and machine-specific reproduction data remain private. Attached synthetic client screenshots and concise PR evidence are the public acceptance record.
