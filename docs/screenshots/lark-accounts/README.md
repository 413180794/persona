# Multiple Lark accounts

Refs #1321 / #1399. Every screenshot comes from an isolated mock (`MOCK_LARK_ACCOUNTS=1`, made-up accounts and chats) and contains no real Lark content.

On 2026-11-04, on an iPhone 18 Pro / iOS 27.2 simulator, four rounds: light / dark × default (Liquid Glass) / BeautifulUI. In every round both flows in `LarkAccountsFlowTests` passed, and each screenshot was checked one by one.

The mock account names are Chinese fixture data and appear as-is in the screenshots: 我 ("Me", the default account) and 工作号 ("Work account").

| Screenshot | What to check |
|---|---|
| `accounts-own` | The Messages page has a row of account pills at the top (each with its unread count). With the default account 我 selected, it lists that account's chats; signed-out accounts do not appear |
| `accounts-second` | Switched to 工作号: only that account's own chats and pinned items are listed; no chats from the default account mixed in |
| `accounts-second-chat` | A 工作号 chat opened: messages are read as 工作号, and the subtitle 「飞书 · 工作号」 ("Lark · Work account") shows which identity sends |
| `accounts-settings` | **Accounts** under Settings › Connectors › Lark: the default-account marker, unread counts, signed-out accounts in red |

To reproduce (substitute your device ID; generate the local config and the Xcode project first):

```sh
MOCK_LARK_ACCOUNTS=1 PORT=<port> python3 scripts/mock/mock_hub.py
xcrun simctl ui <simulator-id> appearance light      # or dark
TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:<port> \
TEST_RUNNER_JARVIS_SHOTS=/tmp/lark-accounts \
TEST_RUNNER_JARVIS_UI_STYLE=beautiful \               # drop this line for the default style
xcodebuild test -project apple/Persona.xcodeproj -scheme Persona \
  -destination 'platform=iOS Simulator,id=<simulator-id>' \
  -only-testing:PersonaUITests/LarkAccountsFlowTests
```

## Web

`web/` connected to the `MOCK_LARK_ACCOUNTS=1` mock, shot with `scripts/test/web_lark_accounts_e2e.py --shots` (Playwright Chromium, 1280 wide in light / dark, 390 narrow); each screenshot checked one by one.

| Screenshot (`web/`) | What to check |
|---|---|
| `messages-second-*` | The account buttons under the search box; after switching to 工作号, only its chats and pinned items are listed |
| `messages-second-chat-*` | A 工作号 chat is read with `ol_account=2`; the chat header reads 「群聊 · 工作号」 ("Group chat · Work account") |
| `inbox-second-*` | A 工作号 item in the Inbox reads 「飞书 · 工作号 · 群聊」 ("Lark · Work account · Group chat"); opening it opens the 工作号 chat |
| `settings-accounts-*` | Accounts under Settings › Connectors › Lark: default, unread counts, signed out |
| `messages-narrow-light` | Narrow screen: the account buttons sit above the list |
