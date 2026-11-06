# Lark draft and outbox recovery regression (#688)

2026-11-03, iPhone 18 Pro / iOS 27. Ran the nine mock flows below in both light and dark mode and checked screenshots of the key states. A real account was used read-only to check the API shapes; no messages or read receipts were sent. Consecutive cases share mock history, so some screenshots include files and images sent by earlier cases; the ring next to a bubble is the main-thread read marker.

| State | Light | Dark |
| --- | --- | --- |
| Saved in the background, text and photo restored after the App is killed | [Draft](light/lark-draft-restored.png) | [Draft](dark/lark-draft-restored.png) |
| A failed file can still be retried explicitly after relaunch | [Failed](light/lark-failure-restored.png) | [Failed](dark/lark-failure-restored.png) |
| Lost receipt: only check the result, no re-upload | [Menu](light/lark-file-check-only.png) | [Menu](dark/lark-file-check-only.png) |
| Successful file without a message ID: only removing the local record is allowed | [Menu](light/lark-idless-file-receipt.png) | [Menu](dark/lark-idless-file-receipt.png) |
| Local draft cannot be read | [Entry](light/lark-draft-unreadable.png) | [Entry](dark/lark-draft-unreadable.png) |
| Explain what gets deleted before clearing the local draft | [Confirm](light/lark-draft-recovery-confirm.png) | [Confirm](dark/lark-draft-recovery-confirm.png) |
| Editable again after confirming the clear | [Edit](light/lark-draft-recovered.png) | [Edit](dark/lark-draft-recovered.png) |
| Legacy draft: confirm which Lark account it belongs to first | [Confirm](light/lark-legacy-draft-confirm.png) | [Confirm](dark/lark-legacy-draft-confirm.png) |
| Legacy draft restored, then relaunched: text and file still kept | [Restored](light/lark-legacy-draft-restored.png) | [Restored](dark/lark-legacy-draft-restored.png) |

The nine `LarkAttachmentFlowTests` flows: image preview / pull-down dismiss / remove; image echoed once; file explicitly rejected, then edited and retried; photo draft saved in the background and restored after relaunch; failed file restored after relaunch and retried; check after a lost receipt and verification after relaunch; successful file without an ID has its local record removed; corrupted draft cleared after confirmation and edited again; legacy draft migrated after confirming ownership and restored after relaunch. Recovery cases drop the DEBUG injection arguments on relaunch. The photo case asserts the complete input before leaving, so unfinished keyboard input is not mistaken for the stored result.

48 storage, attachment and queue tests (3 suites) and 5 mock tests pass. Coverage: isolation by hub group and by actual Lark account; route / token updates keep the draft; account check before sending; switching accounts blocks the POST; draft writes coalesced within one second; immediate save on leave and on send; a late delayed write does not restore an old draft; unpairing cancels in-flight requests and clears local records; a late identity response does not restore the binding; other hubs' data is kept; legacy media migrated after confirmation; successful files without an ID can be removed; plus atomic transactions, missing media, corrupted records and uncertain send results.

Xcode 26.0 builds for iOS (including Watch and extensions) and macOS pass; privacy scan and diff check pass. The GitHub Actions privacy and workflow checks run again too, and the runtime code commit 628e6d87 passes, so the earlier unassigned runner is no longer used as the basis for this round's verification.

Drafts are stored per hub group, actual Lark userId and conversation, independent of address or token. Legacy records have no account identity; only legacy records and legacy text drafts that can be located by the current / fallback address get a confirm-to-restore entry. The account behind every historical credential is not guessed automatically. Editing continues during send, retry and refresh; the input box only locks briefly while the local transaction commits.

File upload has no upstream idempotency protocol. Uncertain results may not be re-uploaded. A successful record without a message ID is not echoed by guessing from a same-named file; it may coexist with the server message for a while and the local record can be removed manually. Recovering a corrupted draft means clearing it locally after explicit confirmation; it is not a backup or a remote recall. Removing a hub invalidates the queue and then clears local data; a plain file-system delete can still fail on a system I/O error, so absolute cleanup in a faulty environment is not promised.

This stage reuses the existing read-only accounts API and needs no new server deployment. Encrypted sending, thread attachments and the pinned list are out of this PR's scope.
