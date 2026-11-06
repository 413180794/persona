Acceptance date: 2026-10-31. Branch: `p0/v-ios`, code version: `2606f3d`.

This is a historical acceptance record: commands, paths and system confirmation text keep the old Jarvis name of the time. They are compatibility / historical references, not a build or upgrade verification after the Persona rename.

**Conclusion: the API checks passed; iOS P0 acceptance is not complete.** The current sandbox cannot reach CoreSimulator, so no simulator could be created, no App installed and no screenshot taken. The API checks and the Swift pairing-link parsing check below do not replace a real App connection and UI tests. The device pairing required by README section 10 was not done either.

| Task step | Actual result | Evidence (local temp files) |
| --- | --- | --- |
| Temporary config | Created a minimal build config under a temporary HOME, bundle id `com.example.jarvis`, no team; the hub used a dedicated database from environment variables, the two specified loopback listen ports and an empty machine list | `/tmp/jarvis-ios-verify/home/` |
| Dedicated simulator | Ran `xcrun simctl create Jarvis-P0-v-ios com.apple.CoreSimulator.SimDeviceType.iPhone-17-Pro`, exit code 1; the CoreSimulatorService connection was refused, simdiskimaged was unavailable, no device ID returned | `/tmp/jarvis-ios-verify/simulator-create.log` |
| hub build and start | The original command exited because reading VCS status failed; with `-buildvcs=false` the build succeeded. HTTP and TLS started fine under the fake HOME | `/tmp/jarvis-ios-verify/go-build.log`, `go-build-no-vcs.log`, `hub-isolated.log` |
| iOS build | Copied the version-controlled iOS files to a temp directory; config generation and XcodeGen succeeded; tried a simulator build with `CODE_SIGNING_ALLOWED=NO`; CoreSimulator was unavailable and the Swift Package fetch hit an HTTP/2 interruption, so no successful build | `/tmp/jarvis-ios-verify/source/ios/`, `ios-build.log` |
| Legacy-format pairing | `jarvisd hub pair --legacy` succeeded; the command only prints the address, the master token and a QR code, and the probe built the link with the same encoding rule as the source. HTTP and WebSocket checks passed; not launched in the App | `/tmp/jarvis-ios-verify/probe.log`, `protocol-results.json` |
| New-format pairing | `jarvisd hub pair` succeeded; the probe used the link from the command output directly; extra fields include `hub`, `fp`, `tls`; HTTP and WebSocket checks passed; not launched in the App | Same as above |
| Existing Swift link parsing | Extracted `ServerConfig` verbatim from `ios/Shared/APIClient.swift` and parsed both real links in a temporary Swift program; both succeeded; the new parameters did not affect reading `url` and `token` | `/tmp/jarvis-ios-verify/PairParser.swift`, `swift-parser.log` |
| Two connection UI tests | Started the repository's existing mock and selected `ConnectionTests/testOutageShowsConnectingThenRecovers` and `ConnectionTests/testShortDropDoesNotFlash`; Xcode exited before running the tests, exit code 74. An attempt with automatic resolution disabled also reported a missing `Package.resolved` | `/tmp/jarvis-ios-verify/ui-tests.log`, `mock-hub.log` |
| Screenshots and UI state | No screenshots; "Connected", no error alert, and the machine list in the App were all unverified | No screenshot path; no images produced |
| Cleanup | This lane's test hub and mock have exited and all three listen ports are closed; simulator creation failed, so there was no device to delete; existing simulators were not touched | `/tmp/jarvis-ios-verify/cleanup.json` |

The API checks followed the existing App's pairing order, but the requests were sent by a temporary Python probe; they do not pose as an end-to-end App result. Both links gave these results:

- `GET /api/v1/machines` returned HTTP 200 with `machines` as `[]`.
- `GET /api/v1/ws` with a Bearer token completed the HTTP 101 upgrade and received the `hello` and `machines` events, the latter with `machines` as `[]`. The probe did not send `client_hello`. No hub connection log attributable to the iOS App was obtained this time; the handshake and event records only prove the probe connected.
- `POST /api/v1/auth/tokens` returned HTTP 200 with string fields `id`, `token`, `role`; requesting the machine list again with the exchanged device token still gave HTTP 200 and an empty list.

Issues found and acceptance boundaries:

- `hub.json.public_url` has to be set explicitly to the test HTTP address. Setting only `listen` does not update it; the `url` in the legacy pairing QR code comes from `PublicURL`, otherwise it falls back to the default host address and default port. Only the temporary config was amended this time.
- An empty `machines` does not turn off the Lark background tasks. Without `lark.token`, `agentToken` in `server/internal/lark/lark.go` tries `docker inspect` on the default container; the first startup log showed the read failing. Once noticed, the process was stopped, the temporary `lark.base_url` pointed at a local mock, a test token filled in, and the hub restarted. Docker was not touched manually; this does not support claiming that the minimal config never tried to access a container. The mock does not implement `/agent/v1`, so the later background 404s are a known result of this isolation config and should not be reported as pairing API failures.
- This check found no compatibility failure in the pairing APIs or fields above, but whole WS messages were not decoded with the real iOS `Envelope`, and long-term capability gating of new events was not verified, so no overall compatibility conclusion can be given.
- The UI tests did not run, so it cannot be confirmed that the mock changes did not break them. The two tests above need to be rerun in an environment with CoreSimulator access, along with App pairing and screenshots for both links.
- An interrupt was sent to the Xcode build; another attempt to kill the process directly was refused by the sandbox, and its final exit status was not confirmed. The test hub and mock exiting and their ports closing were confirmed separately.

Only this report was added; `ios/` and `server/` are unchanged. Build copies, probes and logs are all under `/tmp/jarvis-ios-verify/`. The temporary config and files marked `private` contain test credentials and are not under version control. No git add, commit, push, stash, deploy or connection to a live hub was done. Performance was not measured; this report draws no performance conclusion.

## Addendum: App-level verification (2026-10-31, outside the sandbox)

**Conclusion: the unmodified iOS App is compatible with the new hub; iOS P0 acceptance passes (simulator; device pairing still to do).**

- Environment: the integration branch's hub on loopback (two listeners, HTTP + TLS, a dedicated test database, an empty machine list, Lark pointed at an unreachable address);
  the iOS App built from the current `ios/` as is (Debug, no signing team), installed on a new iPhone 17 Pro simulator (iOS 27.0).
- **Both links pair and reach the main screen**: the legacy format (`--legacy`, master token) and the default new format (`jp_` temporary code + `hub` / `fp` / `tls` / `alt`).
  The App reached the hub with the credentials (only Lark-dependent requests returned 502, which is down to the test config).
- **Credential exchange** (the key to an old App staying usable long-term after scanning a new code):
  - The DEBUG build skips the credential exchange when launched with `-pairURL` (`DeviceCredentials.swift`), so the test was: launch once with `-pairURL` so the config is written to the Keychain;
    then cold-launch without arguments, so the App goes through the real `upgradeCredentials()`.
  - Result: with the new format the temporary code was exchanged for a `phone/full` device token, the temporary code was marked redeemed, and the App then connected to the hub with the device token.
    With the legacy format the master token was likewise exchanged for a device token.
- When `simctl openurl` opens `jarvis://pair`, iOS 27 shows an "Open in Jarvis?" confirmation that a script cannot tap, so that path was not used.
- Found and fixed one issue: in the few seconds connected with the temporary code, the App registers phone tools; the hub looked up the device-token table with the temporary credential, failed and logged an error.
  Now a temporary-code connection only marks the phone in memory, and it is saved when it re-registers after switching to the device token (`register_phone` in `server/internal/hub/http.go`).
- On every cold launch the App gets one or two 401 responses, right in the first second (the hub log records only source and status, not the path). No effect on pairing or credential exchange; not investigated further.
- Not done: device pairing, direct public TLS connection (the current iOS does not support pinning and by design keeps using the intranet / VPN), the watch.
