# Lark attachments regression (#410)

iPhone 18 Pro, iOS 27. Mock hub / simulated sessions only; no messages were sent to real accounts.
Light is Liquid Glass, dark is BeautifulUI. Repeated files with the same name and identical images come from different test rounds, not duplicate echoes of a single send.

| Flow | Light | Dark |
| --- | --- | --- |
| Image preview and removal before sending | [Draft](light/lark-photo-draft.png) | [Draft](dark/lark-photo-draft.png) |
| Full-screen image, pull down to close | [Preview](light/lark-photo-preview.png) | [Preview](dark/lark-photo-preview.png) |
| Image sending | [Sending](light/lark-photo-sending.png) | [Sending](dark/lark-photo-sending.png) |
| Local message replaced once the receipt confirms | [Confirmed](light/lark-photo-sent.png) | [Confirmed](dark/lark-photo-sent.png) |
| File preview, back to the input field after closing | [Preview](light/lark-file-preview.png) | [Preview](dark/lark-file-preview.png) |
| Failed send keeps the file | [Failed](light/lark-file-failed.png) | [Failed](dark/lark-file-failed.png) |
| Leave and re-enter the chat, retry succeeds | [Retry](light/lark-file-retried.png) | [Retry](dark/lark-file-retried.png) |

Automated tests: `LarkAttachmentFlowTests` plus mainline `LarkSendTests`, 10 flows in total, covering the existing text, quote, thread, draft restore, hidden send button on an empty input field, and the send marker next to the bubble. Core data boundaries: `LarkAttachmentTests` and the server's attachment tests.
File preview uses the system Quick Look; the test first waits for the content to finish loading, then asserts the close succeeded before tapping the send button.
For the sending-state regression, start the mock with `MOCK_LARK_SEND_SECONDS=4`: under build load, XCTest's first snapshot can arrive later than the default 2-second receipt and miss the sending state. Pinning the mock delay does not change the app's send behavior or the assertions.

Limits: photos are converted to JPEG (longest edge 2560 px); originals or animated images can be sent as files; at most 4 attachments per batch, files up to 100 MiB.
A failed retry keeps the same client_id and private copy. In this phase, after leaving the chat only content already in the outbox is kept; unsent attachment drafts are kept only on the current page. Text and quote drafts use mainline persistence; cross-page attachment drafts and restore after relaunch come with #713.

2026-11-03: after adapting to the mainline rename, re-verified the attachment flows on the combined branch of #530 + #566 + #713 + #764 and updated the light / dark screenshots on this page. `PersonaTests`: all 672 tests / 116 suites pass; 20 related unit tests on the standalone attachment branch pass. Upstream additions (@ mentions, loading state, read receipts) are kept.

2026-11-03 pre-merge review: the attachment branch synced with mainline `7d9035e9`; 34 related unit tests pass; light Liquid Glass and dark BeautifulUI each pass 3 attachment flows, re-checking the send echo, pull-down to close the preview, back-and-retry after a failure, and the scrolled-to-bottom position. The screenshots are still the existing records of the same flows above; the latest review screenshots are kept in local test artifacts.
