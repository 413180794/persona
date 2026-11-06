# Native encrypted text sending regression (#689)

2026-11-03, iPhone 18 Pro / iOS 27 simulator. All accounts, text, passwords and send results come from a standalone mock hub; no real chat was touched. Each of the 16 screenshots below was checked one by one.

| Scenario | Light | Dark |
| --- | --- | --- |
| Normal message input area | [View](light/native-removedordinary.png) | [View](dark/native-removedordinary.png) |
| Pick a previous public text | [View](light/native-removedcovers.png) | [View](dark/native-removedcovers.png) |
| Unlock on the phone | [View](light/native-removedunlock.png) | [View](dark/native-removedunlock.png) |
| Encrypted message echoed after sending | [View](light/native-removedsent.png) | [View](dark/native-removedsent.png) |
| Retry after failure | [View](light/native-removedretry.png) | [View](dark/native-removedretry.png) |
| Inbox protects the encrypted draft | [View](light/native-removedinbox-protected.png) | [View](dark/native-removedinbox-protected.png) |
| Draft restored on returning to Messages | [View](light/native-removeddraft-restored.png) | [View](dark/native-removeddraft-restored.png) |
| Plain Inbox after clearing | [View](light/native-removedinbox-unchanged.png) | [View](dark/native-removedinbox-unchanged.png) |

The 3 flows in `LarkNativeEncryptionFlowTests` passed once in light and once in dark mode. An earlier run of the same flows also passed, but the visual check then found text showing through the mode bar background; after the fix the flows were rerun in full and the screenshots here updated. A wrong password stays on the unlock page and the right one continues the send. A retry after an explicit rejection reuses the original ciphertext. Switching to the Inbox cannot send the encrypted draft as a normal message; the draft is still there after returning to Messages and after a restart; once the draft is cleared, the plain Inbox no longer shows the new mode bar.

The mode bar takes only one line above the input field; when encryption is selected, the public text shows on the same row. The background separation between the mode bar and the chat content was checked, and no text overlaps with the keyboard up. The ring on the sent bubble is the existing read status, not a loading indicator for resubmitting the message; a UI test asserts that the local outbox's sending marker disappears.

The screenshots do not mean the real Lark gateway / hub is deployed, and they do not cover encrypted attachments, topics or thread input areas.
