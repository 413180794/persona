# Main chat rich UI: pre-deployment acceptance

Recorded: 2026-11-06. Parent task #2017; web restore fix #2938 / PR #2945.

This is an acceptance snapshot of a pinned source revision. It is not a live progress table, and not a record of enabling it in production.

## Pinned source and scope

- Official source: `806e84dc518ec000e8b8e1ad68d388b7ed22ee1c`.
- Build version: `v0.1.11-167-g806e84dc5-202611051613`.
- The server binary, the web app and the acceptance config all correspond to this source; the SHA-256 of the binary and of the web tree are kept in private local evidence.
- Reuses the extension View schema, generated types and rendering components; no separate assistant UI protocol was built.
- The real model in this round ran on the Claude Code runtime. This does not support claiming that the SDK runtime, Codex, or real devices on each platform have passed the same acceptance.

## Real model end-to-end loop passed

The test used its own Postgres, HOME and PID namespace, mounting only test data and read-only build artifacts; no production state was mounted. It created a temporary `main: false` chat; all meeting notes and projects were fictional.

The request only described two ways to organize the material and "keep the same interface from choosing to finishing". It gave no schema, tool names or prebuilt View.

Actual results, in 91.1 seconds:

- The model called `view_show` on its own and created a live View at version 1.
- The browser actually clicked a choice the model provided and sent exactly one action request, with `expected_version: 1` and `client_msg_id`, and no reply text forged by the client.
- The hub returned success and persisted the answer; the model updated the same card through `view_get` / `view_update`.
- The DOM node identity stayed the same; the version went from 1 to 2, then reached `progress.ratio == 1` at version 5.
- The answered state still showed after updates and after completion; the action was not sent twice.
- After a page reload, version 5 and the answered state were restored; the old choices were removed or disabled and could not be submitted again.
- A read-only check of the test database found one live View, one action / outbox entry and one delivered reply; the reply appeared only once in the chat, and there was still only one feed reference.
- The model called only the three card tools; the guard recorded one real restricted launch, with no rejected or unknown launches.
- Light and dark screenshots were both actually checked. Screenshots, the raw chat, temporary identifiers, auth config and logs are not committed to the repository.

The result is marked **pre-deployment isolated acceptance PASS**. It is not the same as acceptance after a production upgrade. The test switch was enabled only on the standalone hub.

## Build and test results

| Check on the pinned source | Actual result |
| --- | --- |
| Full regular server test suite, real isolated Postgres | PASS, 78 package entries; some packages have no tests |
| Targeted race regressions for rich UI, permissions, diagnostics | PASS, three rounds each for DB / assistant / node / hub; not a full server race run |
| Full web test suite | PASS, 292 files, 2879 tests |
| Web production build and privacy scan | PASS |
| Unified build of Linux hub, Darwin node cache, Linux WeChat plugin | PASS |
| Schema generator consistency | PASS, 7 tests |
| Doc path check | PASS, 11 tests |
| Full Go SDK suite | FAIL, `wire.TestCheckLooks`; the release as a whole cannot be called all green |

The SDK failure comes from the new looks validation: when the same image is referenced at two sizes, the map keyed by file overwrites one of the size constraints. On the pinned source it failed nine of ten repeated runs. The repro was added to the original lane for #3200; its files were not taken over. Reading files can be deduplicated, but size constraints must be checked per reference. This snapshot does not include the later fix.

## Earlier failures are not rewritten

- An earlier deployment snapshot had `TestRouteScopes` and `TestLauncherRepair` failures, fixed by #3116 and #3130 respectively and then re-verified.
- The first natural request only asked for progress to stay in the same place. The model first output a choice card in the message body and planned to create a progress card after the choice; the driver, however, required the first choice card to already be live and did not click the body card. That round stays FAIL; afterwards the requirement was clarified to one interface for the whole process.
- The next round really created a live View, but `python -I` excluded the already installed Playwright dependency, so the whole round was still FAIL. Resuming was allowed only on a temporary chat at the same version with no action submitted yet.
- The resumed run really submitted one action; then the driver treated `view_actions` as an array and errored. This already answered session was not clicked again.
- After fixing the driver to use the real map, the web app turned out to have the same problem: during updates the event could show the answered state, but on reload the snapshot could not restore it. That full round stays TIMEOUT, and #2945, submitted by the original owner, was reused instead of writing an identical patch.
- The original web app failed in a browser fixture with the real map; after the fix, all 20 positive, negative, rejection and timeout scenarios gave the expected results. Fixture results do not stand in for real model results.
- An extra two-source candidate acceptance also passed, but the server and the web app came from different commits, so it is explicitly marked as a candidate. Only at the end was the pre-deployment acceptance completed with the single official source in this file.

## Production and remaining boundaries

Production had earlier been updated forward to `2586ec90` as authorized. The installed artifacts, the running hub / node, the web app and the rollback backup were all verified; config was unchanged and `persona_view` stayed off. The web restore fix has been merged, but it is not in that earlier production deployment.

The follow-up shared hub update in this round was paused to coordinate the release window, so `806e84dc` was not applied to production again. No test messages were sent to a real main chat, no real main chat was created or compacted, no user data was deleted, and no TestFlight build was released.

Still to be listed separately:

- Fix the SDK looks check gap recorded on the pinned source, and verify the actual fixed source.
- Confirm the shared production release window, then upgrade forward following the actual production ancestor, artifact provenance, config and hash checks, CAS and a rollback-capable process. Do not present an undeployed mainline update or evidence from old source as the new source having passed.
- After the shared release window is confirmed and the upgrade done, run the final isolated acceptance as the original task requires. Enabling `persona_view` in production must separately wait for explicit confirmation from the original user; collaboration messages or test notifications do not count.
- Apple #2024 / #2025 stay in their original lane; this record does not claim they are done.
- The HTML bridge API exists, but that does not mean JS execution safety on Web / Android / WK has been accepted. The web runtime flag is still off, and HTML keeps the text fallback.
- The new shared schema already includes `diff`, `code`, `file_tree` and others, so they can no longer be described as nonexistent. This round did not do per-item real-device acceptance on all four platforms; coverage of the other components is to be checked separately against the actual implementation.
