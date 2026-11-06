# Android capsule completion hint (#2124)

## Outcome and acceptance evidence

When a session finishes, the small capsule briefly shows "session name · Done" and shrinks back to 64×40dp after about 4 seconds. Tapping the hint opens that session directly, without changing the manual card's session selection or draft. Completion evidence: real state events on the emulator, window screenshots / recordings before and after the 4-second mark, tap routing, unit / UI tests, APK / lint.

## Scope and approach

- The brief single-line hint is a display state separate from the fully expanded card. It does not start chat observation and does not open the keyboard.
- It uses the unified session's online in-turn Working → Unread completion (a NeedsYou in between keeps the running flag). Old unread sessions at startup / reconnect are the baseline; a rev update by itself does not count as a new completion. Dedup is by multi-hub scoped key and completion version. Mute / quiet is filtered at completion time; unmuting does not replay old events.
- While the manual card is expanded or being typed in, the hint does not take over; only the existing unread hint stays. Several qualifying completion hints are shown one at a time. Tapping uses a separate completion target and goes straight to the original session.
- Returning to the App, locking the screen, revoking permission, turning the feature off, or switching pairing clears the current hint and the queue. An old timer does not collapse a newly opened manual card. Docking / rotation and the three fixed entry points are kept.
- Keeps the current same-signature APK release and browser QR-code download. Does not extend voice, screen context, or the #2198 update transport.

## Division of work

Research verified the event / version / multi-hub boundaries. The native implementation owns the notification contract, queueing / timing, window and display. The App implementation owns the actual completion edge, filtering / dedup and explicit targets. Independent acceptance covered emulator screenshots / recordings / tap targets. The main agent did integration, docs, tests / PR and release.

## Stages

- [x] Record, dedup check, claim and plan.
- [x] Completion event adaptation and direct navigation.
- [x] Native brief hint, queue, auto-collapse and isolation from the manual card.
- [x] Tests / emulator / APK / lint.
- [ ] PR and APK release (see the issue comments for progress).

## Pre-merge verification (2026-11-05)

App 91, native 23, data layer 73: 187 related tests pass in total; 2 control tests against a real HTTP / WebSocket mock pass. APK / lint, doc paths and privacy check pass. Independent review found that passing through NeedsYou would miss the hint; the turn's running flag is now kept, with 3 regression tests and a real transition check added.

Dedicated Android 14 emulator: old unread at startup does not pop. Light mode shows in about 0.358 s and collapses at 4.196 s; dark mode shows in about 0.245 s and collapses at 4.195 s. The final Working → NeedsYou → Unread run measured about 0.362 s to show and 4.108 s to collapse. The hint is actually 216×40dp; the small capsule is 64×40dp. With Main / Inbox selected, tapping opens the exact completed session in both cases. FIFO order and dedup, input draft protection, and no replay after revocation cleanup all pass. Kept a 23-second main recording, a 16-second FIFO / tap auxiliary recording and key screenshots. Mock data only.

The baseline had no public fresh-list API, so a separate read-only VerifiedSessionsRepository synchronously projects the existing authority, instead of guessing from stale on-disk Working. No new HTTP or WebSocket was added. Completions inside an unobservable reconnect window are not guessed and replayed.

This record covers up to before the PR merge; for the PR and 0.2.2 APK release status, see the #2124 progress comments. Voice, screen context and the #2198 update path are out of this round.
