# Message Tweaks: Keep Recalled Messages and Edit History

iPhone 18 Pro / iOS 27, light Glass and dark BeautifulUI. All data is generated with `MOCK_LARK_TRAINER=1`; no real chat screenshots are used.

| Scenario | Light | Dark |
| --- | --- | --- |
| The two display switches | [Settings](light/trainer-settings-on.png) | [Settings](dark/trainer-settings-on.png) |
| On: kept text and recall marker | [Chat](light/trainer-chat-on.png) | [Chat](dark/trainer-chat-on.png) |
| Off: text and quote hidden | [Chat](light/trainer-chat-off.png) | [Chat](dark/trainer-chat-off.png) |
| Version history and the locked public view | [History](light/trainer-edit-history.png) | [History](dark/trainer-edit-history.png) |

`LarkMessageDisplayFlowTests` enters from the Lark settings, turns the switches on and off, and relaunches into a chat. With the history panel left open, it changes the setting from the mock and checks that the content hides immediately. When Keep Recalled Messages changes, it checks both the message text and the quote. The second round added: long-pressing a kept message offers no reply / reaction actions, and a recall event arriving in the current chat hides the text immediately. The final re-check added recalled system messages: they stay a centered notice and never show their original text. Both themes pass in full.

A system recall notice without a sender shows "Message recalled" and does not try to show the original text. The light and dark regression runs cover both named and anonymous system messages.

Account conflicts, read-only connections, switching hubs, offline, and old-cache cleanup are verified by unit / API tests; they are not described as tests written against a real account. Media and the outbox keep using the existing implementation.

After merging the interactive quote card from the main branch, light Glass and dark BeautifulUI passed in full again; the screenshots are updated to the shared `QuoteCard` look. Quotes still jump to the source on tap. With Keep Recalled Messages off, the title still shows the sender and the summary becomes "Message recalled".
