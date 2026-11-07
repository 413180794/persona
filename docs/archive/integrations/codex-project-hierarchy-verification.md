# Codex project and task hierarchy verification

Issue: #5620. Source: `401fe91eb1dc0d7be55c90335393abc7a0b37349`.

## Changes

Resolve Desktop projects on their source machine using full canonical paths and Git common directories. Cache both successful and fallback resolutions briefly. App project identifiers and directory basenames are not repository identities.

Preserve explicit parent and shared-root metadata for historical and live Codex threads. Public session IDs isolate Codex homes; machine identity isolates computers. Carry metadata through node change detection, manager decoration and HTTP/WebSocket APIs. Missing parents remain visible; cycles terminate.

Android groups live and historical Desktop tasks together in the All range. Children are collapsed initially and can be expanded recursively. Search retains ancestors; status filters use the root task status. Children, including orphans, do not contribute to status or machine counts. Ordinary non-Codex sessions retain their behavior. Server Fleet aggregation also excludes children without dropping them from session APIs.

## Verification

- Node Codex, send and list-key regression tests: passed, including real Git worktrees, same-name directories, aliases, duplicate App project IDs, historical multi-level ancestry, absent parents, source isolation and cache expiry.
- Manager Fleet and task-start regression tests: passed without overlays. An existing test assigning to a constant was corrected to use the existing per-service retry override.
- Hub session API and protocol tests: passed, including metadata-only deltas and historical children.
- Android domain and Agents module tests: passed. Compose/Robolectric clicks verify expansion and collapse in all three styles and both themes; generated screenshots were visually inspected.

## Deployment and limitations

Update the node on computers that discover Codex threads, the manager responsible for those computers, and the hub binary that relays the new fields, as well as Android. No database migration is required.

Actual Desktop inventory and physical-phone behavior remain for user acceptance. A deleted worktree without surviving Git or Desktop project metadata cannot be reliably mapped; keep its original directory rather than guessing by name. No phone installation or production-service update was performed.

Future guidance improvement: document explicit source-scoped parent IDs and top-level task count semantics in the shared Agents contract; no global rules or skills were changed.
