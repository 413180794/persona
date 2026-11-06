# Bounded Agents list verification

The runtime chat uses the exact lookup added in server PR #6086 when `hello.features` advertises `sessions_lookup`: `GET /sessions?summary=0&limit=50&machine_id=<id>&session_id=<id>`. Initial entry, coalesced invalidations, reconnect and visibility recovery load only that complete record. Responses are identity-checked; missing rows and unauthorized reads never substitute another session or trigger a collection scan. Complete snapshots replace optional fields so stopped background/subagent/workflow/queued tasks do not survive as stale arrays.

On an older paged hub without `sessions_lookup`, list reads use bounded `summary=0` pages and retain complete loaded/clicked rows; revision deltas update only retained detail identities. A non-paged legacy hub retains its initial legacy initialization and socket delivery. Active-chat reads and reconnects never redownload the full collection: they use retained data and the existing per-session info/transcript paths. An unlisted legacy chat can open with an explicit metadata-unavailable notice rather than scanning every session.

Run from the repository after installing web dependencies:

```sh
cd web
npm run typecheck
npm run lint
npm test
npm run build
node e2e/agents-performance.mjs <scratch-directory> 18983
```

Use an unused loopback port and a scratch directory with space for a source copy. The runner needs Go (the version in `server/go.mod`) and Chrome; `playwright-core` drives the installed `chrome` channel. It never starts a real node, reads node process environments, connects to a production hub, or migrates a database. Test-database environment variables are removed from the fixture process.

The runner copies the existing `server/` and local Go SDK into its scratch directory, adds `agents_hub_test.go` to that copy, and compiles the existing hub test package. The fixture uses the real hub session HTTP/WebSocket handlers with the same `syntheticAgentHub` as #5963: 8,000 invented sessions, four computers, 20 projects, ten computer-local spaces, four states and long previews. Production web assets are served by this loopback fixture. Unrelated API responses are minimal synthetic stubs. Personal-agent acceptance separately intercepts that collection's HTTP responses with 8,000 camelCase rows; it does not claim to measure a real extension upstream. The legacy test removes `sessions_list` from the socket hello and verifies the unchanged full-list HTTP route.

Acceptance includes:

- Exactly one initial `summary=1&limit=50` runtime page and no complete-list GET in negotiated mode.
- Every frozen page reached by scroll, with 8,000 loaded identities and exactly 159 continuation pages.
- Main-list DOM bounded to visible rows plus overscan, including at the end of the complete traversal.
- Title-only invalidation using `since`, with no new baseline GET.
- Server state/search/project filters and machine-scoped space filtering/counts.
- Personal-agent first page, scroll continuation, virtual rows and its own revision delta.
- Older-hub fallback with a visible legacy list and all 8,000 rows returned.
- Cold deep link to the oldest row outside the first page: one exact full snapshot, including queued work/background data, with no cursor/list scan.
- Active-chat invalidation: full uncapped title changes and omitted task arrays replace cached metadata.
- Paged-but-no-lookup compatibility: complete 50-row pages plus revision recovery keep the chat current without an exact parameter or full-list redownload.

Artifacts are `results.json`, `agents-scroll.trace.json`, light/dark runtime, personal-agent and active-chat screenshots, and `hub.log`. Load the trace in Chrome DevTools Performance. Initial rendering is recorded on the second animation frame after the first visible main-list row enters the DOM. The rapid-scroll sample advances 240 CSS pixels per animation frame until all 8,000 rows are loaded and the scroll reaches the end (45-second ceiling). FPS is animation-frame cadence in headless Chrome, not a claim about a physical phone, GPU presentation or internet latency. Payload bytes are uncompressed HTTP JSON.

## Integration lessons and boundaries

- Do not fetch a legacy personal-agent list from a mount effect before the socket hello: that bypasses negotiation and downloads the entire collection on a cold `/a` entry.
- Keep frozen-page cursors, the completed delta checkpoint and aggregate-count revisions separate. Keep updates/removals for not-yet-loaded identities so later frozen pages cannot restore stale rows. Apply a delta checkpoint only after all its pages succeed.
- Summary rows and complete subscribed chat details are separate stores. Explicit `waiting: false` overrides old task arrays; missing arrays are not proof that no task runs.
- Counts are intersections of all supplied filters. State controls request counts without `state`, but retain the chosen computer/project/space/query scope. Directory suggestions use separate facet counts and render at most 50 options.
- The transport sorts by update time and lexical composite identity, not client pin/state/name preferences. Preserve nanosecond ties and decode runtime removals as JSON tuples, never by splitting on `/`.
- The server does not expose to-do/custom-section/pin/mute/botmux/quiet/assistant facets or custom sorting. Negotiated mode offers its supported computer/project/space/account/state/query filters and raw collection counts; it retains row actions but does not present incomplete-prefix counts as global preference-aware totals. Legacy mode keeps the original presentation. Extending those server facets is separate work, not a web-only approximation.
- This change only merges the web implementation. Production deployment and live validation are deliberately not part of the runner or the handoff.

## Approved rollout

After the deployment owner obtains user approval, deploy the hub binary containing server PR #6086 and the web assets containing #6051. Deploy the backend first (or together in the approved release), then reload the web client so it negotiates `sessions_lookup`. This endpoint extension needs no database migration or new node RPC. Until then the documented older-hub fallback applies. This task does not deploy or restart any production service.
