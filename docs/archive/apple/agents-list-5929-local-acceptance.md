# Bounded Agents list API: local acceptance (#5929)

This is a one-off local verification record, not the evolving API contract. The contract is in [protocol.md](../../protocol/protocol.md#bounded-agents-lists-sessions_list-5929). Production deployment is a separate user-approved step and has **not** been performed by this work.

## Scope and existing callers

Runtime Claude Code/Codex sessions use `GET /api/v1/sessions`; extension personal-agent conversations use `GET /api/v1/agents/sessions`; `GET /api/v1/agents` is source/setup status.

| Client | Runtime collection | Personal-agent collection |
| --- | --- | --- |
| iOS | `apple/Shared/APIClient.swift`, `sessions()`: unfiltered complete REST list, decoded off-main; `apple/Persona/App/AppModel.swift` refreshes each hub link. `apple/Shared/LiveConnection.swift` requests `sessions_delta`, receiving a whole initial per-machine snapshot, then changed whole rows. | `apple/Persona/Views/Agents/PersonalAgentModels.swift`: unfiltered status/list GETs; `PersonalAgentListView.swift` loads on mount, manual refresh and `PersonalAgentSync` changes. |
| Web | `web/src/lib/store.ts`, `refresh()`: unfiltered complete REST list on non-resumed connection and actions. Requests `sessions_delta`; keeps Codex history from REST while applying live socket changes. | `web/src/lib/agents.ts`, `refreshAgentSessions()`: status/list concurrently, coalesced refreshes on mount/hello/`agents` events; list still refetches all rows despite event session ids. |
| Android | `HubChatTransport.kt`, `sessions()`: unfiltered complete REST list on non-resumed connection/manual refresh and action recovery. `HubSessionsRepository.kt` consumes whole machine lists; this implementation does not negotiate/consume `sessions_delta`. Its five-second Agents loop checks host status, not session REST lists. | No production caller of the two personal-agent list endpoints was found. `/agents/triage` settings are a different API. |

The new request parameters and socket feature are opt-in. No client implementation was changed. Counts describe complete filtered membership rather than the rows loaded so far. Summary-aware clients must use scalar `waiting`/running-task counts and must not overwrite full chat detail with a row projection.

## Reproduction

Configure a real isolated Postgres test database using `PERSONA_TEST_DATABASE_URL`, as in the server test setup, then run from `server/`:

```sh
go test -p 1 -timeout 8m ./internal/hub ./internal/extmgr ./internal/agents ./internal/sessionlist ./internal/i18n ./internal/proto -count=1
go test -race -p 1 -timeout 5m ./internal/hub ./internal/extmgr ./internal/agents ./internal/sessionlist -run 'TestRuntimeList|TestPersonalAgentPaged|TestPersonalAgentListErrors|TestPaged|TestConcurrent|TestPayload|TestFrozen|TestDelta' -count=1
go test -p 1 ./internal/hub ./internal/extmgr -run 'TestAgents8000Measurement|TestRuntimeList8000OverHTTPAndSocket|TestPersonalAgentPagedRoutes8000' -count=1 -v
go vet ./internal/sessionlist ./internal/agents ./internal/hub ./internal/extmgr
go build ./cmd/personad
```

The baseline was commit `33f668ebcb`, with the same synthetic measurement test copied into a separate baseline branch. All benchmark data is invented: 8,000 rows, four machines, 20 projects, 10 spaces, four states, 600-character previews and background-task metadata. Responses are uncompressed; the measurement times the real HTTP handler for 100 requests, not internet transport. Socket/HTTP correctness also runs over a real local TCP server and WebSocket with authentication, without a real node.

## Baseline readings

| Request | Mean | JSON bytes | CPU/request | Allocated/request |
| --- | --- | --- | --- | --- |
| Complete runtime list | 46.140034 ms | 9,728,015 | 77.77816 ms | 76,677,041 B |
| `?summary=1&limit=50` (ignored by baseline) | 45.701786 ms | 9,728,015 | 78.05562 ms | 79,814,134 B |
| `?counts=1` (ignored by baseline) | 44.391607 ms | 9,728,015 | 76.40736 ms | 77,723,112 B |

The old first call of the summary URL took 50.306367 ms. Retained heap after that workload was 43,495,528 B; process peak RSS was 193,642,496 B. RSS is a process highwater including previous full-list requests, not idle hub memory.

## Final local results

Verified code commit: `5fce80004096f3003ee4a6e5d74cec2ac6eb370b`, based on master `4cd447fe15` including the author's #5972 fixture repair. PR: [#5963](https://github.com/nwverse/persona/pull/5963). The final current-base run passed all six related packages, then the focused race run and the real HTTP/WebSocket 8,000-row test.

| Final request | Mean (warm) | JSON bytes | CPU/request | Allocated/request |
| --- | --- | --- | --- | --- |
| Complete legacy list | 45.197917 ms | 9,728,015 | 78.49078 ms | 80,858,640 B |
| `?summary=1&limit=50` | **0.085054 ms** | **33,532** | **0.10252 ms** | **85,748 B** |
| `?counts=1` | **0.020894 ms** | **824** | **0.02595 ms** | **5,837 B** |

The **cold** first paginated request took **148.337854 ms** to initialize the index, versus 50.306367 ms for the baseline summary URL's first call. This cold cost is not the warm latency above. A negotiated `sessions_list` socket initializes the index before subsequent HTTP pages. Final retained heap: 65,511,472 B after the first-page workload / 65,484,008 B after counts, versus 43,495,528 B in the baseline: about **22.0 MB additional retained memory**. Final process peak RSS: 183,648,256 B, versus baseline 193,642,496 B; both include the legacy complete-list workload and are not idle-hub RSS estimates.

Unfiltered aggregate counters are maintained on updates, so unfiltered revision deltas do not build/sort an 8,000-row list just to report their total. Filtered count cache misses count matching membership without sorting it. The real WebSocket test received **65-byte** invalidation notices before and after a title-only change, no full list, and the authenticated HTTP delta returned only the changed row.

Complete package test output:

```text
ok   github.com/nwverse/persona/server/internal/hub          112.653s
ok   github.com/nwverse/persona/server/internal/extmgr        46.575s
ok   github.com/nwverse/persona/server/internal/agents         0.027s
ok   github.com/nwverse/persona/server/internal/sessionlist    0.193s
ok   github.com/nwverse/persona/server/internal/i18n           1.023s
ok   github.com/nwverse/persona/server/internal/proto          0.023s
```

Focused race output:

```text
ok   github.com/nwverse/persona/server/internal/hub           4.651s
ok   github.com/nwverse/persona/server/internal/extmgr         2.168s
ok   github.com/nwverse/persona/server/internal/agents         1.057s
ok   github.com/nwverse/persona/server/internal/sessionlist    1.852s
```

Measurement / real-transport output:

```text
--- PASS: TestAgents8000Measurement (4.81s)
8000-session WebSocket notice: 65 bytes
8000-session WebSocket notice: 65 bytes
--- PASS: TestRuntimeList8000OverHTTPAndSocket (0.34s)
--- PASS: TestPersonalAgentPagedRoutes8000 (0.06s)
```

`go vet` and a local `personad` build passed on the final integrated source. Privacy scan: clean. Documentation checks: 11 path tests and 3 Go-path tests passed. No simulator screenshot was taken because no screen changed.

## Boundaries and follow-up

- Cursor retention: five minutes / 32 views; query caches: 64 current-revision views; change journal: 128 revisions / 16,384 changed identities. Restart/eviction has explicit 409 reset semantics.
- Filters use supplied paths/space ids, not inferred repository trees, to-do grouping or preference-defined custom sections. Legacy requests and old socket clients retain complete-list behavior.
- GitHub workflows were found active with PR jobs queued, despite the initial report that Actions had been stopped. Local tests, not an unexecuted CI result, are the acceptance evidence. This work did not alter workflow settings.
- A prior merged-base run passed the hub but failed existing `TestNodeProgramsForgotten` once. Its isolated 10-repeat run and full extension-manager rerun passed; this failure was not silently skipped.
- The first full run also caught the advertised-capability golden mismatch; only the two `sessions_list` feature entries were added to the connection fixture.
- UI screenshots are not applicable: this PR changes no screen. The Apple client and its dark/light simulator acceptance belong to the separate client work.
- Integration against an intermediate master exposed dialog revision fixture drift. The author merged #5972; the final all-green run above includes that repair. Required failing tests were not skipped.
- **Production deployment and live verification are handed back to the user; this work does not deploy. No live endpoint result is claimed.** The remaining live checks are bounded first-page output, totals/group counts, cursor continuation and a state/title-only revision change. Never publish live session identities, titles, machine addresses or credentials in GitHub evidence.
