# Apple extension model settings acceptance (#1861)

Code freeze and local verification completed on 2026-11-05, at commit `214395985`. This record covers only #1861; already-merged #1859 / #1860 are not repeated. System widgets, watch faces, notification button callbacks, and App Intents still belong to #1862.

## Verification results

Used Xcode 27.1; Xcode 26.0 was unavailable locally and was not built against. The minimum compiler policy is unchanged.

| Verification | Result |
| --- | --- |
| iOS `build-for-testing` | Passed |
| Full `PersonaTests` | 1368 tests, 199 suites passed |
| `ExtensionManagementUITests`: glass light | 9/9 passed |
| `ExtensionManagementUITests`: BeautifulUI dark | 9/9 passed |
| Glass dark, BeautifulUI light | 8/8 each passed before adding the expiry case; screenshots actually inspected |
| `PersonaMac`, `PersonaWatch` simulator target builds | Passed; Mac was not launched, did not take focus, and was not screenshotted |
| `scripts/privacy_check.py` | clean |
| `scripts/test/test_doc_paths.py` | 6 tests passed |
| `scripts/test/test_extension_management_mock.py` | 5 tests passed |
| `scripts/gen/extension_schema.py --check` | Passed |

The GitHub Actions runner was disabled during this task, so CI did not run and is not reported as passed. Under the project's environment-failure policy, local verification is the merge gate.

The simulator used `scripts/mock/mock_hub.py` on an independent port with `MOCK_EXTENSIONS=1`, `MOCK_EXTENSION_MANAGEMENT=1`, and `MOCK_EXTENSION_CARDS=1`. Fixtures toggle, install, and uninstall only in memory and record every write request; they did not connect to the production hub. Tests launched with `app.launchPinned()`, explicitly specifying style and light/dark mode, and shut down their own simulator afterward.

## Behaviors covered

- Independent contribution toggles; items with `can_disable: false` cannot be operated, and other configuration drafts are preserved.
- Dependents block the first disable or uninstall attempt; cancellation sends no `cascade`, and execution occurs only after a second explicit confirmation.
- Bundle previews list every member's permissions in full; one installation carries each preview's permission digest. Confirmation is disabled while dependencies remain unresolved.
- The preview remains open until the earliest member expires, then shows an expiry explanation and disables installation, with no install request. Waiting for expiry updates only once, without an empty `TimelineView` taking up a list row.
- If bundle uninstall reports success for only some members, report only the actual success count and retain members not reported as successful.
- Prompts only fill the composer, without sending; replacing a sorting template requires confirmation, then fills only the draft without auto-saving.
- Existing themes remain unchanged until an extension theme is selected; invalid themes are hidden; selection takes effect; disappearance of a remote contribution falls back to the default.
- A remote-theme fallback genuinely rebuilds the page, while preserving unsaved settings drafts and the save button state.
- Old hubs do not synthesize contribution toggles, and declaration-only interfaces make no requests; unit tests cover offline theme caching, retention on network failure, and cross-hub isolation.

## Screenshot evidence

All data is mock, with no production messages, credentials, or tester identities. Original images and the overview were actually inspected; the final originals confirm the blank row in the install preview is gone.

- `docs/screenshots/extension-management-apple-glass-light.png`
- `docs/screenshots/extension-management-apple-beautiful-dark.png`

## Failures and corrections

Failures were not skipped as CI environment problems; they were fixed and rerun:

- The dynamic role-color getter created a new `Color` each time, making color identity unstable: default colors are statically cached, and extension colors are created and reused when composing the style; original color tests remain.
- The fixed validity period in a historical successful-install case had expired: switched to a dynamic validity period, while retaining a separate member-expiry rejection test.
- An initially empty List row did not start template loading: load from the nonempty page and pass the result to the menu.
- Native menus do not retain SwiftUI button identifiers: locate by visible title.
- Reading the accessibility snapshot immediately after typing could omit the last character: wait for complete input to stabilize; explicitly wait for keyboard focus after navigation as well.
- `TimelineView` inside a List still generated a blank row when there was no error: changed to a page-level wait for the earliest expiry and added a real expiry UI test.
- The first Mac build after synchronization still used the old project index, omitting a newly added upstream source file: regenerating the project made Mac / Watch builds pass, without changing that source.
- Privacy hits in upstream fixtures were fixed by #2382, #2537, and #2599 respectively; the Apple branch synced those fixes. The final whole-repository scan was clean, without taking over other platforms' code.

## Boundaries

No iCloud access, no real tokens / keys read or printed, no production configuration changes, no hub deployment, and no TestFlight release. The Mac UI was only built, not screenshotted; these results do not imply that #1862 or the original overall Apple task is complete.
