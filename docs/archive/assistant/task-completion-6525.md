# Task completion reconciliation (#6525)

## Evidence boundary

A reported Android screen showed a completed task above an agent still running a command.
The retained API and independent read-only database observations show an open task, without a completion,
reopen, or review record. The service journal confirms the serving build but does not reconstruct the
earlier client payload. An in-memory agent snapshot can advance the API's task timestamp without changing
the database task row. None of these observations proves an earlier completion or reopen, its actor, or a
deployment at the screenshot time. No particular recent commit is established as the incident's cause.

The fixes address separately reproduced failure mechanisms; they are not a reconstruction of that missing history.
The real task and its sessions were not modified during verification.

## Reproduced failures and changes

- Completion used a decorated session state and could overlook a raw running tool or a missing linked agent.
  Every active agent must now be reachable, present, and settled, including its background work and interactions.
- Transcript reads and completion judgments could outlive a resumed turn. Runtime and work-generation checks
  now reject the old result, including a pending-review write. Committing completion and observing a resumed
  turn are serialized so that either the old judgment is discarded or the new work reopens the task.
- A temporary idle tick could pair old assistant text with a still-open tool exchange. Tool/thinking tails
  cannot serve as final-answer evidence. A subsequent stopped-session update retries that held receipt,
  including idle-to-idle updates without an intervening working tick.
- A reconnect's initial session-list response could overwrite a newer live notification. The response now
  requires both the current connection and an unchanged local session revision.

Android lookup, refresh ordering, and rendered task-state coverage are in PR #6538. This server change does
not alter the node protocol, require node upgrades, or relabel a running task as completed.

## Verification

The previous completion implementation, substituted with a Go overlay while retaining the regression
test scaffolding, fails the resumed-judgment, running-tool, and idle-tool-answer tests. This is a focused
behavior comparison, not a claim that the entire historical release was rebuilt.

Run the affected regressions with:

```sh
cd server
go test -race ./internal/assistant -run 'TestTaskCompletion|TestTaskReceiptDoesNot' -count=1
go test -race ./internal/devices -run TestReconnectSnapshot -count=1
go test ./internal/assistant ./internal/devices -count=1
```

Hosted workflows were all `disabled_manually` during verification, and the PR had no hosted check results.
Portable server CI initially stopped before tests because the Postgres fixture image hit the registry's
anonymous pull limit; the public mirror recovered the fixture. The complete script then passed with real
Postgres 18 (server tests: 517.2 seconds), server and Windows builds/vet, extension checks, and Go SDK tests.
It also exposed two test-harness defects: fleet fixtures used a logger after test cleanup, and a shared
test referenced a Unix-only service-unit helper on Windows. Reusing the lifetime-aware logger and moving
the platform assertion into a constrained test fixed both without changing node runtime code.
New completion and reconnect regressions passed with the race detector, including a final-answer update
overtaking an older transcript read. An absent hosted check is not a passing check.
Deployment acceptance requires the unique release owner
to confirm the merged source SHA and the actually serving version separately from these source tests.
