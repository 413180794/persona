# Keeping drafts after the completion toast (#2124)

## Outcome and evidence

Type unsent text, collapse, let the completion toast appear and auto-retract, expand again: the original draft is still there; drafts for different sessions stay separate and are cleared on close or when switching pairings. Proven by a real reproduction on the old version, a production Compose regression test, the same path on the fixed build in the emulator, and APK / lint / related tests.

## Scope

Only lifts the UI draft to the Host lifecycle and wires it back into the Surface; it does not keep a hidden large window, and does not change the completion trigger, the queue, the 4-second duration or target routing. The original 187 tests and the completion-toast field test still apply; this round retests the changed draft path.

## Stages

- [x] Record the regression and create a separate fix branch.
- [x] Reproduce on the old version, fix / regression-test the production component.
- [x] Native / App tests, APK / lint and real draft verification.
- [ ] PR and 0.2.3 release (external progress in the issue).

## Regression and fix evidence

On 0.2.2 in a real emulator, after typing and collapsing, another session finished, the toast appeared at about 0.195 s and retracted at 4.190 s; on expanding again the draft was empty. The Compose red test on the same path failed its input-field assertion after the toast ended. The fix lifts the draft map into the Host, one per Host, cleared on close; the existing Runtime closes the old Host when switching pairings or turning the switch off. After the fix, 24 native tests passed, the new regression test covers both the main chat draft and an Agent draft, and an independent read-only review passed.

The final APK, emulator acceptance, and merge / release results are in the progress comments on #2124.

The 0.2.3 same-signature upgrade in place keeps the existing pairing and the Always switch. On the same real path the new draft is identical before and after; the toast appeared at about 0.282 s and retracted at 4.145 s; the toast and restored-draft screenshots were checked. App 91, native 24 tests and APK / lint passed; the 3 doc-path checks and the staged privacy scan passed. The data layer was unchanged, so the previous round's 73 passing tests still stand.
