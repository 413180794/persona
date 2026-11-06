# Android compact capsule and three entries (#2124)

## User outcome and evidence

The oversized activity summary shrinks into a fixed small capsule; expanding it shows only three fixed entries: Unread / Main chat / Inbox. The unintuitive priority-state count / active-total score is removed. Evidence: on a dedicated emulator over an ordinary App, the collapsed size, the three entries with their real data / routes, sending to the main chat, light and dark and keyboard screenshots, plus the related tests and APK / lint.

## Scope

- Collapsed is about 64 × 40 dp, showing only the Persona avatar and a small alert badge; expanded is about 300 dp wide and at most 420 dp tall, still constrained by the keyboard.
- Unread reuses AgentState.Unread and the seen-version projection; list previews and tapping a session keep the existing send / full-page entry.
- Main chat always sends to the Persona main session and does not follow an urgent Agent.
- Inbox reuses the main App's action grouping, including items and interactions; items belong to the currently selected hub group and open the real detail through existing routes.
- Mute / quiet constraints on the global overlay alerts and on auto-appearing stay in effect; Unread state and seen logic follow Agents, and the capsule list still excludes muted / quiet sessions; Inbox follows the main App's action rows. The collapsed badge is only an alert; the exact numbers are labeled in the three entries.
- The native module receives business projections as generic section / entry data, keeping content and action extension slots; voice / screen context and the update path (#2198) are out of scope for this round.

## Roles

The researcher verifies Unread / Inbox / multi-hub routing. Native implementation owns the generic sections contract, the compact window and the UI; App implementation owns the three entries' real data / behavior / regressions; independent acceptance owns the emulator and real HTTP mock sending / entry navigation. The lead agent does integration, docs, checks, PR / release.

## Stages

- [x] Record feedback, dedupe, claim, confirm data and boundaries.
- [x] Generic native UI and window sizes.
- [x] Data, counts and routes for the three entries.
- [x] Tests / lint / emulator screenshots and review.
- [ ] Merge the PR, update the package and the download entry.

## Pre-merge verification record (2026-11-05)

- App 76, Settings 189, Navigation 9, native policy / Compose 14: 288 related tests passed in total; APK and lint passed. Independent source review, privacy scan and doc-path check passed.
- Dedicated Android 14 emulator: collapsed measures 168 × 105 px (density 420, 64 × 40 dp); expanded about 300 × 420 dp, with the input field and send button above the keyboard in both light and dark.
- Three real mock HTTP sends all reached the Persona main session and were not hijacked by an Agent needing attention; the Unread preview kept its count, which went from 2 to 1 after opening the full session. Inbox item and interaction navigation, the final same-session request description, plugin slots, docking left and right, rotation and the three-level switch all passed.
- A 48-second recording, key screenshots, mock send targets and the acceptance JSON are kept as delivery evidence on the dev machine; only fake data was used. During the final extra screenshots the emulator host exited on a graphics error; after reinstalling, verification passed again; the host event was not counted as an App crash.
- The design-token baseline check reports the same set of pre-existing issues on the original baseline d5493d99 and on this branch; rerunning after extracting the unchanged baseline gave an identical JSON report. This round added no hard-coded values.
- Repository CI was disabled manually, so there are no cloud CI results. The above is local and emulator verification.

This record covers up to just before PR #2229 merged; for merge and package release status see the progress comments on #2124. The 0.2.1 upgrade-in-place package is built; the download restriction for old HTTP pairings is still tracked in #2198, and this round keeps offering the browser QR-code download.
