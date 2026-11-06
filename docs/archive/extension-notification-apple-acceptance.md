# Apple extension notifications: phase acceptance (#1862, partial delivery)

Date: 2026-11-06. Code frozen at `fe49ab99eb9c0638d0ecf7cced1549934dbbb7aa`.

## Delivered

- The iOS / macOS Notification Service Extension registers dynamic notification categories before delivery; at launch the App keeps the extension categories and merges in the static interactive ones. They are also registered before a foreground notification is shown.
- Tapping a notification opens the full extension page with the original `state`. A foreground button is claimed and run once before the result is drawn, so a change of appearance or a page rebuild does not replay the write. Links, navigation, stale-refresh and result toasts are shared with regular pages.
- A background button runs against the hub the notification belongs to. Text input is sent as `values.text`. Only `toast` is shown, as a local notification; link and navigation results are ignored. When the background deadline ends, the request is cancelled and the delegate callback completes exactly once.
- An unknown, conflicting or disconnected source never falls back to the current hub; unknown buttons, wrong field types and invalid paths send no request. Dedup keys on group, notification id and the optional action id.
- System APIs and UI context moved into Shared, and the Watch target compiles through a compatibility layer. This only prepares the API; it does not mean the system placements are wired up.

## Local verification

Xcode 27.1. Every build and test ran sequentially through `~/.local/bin/xcode-slot`, on a dedicated stock iOS 27.0 simulator.

| Check | Result |
| --- | --- |
| `Persona build-for-testing` | Pass |
| Full `PersonaTests` | 1455 tests in 212 suites, all pass |
| `ExtensionSystemAPITests` / `ExtensionNotificationActionsTests` / `ExtensionNotificationRoutingTests` | 14 targeted tests pass; the final full run includes them too |
| `PersonaWatch build`, watchOS Simulator | Pass; minimum watchOS 10 not raised |
| `PersonaMac build`, generic macOS | Pass; the App was not launched and no focus-stealing screenshots were taken |
| `ExtensionNotificationUITests`, Liquid Glass / light | 3/3 pass |
| Same UI suite, Liquid Glass / dark | 3/3 pass |
| `scripts/test/test_extension_notifications_mock.py` | 4 tests pass |
| `scripts/test/test_doc_paths.py` | 7 tests pass |
| `scripts/privacy_check.py` | clean |
| `scripts/gen/extension_schema.py --check` | Pass |

The UI regressions drive real SpringBoard notifications and cover text reply, foreground handling and tapping the notification itself. The mock checks that an action is sent only once, carries the original state and the typed text, and that background handling opens no page. The merged accessibility element of the foreground result toast really exists and is visible.

Screenshots of the notification buttons, foreground action results and the regular tap page were reviewed in light and dark. Saved evidence for the regular tap page:

- [Liquid Glass / light](../screenshots/extension-notification-apple-glass-light.png)
- [Liquid Glass / dark](../screenshots/extension-notification-apple-glass-dark.png)

Earlier failures were not counted as passes. The verification above was rerun after fixing: an actor passing a non-Sendable SDK object, dependency factory calls, fixture setup timing, locating the TextView in the system reply bar, a missing toast on system pages, and locating the merged accessibility element.

## Re-verification after syncing with master

Code after rebasing onto `8520b954c`: `79a7d4597e73d7074da4b9c6a2385177d4eeebd2`. The only manual conflict was at App launch: kept upstream's `QuickAction.publish()` and merged the notification category registration right next to it; all other commits replayed automatically.

- After the sync, `Persona build-for-testing` passes; the privacy and generated-type consistency checks pass.
- The targeted test rerun queued in `xcode-slot` for 40 minutes, with only "all 2 slots busy, waiting" in the log, and exited 124 on timeout: **the tests never started**. The light UI rerun that depends on it did not run. Neither can be recorded as passing after the sync.
- Full verification of this notification logic still rests on the frozen run above: 1455 unit tests and 3/3 UI regressions in each of light and dark. A passing build was not used in place of unit tests, and the machine-wide slot limit was not rerun or bypassed.
- The PR's CI checks (privacy, scripts, schema, etc.) were actually QUEUED; they did not run or pass. Per the project and user convention, the PR was merged on the completed local verification.

## Not verified / not done

- No Xcode 26.0 environment was available, so 26.0 was not actually compiled; the minimum-compiler policy is unchanged.
- GitHub CI is not counted as a local pass. While the project's self-hosted runner is disabled, merging on local verification follows the convention; the PR records the real CI status separately.
- `simctl push` does not run the NSE. Local notifications share the category registration logic, which proves the system buttons and App callbacks, but not NSE delivery of the first background notification over direct or relay APNs on a real device. That boundary still needs real-device verification.
- Home Screen widgets, Watch complications and App Intents are not wired up yet. Drafts are not part of this commit; #1862 stays open.
- No TestFlight release, no iCloud access, no change to production config, credentials or signing material, no real APNs notification sent.
