# Lark home: pinned chats

Part of phase 1 of #949. Every screenshot comes from an isolated mock (made-up chats and accounts) and contains no real Lark content.

Done on 2026-11-04 on the iPhone 18 Pro / iOS 27 simulator under BeautifulUI, one light and one dark round. Each round passed the 4 `LarkPinnedShelfFlowTests` flows and every screenshot was looked at; the dark round also ran 3 `LarkEntryTests` covering the Settings entry, turning it on/off, jumping into the shared chat, and relaunch. The Xcode 26 device-signed build passed.

| Screenshot | Checks |
|---|---|
| `pins-compact` / `pins-expanded` | One row by default, expanding shows the full set, avatars and unread counts, buttons don't wrap |
| `pins-return-collapsed` / `pins-chat-return` | Scrolling up past the pins collapses them, scrolling right back keeps them collapsed; swipe right to go back from the shared chat |
| `pins-single` / `pins-empty` | A single pin shows no expand control; no pins leaves no blank space |
| `pins-live-reordered` | Socket pushes update order and unread counts, no manual refresh |
| `pins-offline-cache` / `pins-new-account` | Relaunching offline shows the cache first; switching accounts clears the old account |
| `pins-large-*` | Largest accessibility text size, 80 pins, scrolling across several screens; the top entry still works |
| `pins-reader-before` / `pins-reader-after` | After removing a pin and inserting a new chat at the top, the reading row moves by less than 2 pt |

Pulling down again after expanding does not add read requests and does not show "Release to refresh messages". Chat rows are 64 pt tall at the default text size. At the largest accessibility size, existing regular chat rows still truncate the title / preview; that is a limit of the existing list layout. The pinned area and the top toggle button are both reachable this time.

To reproduce (replace the device ID; generate the local config and the Xcode project first):

```sh
MOCK_LARK_PINS=1 PORT=18826 python3 scripts/mock/mock_hub.py
xcrun simctl ui <simulator-id> appearance light
TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:18826 \
TEST_RUNNER_JARVIS_UI_STYLE=beautiful \
TEST_RUNNER_JARVIS_SHOTS=/tmp/pins-light \
xcodebuild -project apple/Persona.xcodeproj -scheme Persona \
  -destination 'platform=iOS Simulator,id=<simulator-id>' \
  -parallel-testing-enabled NO \
  -only-testing:PersonaTests/LarkConversationsTests \
  -only-testing:PersonaTests/LarkPinnedInteractionTests \
  -only-testing:PersonaUITests/LarkPinnedShelfFlowTests test
```

Switch to `appearance dark`, change the screenshot directory, and run another round. Real Lark sending, read receipts and pin editing were not used in these regressions.

The account-bound snapshot needs a new hub; when an old hub returns no account identifier, the app shows an update prompt and writes no unowned data to the cache. Older clients keep using the original API contract. In-chat pins are a separate later migration and are out of scope for this acceptance.
