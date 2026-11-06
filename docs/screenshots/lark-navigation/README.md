# Lark chat navigation transition (#952)

iPhone 18 Pro / iOS 27.0, synthetic mock data. Every screenshot was reviewed; no real Lark account took part in testing.

The chat page explicitly uses the system navigation bar material `.bar` and keeps it visible, so a push no longer shows the sharp cached image through first and only blurs it after the animation ends. The tab bar reuses the existing `hidesTabBarWhenShown()`, so hiding and restoring follow the navigation transition.

## Animation check

| Scenario | Evidence and result |
| --- | --- |
| Cached group chat, before / after | [Frame-by-frame navigation bar comparison](push-before-after.png): top row is before the change, bottom row is the final fix. Before, the image showed sharply behind the title first; after the fix the background is frosted from the start of the transition. An intermediate approach that only set `.visible` failed this check and was not used. |
| Dark glass, light BeautifulUI | [Two more styles](push-other-styles.png): includes mid-transition frames; each keeps its own theme and no longer goes transparent before blurring. |
| Not cached, network not back yet | [First load](loading-sequence.png): only the synthetic group chat's cache was removed, and the local mock delayed the message request by 8 seconds; from the list, through entering and waiting, to messages appearing, the background stays in place. |
| Swipe-back cancelled, swipe-back completed | [Interactive back](interactive-back.png): a short right swipe stays in the chat; a full right swipe returns to the original message list, and the background transitions with the page. |

## Automated regression

The two `LarkNavigationTests` tests enter from real conversation rows on the Messages page and in the Inbox respectively. They cover the new and old layouts and both group chats and direct messages, checking that a cancelled right swipe stays in the chat and a full right swipe returns to the original entry point; on the old layout they also check tab bar visibility.

- Light glass: 2 tests passed, 8 navigation paths.
- Dark BeautifulUI: 2 tests passed, 8 navigation paths.
- Automated assertions cover the pages and back paths; material continuity is judged from mid-transition frames in the screen recordings.
- The first Inbox test used a DEBUG deep link that replaced the navigation stack, so it could not go back to the Inbox. It now taps a real Inbox row, and both themes pass on rerun. Product routing was not changed for the test.

| Theme | Messages / Inbox screenshots (all after a cancelled right swipe) |
| --- | --- |
| Light glass | [Set 1](light-glass-0.png), [Set 2](light-glass-1.png) |
| Dark BeautifulUI | [Set 1](dark-beautiful-0.png), [Set 2](dark-beautiful-1.png) |

Xcode 26.0 builds for iOS, Watch, extensions and Mac passed; simulator tests used Xcode 27. This change only affects Lark chat navigation; it does not change message content, the input box or the server.
