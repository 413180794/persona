# Android floating capsule (#2124)

## Results and evidence

The native Android module provides a capsule that can be toggled across apps, dragged to snap to an edge, and expanded into a card. It reuses existing session sending and state; extensions can contribute declarative content and actions. Acceptance evidence includes cross-app screen recordings on an emulator, light/dark screenshots, relevant unit tests, and build / lint results. Voice and current-screen context are follow-up deliverables under #2124; keep the issue open until they are complete.

## Stages

- [x] Native overlay, three-mode setting disabled by default, permission guidance, remembered position, focus handling, and cleanup on exit.
- [x] App adapters for existing sessions / main chat and multi-hub routing, sending, replies, and navigation to the full chat.
- [x] Declarative capsule content / action slots; extension execution stays on the hub, and native permissions belong to the client.
- [x] Unit / integration tests, APK / lint, cross-app emulator screenshots and recordings in light/dark mode, independent review, and PR.

## Responsibilities and boundaries

The native module owns the window and generic content interface; the app composition root supplies business adapters; settings use the existing settings entry; extensions reuse the existing declarative UI rendering and action protocol. Research, implementation, testing, and review are verified separately. No new WebSocket or separate messaging / authorization model. All changes are in the codex/2124-android-capsule worktree.

## Test design

Cover Off / When tasks exist / Always, foreground/background transitions and unpairing cleanup, missing permissions, position clamping, and rotation; actual send calls and results, cross-hub routing, disconnected state, duplicate operations, and extension card actions. The emulator uses an isolated app ID and mock hub, recording collapse, drag, expansion, input, and dismissal over other apps.


## Verification results (2026-11-05)

- Android: 68 app, 186 settings, 10 native policy, and 9 navigation tests, totaling 273 relevant tests passed; Debug APK and lint passed.
- Go: extension module, capsule rendering and action tests with a real hub / extension process, and SDK tests passed; schema / bundle / document-path consistency and mock HTTP tests passed; web type checking passed.
- Dedicated Android 14 emulator: all passed for the three setting modes, system permission grant, left/right edge snapping and position memory, rotation, hiding in the foreground, recovery after disabling and revoking permission, overlays over regular apps, light/dark mode, actual sending with the keyboard open, and extension card actions.
- The latest APK passed 10 consecutive cold-start / foreground-background cycles, with process and crash-buffer evidence retained for each round; protected system settings pages deliberately hide overlays, and the overlay was verified in the regular Clock app.
- Reproduced and fixed a lifecycle race where registration occurred before attachment, competing Compose and native drag handling, and IME local coordinates being mistaken for full-screen coordinates. See Android lessons.
- Recordings, screenshots, test logs, and the test APK remain in this round's evidence / delivery directory on the development computer; chats and extensions contain only mock data. No vendor-specific background survival or power measurements on real hardware.

## Delivery scope for this round

The basic native capsule, toggle, session entry, and declarative content / action extension slots are complete. Long-press voice, double-tap current-screen context, and completing all approvals in place remain under #2124. The PR uses Refs to keep the issue open.
