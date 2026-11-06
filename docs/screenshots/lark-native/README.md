# Home messages toggle

iPhone 18 Pro / iOS 27; all data comes from a mock hub. Turning on **Show Messages on Home** in the Lark settings adds a **Messages** page to the home screen; turning it off restores the original two pages.

| Page | Light, Liquid Glass | Dark, BeautifulUI |
| --- | --- | --- |
| Lark settings toggle | [Screenshot](light/native-feishu-settings.png) | [Screenshot](dark/native-feishu-settings.png) |
| Home message list | [Screenshot](light/native-feishu-list.png) | [Screenshot](dark/native-feishu-list.png) |
| Shared chat page | [Screenshot](light/native-feishu-chat.png) | [Screenshot](dark/native-feishu-chat.png) |
| Home after turning it off | [Screenshot](light/native-feishu-disabled.png) | [Screenshot](dark/native-feishu-disabled.png) |

`LarkEntryTests` checks that the toggle persists, that swiping right in a chat goes back, that horizontal swipes move between adjacent pages, and that settings stay usable after the current Messages page is turned off; `LarkMessagesShellTests` checks the paging gesture boundaries; caching and paging are covered by `LarkConversationsTests`.

Re-tested on 2026-11-03 after adapting to the Persona directories on master; the screenshots come from a combined branch of #530 + #566 + #713 + #764. Added a check for swiping right from the middle of a message row back to Agents, then left back to Messages; the pending badge in the three-page navigation moved above the tab label, clear of the text and the neighboring page edge. On the combined branch, 672 unit tests / 116 suites passed, plus 16 targeted tests for conversation paging, reconnect cancellation and the shell.
