# Apple rich UI wrap-up: empty states and screenshots (2026-11-06)

Related to #2024. This record covers only this round's empty-state fix, shared-sample screenshots, and Mac row-height fix. It does not replace end-to-end acceptance with a real model / production backend.

## Fixes

- Empty states / skeletons check the composed chat rows, transcript, pending / arriving messages, and streaming body together. A chat containing only a live card no longer shows "No messages yet"; a truly empty session keeps the original prompt.
- Removed the page-level `.pluginLinks` wrapper from Mac assistant card rows so `GeometryReader` no longer compresses lazy rows. HTTPS links still go through the parent `ChatView`'s full-page link panel. The original iOS rendering is unchanged; no hardcoded card height was added.
- HTML fallback now explicitly provides accessibility labels and IDs. Mac still shows only a summary; execution remains disabled.
- DEBUG batch screenshots launch the real Mac App once, with an independent bundle ID, temporary HOME, loopback fixture, and a policy that prevents activation. Capture follows feed loading, body-marker checks, and stable-layout checks. The App is not brought forward, the mouse is not operated, and the App is not restarted for each screenshot.

## Completed iOS matrix

The six shared samples are a decision, comparison table, progress, form, chart, and HTML. Each cell has six screenshots, for 60 total.

| Style | Light | Dark |
| --- | --- | --- |
| glass | Passed and images inspected | Passed and images inspected |
| clay | Passed and images inspected | Passed and images inspected |
| island | Passed and images inspected | Passed and images inspected |
| lark | Passed and images inspected | Passed and images inspected |
| beautiful | Passed and images inspected | Passed and images inspected |

All screenshots came from the real App's `AssistantRichCardTests/testAllSharedSamplesAreVisibleAndCaptured`. Actual web buttons, including HTML buttons, entered an unobscured area; the test did not just wait for the native container. Every image's content was inspected, with no blank pages or incorrect empty states above live cards. The initially missing cell timed out while waiting for a build slot; its tests never started. Only the missing cell was rerun, and the original timeout record was retained.

The final glass / light regression had 35 tests, 0 failures, and 0 skips. It covered a truly empty session, the six shared samples, and existing native confirmation / multi-device selection / version-conflict / restored submitted values / duplicate-reply-lock regressions. Xcode 27.1, stock iOS 26.5 simulator; 26.0 was unavailable and was not actually built.

## Completed Mac screenshots

One real `PersonaMac` instance captured the six shared samples × light and dark, for 12 screenshots total. The originals are 2560 × 1800; component-test windows were not substituted for the real App. The body regions of all six light / six dark images in `mac-shots-v6` were inspected. Form fields, submission, comparison tables, progress rows, charts, and HTML summaries are fully visible. The activation-prevention policy stayed unchanged, the native window never became key, and the real keychain / pairing was not modified.

An earlier batch showed only titles, with bodies behind the input area; that does not count as a pass. Full bodies became visible only after removing the inline page-level geometry wrapper. Readiness checks also failed when an AX protocol cast could not obtain a frame, progress labels used combined text, and HTML summaries lacked explicit AX labels. Failed logs and old images were retained. `cacheDisplay` still has known limitations for glass materials and background system-control colors; layout screenshots cannot be treated as acceptance of on-screen materials / active-window colors.

## Evidence and boundaries

The valid iOS cell list, xcresult, attachments, source fingerprints, and failure records are in this task's `/tmp/richui-final-2024-evidence`; the completion list is `completed-iOS-matrix.json`. Mac v6 full-App originals and `captures.json` are retained separately. Actual results for the later mainline integration candidate are appended at the end of this record; old-source evidence is not rewritten as a pass for new source.

The normal Mac safety matrix was merged separately through PR #4367; see [Apple HTML normal Mac matrix](apple-html-mac-normal-20261106.md). The Mac execution gate remains false. Infinite loops / real OS termination still require an isolated environment that can be destroyed externally; there is no guarantee of hard resource quotas or immediate memory reclamation.

No real main-chat writes, production-switch changes, deployment, or TestFlight release took place. Other interaction semantics under #4031 remain with their original lane; this work does not take them over.

## Latest integration-candidate revalidation

Candidate `4fe8f5d50ad9afebbc331dbea678b38b1a81b4c5` incorporated the latest mainline at the time. Conflicts retained only mainline's `String(localized:)` empty-state text and this round's pure computed check; both sets of lessons entries were preserved.

- `xcode-slot xcodebuild … -scheme Persona … build-for-testing` exited 0. The subsequent `test-without-building` regression summary was 35 tests, 0 failures, and 0 skips (including one case with two parameter sets). The final six shared-sample screenshots were inspected again, with no blanks or incorrect empty states.
- `xcode-slot xcodebuild … -scheme PersonaMac … build` exited 0. The same real App captured another 12 full-window light and dark images. Every body region was inspected again; forms, comparison tables, progress, charts, and HTML fallback are fully visible.
- `xcode-slot xcodebuild … -scheme PersonaWatch … build` exited 0. There were no Watch UI changes in this round. Previously completed progress / HTML plain-text fallback screenshots refer to the earlier-stage record; no new screenshots are claimed.
- Final evidence: `merged-final/glass-light.xcresult`, `merged-final/glass-light-summary.json`, `mac-shots-final/captures.json` and original images, and `merged-final-*-build.log`, all in the task evidence directory above.

All builds ran serially through the machine-wide slot. Xcode 27.1; 26.0 was not actually built. CI that did not run is not reported as passing. Privacy scanning and document-path tests were checked again before the PR submission.
