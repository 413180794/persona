# hub contract check

Scope: `server/`, covering HUB-S03, N39, N13, 12.1 and 13.8; checked on 2026-10-31.

| Item | Result |
| --- | --- |
| Lark replies and idempotency (N39) | Plain sends already accepted `client_id`; this change adds accepting and passing it through for Inbox replies. Both entry points reuse `client_ops` to store the receipt. A retry with the same ID returns the original state and body without calling upstream again; a different payload or target returns 409. Old clients that don't send the field keep the old behavior. |
| Mail detail (12.1) | Already met. `GET /api/v1/mail/messages/{aid}/{uid}?mailbox=INBOX` returns `message` with sender and recipients, time, `text`, `html`, `attachments`, `seen`. Attachments carry `filename`, `mime`, `size`. The GET does not mark the message read; once the page opens, the client calls `/flags` on the same path with `mailbox` and `seen:true`. Attachment download is outside the current contract. |
| Device notification preferences (13.8) | `GET/PUT /api/v1/devices/{id}/prefs` already exists. It supports partial updates of `input` and `done:off\|long\|always`, and returns the hub's delay and long-task settings. APNs registration is not required first. This change fixes in-memory state being modified when the database write fails. Android notifications should consume the `notify` event gated on `c.has("ws_notify")` rather than building notifications from raw interactions and bypassing the preferences. |
| Per-device tool registration (N13) | Already met. Migration 0091 stores tools keyed by `(device_id,name)`; replacing or clearing one device's tools does not delete another's. When old iOS clients don't send the new fields, the authenticated device ID is used; Android can send its own ID and platform. Tools are merged and de-duplicated; a call only picks an online device that provides the tool, preferring physical devices and the most recently active connection. |
| Session list actions (HUB-S03) | Complete: `GET/PUT /api/v1/pins`, `GET/PUT /api/v1/mutes`; `POST /api/v1/sessions/{mid}/{sid}/close` and `/rename`. Pins and mutes are keyed by `machine/session`; rename accepts `tab_name` or `title`. Spaces have their own `/api/v1/machines/{mid}/spaces/{wid}/rename`. |

This change only touches the hub implementation, related tests, `docs/protocol.md` and this report; iOS is unchanged. New request fields are optional, HTTP response shapes are unchanged, and there are no new WS events. Existing database tables are reused, there is no new migration, and 0131 is not taken.

Implementation files are `server/internal/hub/{reply_receipts,clientops,lark,inbox,inbox_triage,push}.go`; tests are `reply_receipts_test.go` and `lark_test.go` in the same directory.

Verification passed:

- `go build -buildvcs=false ./...` in `server/`. The sandbox cannot read the VCS state used by the build, so VCS stamping is turned off.
- Targeted hub tests: `TestReplyReceiptsPersist`, `TestLarkSendClientID`, `TestDevicePrefsStorageFailure`, `TestDevicePrefsAPI`, `TestPhoneToolsDeviceRouting`, `TestClientOps`, `TestWSNotifyDone`, `TestWSNotifyInteractionAndAssistant`.
- Targeted mail tests: `TestHTTPMessagesShape`, `TestHTTPFlagsAndErrors`, `TestGetMessage`.
- Targeted db tests: `TestClientOps`, `TestDevicePrefs`, `TestPhoneToolsDevicePersistence`.
- All tests ran with `-buildvcs=false -count=1 -p 1`; database tests used the dedicated database provided by the environment and were not skipped. `git diff --check` passed.

Executions with an unknown outcome are never resent automatically: a stored upstream error is returned as is; an execution left over from a restart with no result returns `202 state=unknown`, and the client has to reconcile it. The production hub was not contacted; no end-to-end testing with the phone and no performance measurements were done.
