# Persona Link node update acceptance (#1158)

Date: 2026-11-04. Scope: updating the resident personad over the existing node RPC, covering hub orchestration for both active and passive Link: verification, idle check, atomic replacement, independent rollback, and version confirmation. Design and first-upgrade steps are in [node-update.md](../../operations/node-update.md).

## Verified

| Item | Result |
| --- | --- |
| Full server suite | Ran `go test -count=1 -p 1 ./...` on the hub test machine against real Postgres with an isolated test database: passed. After the final small changes, the updater and both command-entry tests were rerun: passed. |
| Static checks | `go vet ./...` passed. Context cancellation on early-return paths is now complete. |
| Concurrency checks | The hub's update / offline queue / wire tests and the whole `internal/nodeupdate` package passed under `-race`. |
| Web | After merging the latest master, 47 test files and 583 tests passed; the production build passed. |
| Browser | Anonymous HTTP / WS fixture: the list, each update stage, the first-upgrade prompt for old nodes, and failure recovery; light / dark and narrow screenshots actually checked. One click sends exactly one deploy request; no browser errors. |
| Privacy and docs | A temporary config without credentials loaded 20 real private values; the privacy scan passed. The doc path check passed. The temporary config is not committed. |

Updater unit tests cover: corrupted / truncated / wrong-version / wrong platform and architecture, chunk retries and conflicts, transaction expiry, concurrent begin, retry after busy and after cancel, rejecting the same version, no restart before the response is written, timeout recovery, protection against external deploys, symlinks / custom run roots, and preserving arguments / environment / working directory.

An independent review found and fixed a race where the service manager starting a replacement process overlapped with rollback. After the old file is restored, a rollback barrier stays in place: a process that has already loaded the new code must re-exec the old file before opening the service socket, and only the old version clears the barrier when it starts. A deterministic test pauses the process after the new version is loaded and before it enters the update manager, lets the restore happen first, then verifies the same PID execs the old version. The key busy / cancel retry and overlap scenarios passed five times in a row.

## Real Linux user service

In a separate temporary directory with a separate systemd user unit, using the real personad and the resident attach RPC, without touching the existing node service, pairing, or production config:

1. The original node is started by systemd and reports the remote-update capability.
2. A real personad of another version is sent in chunks over RPC; the new version is swapped in and exec'd, reconnects with a matching version, and is confirmed.
3. Then a Go test program is sent: `node-update-check` returns a valid target version / platform, but actually starting the service fails immediately.
4. The node service keeps failing to start while the independent transient systemd recovery service stays alive; after the confirmation timeout the previous version is restored, the original user unit restarts and serves RPC normally.
5. The recovery barrier is cleared by the old version on startup, and the confirm endpoint works again. At the end the dedicated unit was stopped, the temporary directory deleted, and no leftover test unit or temporary root was confirmed.

This round directly verifies the resident node RPC and the systemd lifecycle. Transport for active / passive Link reuses the existing byte stream; its deploy orchestration is covered by the hub RPC fixture and the existing full Link test suite. The real Linux user-service test is not written up as an acceptance of a real public-network Link connection.

## First-round failures and verification boundaries

- Several batches of GitHub Actions jobs stayed queued; the privacy check job never got a runner. CI did not run and is not counted as passed. Under the repository's runner-backlog exception, full local / isolated remote verification was used instead, and the environment problem is recorded in the PR.
- The standard `scripts/test/test-server.sh` failed during Docker setup because the SSH user has no permission on the Docker socket; the tests never started. The same `server_test_env.py` the script uses was run against the already running real Postgres, isolated, without changing Docker permissions or production services. The original script is not reported as passing.
- After adding the new machine capability field, the old wire golden failed. The fixture was updated and its diff reviewed: it only adds the deploy capability and the offline reason. The regression passed.
- The first race build hit the two-minute task limit and was stopped; the task budget was extended afterwards. The original target-version test also depended on an 80 ms transfer time and finished early under race; it now cancels on a commit event and still verifies that the old version cannot confirm success. The race regression passed.
- The first independent smoke-test client did not fully handle large writes and bidirectional requests, so the update acceptance did not complete. After fixing the client, the real user-service verification above was redone.
- The macOS updater and key process fixture tests passed. The existing `CodexRemote*` cases in the full local node package timed out and are not counted as passed. That package passed in the isolated Linux / real Postgres full suite.
- No real Mac LaunchAgent was installed, no first upgrade was run on existing live machines, and no Mac App or TestFlight build was released. This change does not automatically turn old nodes into versions that understand the new protocol; the first bootstrap upgrade must still follow the design doc.
