# Rolling-upgrade verification (#6223)

Local isolated verification, 2026-11-07. Baseline: `39a706a338`; identity cutover: `5969be6293`.
The requested Chinese contract matrix is [rolling-upgrade-compatibility.md](../../protocol/rolling-upgrade-compatibility.md).
This record is not permission to deploy, publish, upload TestFlight builds or resume a release loop.

## Real binary matrix

`python3 -I scripts/test/test_rolling_compatibility.py --workspace <private-workspace> --real-nodes`
requires an explicitly supplied `PERSONA_COMPAT_DATABASE_URL`. The script compiles the baseline and current
binaries itself, or accepts already-built binaries through `--old-bin` / `--new-bin`.

| Hub | Manager | Node | Frozen wire fixture | Real node |
| --- | --- | --- | --- | --- |
| Current | Baseline | Baseline | PASS | PASS |
| Baseline | Current | Current | PASS | PASS |
| Current | Current | Baseline | PASS | PASS |
| Baseline | Baseline | Current | PASS | PASS |

- Both hub and manager are real binaries in every row.
- The same wire-fixture source is compiled against each revision's actual protocol structs. The baseline
  product implementation is not patched. It checks the session-origin field, legacy request headers,
  context transport and retry receipts. Two sends with one operation ID must add exactly one send log record.
- Real nodes run inside an already available public `debian:bookworm-slim` image with independent PID,
  network and HOME namespaces. No host HOME, sockets or process namespace is mounted, and image pulls are disabled.
  The real-node rows check machine registration, protocol floor, version-appropriate capability and session reads;
  message execution is covered by the deterministic wire-fixture rows, not claimed for the empty real-node environment.
- Only randomly named fixture databases are created/dropped. Cleanup stops only owned process groups and verifies
  the container's nonce label before removal. Diagnostics and credentials stay in the private fixture workspace.

## Current clients against the real baseline hub

`--clients` adds the current web REST probe; `--android` adds the current Android JVM transport probe.
Use `PERSONA_COMPAT_GRADLE_ARGS` for build-environment-specific Gradle flags, not checked-in private paths.
Both probes receive a disposable loopback endpoint, token, machine and session through environment variables.

- Web REST probe: PASS. Session decoding, context/link/tool aliases, duplicate message write and a completed receipt.
- Web browser: PASS for legacy local-storage login, first-frame legacy theme, Agents list, actual session navigation
  and rendered synthetic answer in dark/light. Browser runtime errors: none. No API response rewriting or mocked hub.
- Android live transport probe: PASS against the real pinned baseline hub with both baseline and current managers.
  The web probe passed in both cases as well. Each client's two same-ID writes added exactly one actual fixture
  send and returned a `done` receipt. A preliminary cached Gradle exit 0 added no send and was correctly rejected;
  the final probes disable build/configuration caches and explicitly rerun the test task, with private logs retained.

### Inspected screenshots

These are synthetic fixture data only, with no credentials, personal records or real computer names.
The first chat capture was still loading; it was replaced after waiting for the actual fixture answer.

| Agents | Chat |
| --- | --- |
| Verification image removed; see the associated PR. | Verification image removed; see the associated PR. |
| Verification image removed; see the associated PR. | Verification image removed; see the associated PR. |

## Unit, contract and build verification

- Server full suite after the final current-master merge: PASS on real isolated Postgres databases with Go 1.27.1:
  91 packages, 6,232 top-level tests (9,179 passing test events including subtests), zero failures; 53 skip events (52 top-level) for optional
  live, platform and helper-only cases. Earlier failures are retained: DTO embedding and nil optional environment
  lookup were fixed, retired-choice wire assertions were updated, and the loader source parser was corrected.
  Go 1.27 uses `GOTMPDIR` for `testing.TempDir`; both temporary-directory variables now name a short real root
  outside personal configuration ancestors. Synthetic update stubs disable VCS stamping explicitly, without
  changing production build or signing checks.
- Focused server compatibility: PASS for headers/conflicts, session presence, environment/root aliases, builtin
  MCP namespace, HPKE domains, push-to-start registration selection and legacy look projection.
- Services/discovery: PASS with race checking for single-store executable aliases, independent-installation rejection,
  legacy environment-file/socket retention and per-service mDNS goodbye handling.
- Runtime packages: regenerated from integration sources. The loader's package manifest and hash checks remain active.
- SDK Go `./ext ./wire`: PASS; TypeScript SDK: 89 tests PASS and compiler/typecheck PASS, including 13 negative fixtures.
- Web full suite after the final current-master merge: 5,025 PASS in 534 files, one optional live test skipped when its environment was absent; build PASS.
  The separate explicit live probe above did run against the real baseline hub.
- Android changed-module suites after the final current-master merge: 2,501 PASS, zero failures, eight optional
  live tests skipped without environment input. One is the compatibility probe, executed separately below;
  seven are new upstream native-extension showcase cases not requested here. All ten test tasks explicitly rerun
  without a test filter, and the APK repackaged successfully. Counts: contracts 231, domain 596, hub 355
  (354 PASS, one optional skip), navigation 21, data 138, design system 145, chat 304, settings 550
  (543 PASS, seven optional showcase skips), phone tools 27, app 142. One startup retry waited for a shared
  Gradle cache owner to exit; no locks or other processes were removed. Earlier full hub tests saw foreign port-discovery
  requests consume MockWebServer responses. An observed exact diagnostic request reproduced the issue; the test-only
  queue adapter excludes only that method/path/user-agent combination. All 42 queue fixtures use it; ten custom
  dispatchers are unchanged. An initial wrapper compile failure lacked implicit TLS setup methods; no hub test
  was counted passed in that run. TLS/start APIs now delegate unchanged, and extra regressions cover timed-read
  semantics and an uncertain Close verification with both generations' diagnostic requests interleaved.
  Normal root, API and refusal requests still consume fixture responses and remain asserted. No resident node
  was stopped or reconfigured.
- Script tests cover configuration precedence, legacy entrypoints, service environment files, Docker entrypoint,
  byte-identical archive aliases, manifest descriptors and privacy scanning.
- Exact identifier registration rejects unknown tokens, other file paths, wildcard permissions and stale entries;
  it does not waive privacy or authorize another repository. The requested Chinese matrix is a single-file exception.
- CI is disabled and was not reported as passed.

## Release and system boundaries

The existing publish/deploy/release entrypoints contained no default-deny compatibility gate to replace; no new
workspace/database-dependent release blocker was added. Existing privacy, version, ancestry, signing and digest
checks remain. Read-only artifact fallback is limited to HTTP 404; an integrity failure does not downgrade or retry
under another name. The current binary update channel has HTTPS and archive/binary SHA-256 checks, not a new
signed-manifest system; this verification does not claim update-manifest signature validation.

Not performed here:

- Production deployment, service restart, real pairing changes, TestFlight upload or a release loop.
- Real Android device/emulator visual or in-place package-upgrade acceptance; this environment has no usable KVM.
- Real macOS/Windows service installation or multicast network acceptance; service logic is tested with owned fixtures.
- Apple Keychain, Intent, system controls, nominal Activity types, watch in-flight transfers or iOS GUI acceptance:
  these remain owned by #6152 and #6192. Two unchanged negative test inputs were moved to a dedicated compatibility
  fixture and the native owner was notified; no Apple production behavior is changed here. This Linux environment
  has no Xcode: native compilation and Xcode 26.0 are not verified here. Their results are not inferred from
  this server/browser/JVM verification.
