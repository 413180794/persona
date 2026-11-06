# Pinned messages inside a chat

Validation uses an isolated mock with `MOCK_LARK_CHAT_PINS=1` and `LarkChatPinFlowTests`. It does not read real-account pins, send real messages, or modify real pins.

Scenarios: no pin prefetch when opening a chat; list pagination, encrypted bodies and recalled-message placeholders, permission-based hiding; preserving chat position on close; locating regular messages and thread replies; empty lists, offline retention, old hubs, old Lark gateways, account switching; pagination-failure retry and locate failures; add/remove confirmation, checking only the list after an unknown write receipt without resending; maximum accessibility text size.

2026-11-04: iPhone 18 Pro / iOS 27 simulator, default glass style, all 5 UI tests run in full in both light and dark mode; all passed. All 16 screenshots in each run were inspected individually; the table below retains the main views. Duplicate empty-placeholder text and avatar-text overflow at maximum accessibility text size were fixed.

| Scenario | Light | Dark |
| --- | --- | --- |
| List, encrypted summaries, recalled-message placeholders, and permissions | [Screenshot](light-list.png) | [Screenshot](dark-list.png) |
| Locate a thread reply | [Screenshot](light-thread-target.png) | [Screenshot](dark-thread-target.png) |
| Add confirmation | [Screenshot](light-add-confirm.png) | [Screenshot](dark-add-confirm.png) |
| Unknown write result, no automatic resend | [Screenshot](light-lost-receipt.png) | [Screenshot](dark-lost-receipt.png) |
| Hub missing the API | [Screenshot](light-upgrade-hub.png) | [Screenshot](dark-upgrade-hub.png) |
| Lark gateway missing the API | [Screenshot](light-upgrade-gateway.png) | [Screenshot](dark-upgrade-gateway.png) |
| Maximum accessibility text size | [Screenshot](light-large.png) | [Screenshot](dark-large.png) |

The same round also verified empty lists, remove confirmation, locating regular chat messages, unchanged scroll position after closing, automatic pagination, pagination-failure retry, retaining the old list offline, clearing on account switch, and retaining the current chat after a locate failure.

Model tests cover pagination deduplication, cursor loops, atomic replacement of complete pages, merging live refreshes, account isolation, late results after closing the page, no retry after an unknown write receipt, and redaction of OLK body encodings. Server tests cover role permissions, permission rechecks before writes, pin/message/chat associations, account switching, no-store, regular/thread location, and the distinction between missing resources and old APIs.

To reproduce: start `scripts/mock/mock_hub.py` with `MOCK_LARK_CHAT_PINS=1`, point `TEST_RUNNER_PERSONA_MOCK_HUB` to that isolated instance, then run `PersonaUITests/LarkChatPinFlowTests`. Set `TEST_RUNNER_PERSONA_SHOTS` to choose a screenshot directory; run separately with `simctl ui <simulator> appearance light|dark`.

Reading chat pins through the Lark gateway clears that chat's unread-pin marker, so this API was not smoke-tested with a real account. No real messages, pin writes, or read-status actions were performed. Everything in this directory is synthetic data, with no real chat content.
