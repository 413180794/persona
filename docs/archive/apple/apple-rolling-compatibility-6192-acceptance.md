# Apple rolling-compatibility acceptance (#6192 / #6277)

2026-11-07. This is a point-in-time local acceptance record, not a production deployment or a TestFlight release.

## Conclusion and frozen source

The new Apple client works against the actual pre-rename hub baseline `39a706a338` in an isolated lab. Full-suite code freeze: **`3768f2c4e4`**, including current-master behavior and the separately tracked synthetic fixture fixes from #6370 (merged at `c0f2bac1b3d16e09d62cebfe0cd81c2f31eb5f5d`). The subsequent documentation reorganization changes comment/help-document paths, not the accepted network contract or GUI behavior; the final conflict resolution retains this lane's capability/alias logic and adopts the new guide paths. The complete native dependency #6296, merged at `868c71dc2f22d5711a416b59a251ab6f9ca0600d`, is included. Temporary caller/crypto overlays were removed before validation.

- iOS full suite: **2,421 passed, 1 independently reproduced existing baseline failure, 4 unchanged opt-in skips** (2,426 definitions). Raw `xcodebuild` exit **65**. Zero new failures after the explicitly authorized exact-baseline comparison; this is not an all-green raw suite.
- Mac full suite, including the real old-hub integration: **387/387 passed**, zero failures/skips, exit 0.
- Persona Link: **28/28 XCTest tests passed**, zero failures, exit 0. Its separate empty Swift Testing run is not another 28 tests.
- Real old-hub GUI: **2/2 passed**, zero failures/skips, exit 0. All six send/cold-reconnect/main-conversation screenshots were actually inspected.
- Final iOS test builds include Watch and extensions; Vision/Share simulator build passed. Minimum watch deployment target remains 10.0.
- Precise Apple naming/registration guards **11/11**, Apple localization **55/55**, document paths **12/12**, Go document paths **3/3**, README language parity **8/8**, external-service references **4/4**, privacy scan **clean (23 configured checks)**, committed diff checks clean.
- No deployment, production restart, migration apply, TestFlight upload or release-loop operation was performed.

The opposite client/server direction is owned by #6223, documented in [the shared contract](../../protocol/rolling-upgrade-compatibility.md) and [its acceptance record](../operations/rolling-upgrade-compatibility-verification.md). This lane does not claim to have independently rerun that complete server/Web/Android matrix.

## Contract boundaries

| Boundary | Implemented and verified behavior |
| --- | --- |
| Capability selection | Only this connection's authenticated `hello.features` containing `persona_contracts_v1` enables canonical outbound context/file tags. No hello/capability means the previous contract. Disconnect, reopen and receive failure clear the selection; no persisted/process-wide switch or build-name guess. |
| Operation IDs | Both header names carry the same lowercase UUID, including retries. Real old-hub integration sends twice with one operation ID and sees one user message and one reply. |
| Session origin | Canonical field presence wins, including false; previous field is the fallback. Real old regular-session origin is false, real old main-conversation origin true. Watch snapshots mirror both keys through the shared envelope. |
| URLs | Exactly both approved schemes are registered by the four app plists and three Xcode app targets. Runtime readers accept both. Previous conversation paths remain origin/path scoped; computer pairing still only prefills its own confirmation UI. |
| Contexts and attachments | Chat, voice, watch and file readers accept both generations through shared patterns. Both closing tags are sanitized with the existing two-pass linear treatment. Only protocol tag names are converted on outbound messages/initial prompts. |
| Recording transport | Transfer envelopes and receipts encode once, mirror identical bytes under both keys and read either generation. Malformed present canonical values do not fall back to apparently valid old data. |
| Push/config callers | Callers use the complete native dual-domain authenticated decoder and shared Info.plist fallback. Unit coverage includes wrong keys, tampering, freshness and forged unauthenticated outer route/wake/withdrawal fields. |
| Persona Link | One physical compatibility source via a relative symlink, storage-only compilation. Existing environment/state roots, service plists, passive connector output and installed-bundle handoff are read without moving/deleting state or silently pairing. |
| Naming guards | Duplicate keys/names and single physical old-identifier source checks remain strict. Exact OS registration and shared-reader assertions replace single-only contracts; no file-existence conditional or broad scan exemption. |

## Actual old-hub lab and interaction

The lab runs the actual baseline hub, assistant/manager and node binaries in dedicated containers with a fresh isolated Postgres database, blank HOME and its own state volume. No host HOME/PID mount or production credentials are used; the database has no host port and hub ingress is loopback-only. Device credentials are generated by the disposable lab and kept in private fixture files.

Synthetic Echo and assistant runtimes provide replies and an approval card; no real LLM or user data source is used. Passing integration covers:

1. Authenticated old hello without the new capability; the client retains old outbound naming.
2. Machine/session creation, history and raw old origin fields.
3. Actual WebSocket deltas, duplicate same-operation delivery exactly once and permission approval.
4. Actual old main conversation and its true origin marker.
5. Disconnect/reconnect with old hello and retained history.

Passing GUI covers separate fresh regular/main histories for each appearance, unique sent text, a real reply, cold relaunch **without pairing reinjection**, retained matching reply and a main-conversation send/reply. The unique suffix is reused for that appearance's cold check; historical same-name replies cannot satisfy the test.

## Verification commands and evidence

All `xcodebuild` executions use `~/.local/bin/xcode-slot`; own builds are serial. The simulator lock and two-device machine limit were observed; only this lane's device was shut down after validation. No mouse/computer-use automation was used.

The disposable fixture URL/token and appearance-specific session IDs are supplied to the test runner privately. Do not substitute production credentials or a saved paired hub. Commands below omit those values:

```sh
python3 scripts/persona_config.py xcconfig
xcodegen generate --spec apple/project.yml

# Run from apple/, with the disposable fixture test environment and an owned stock simulator.
~/.local/bin/xcode-slot xcodebuild -project Persona.xcodeproj -scheme Persona \
  -configuration Debug -destination 'id=<owned-stock-simulator>' \
  -parallel-testing-enabled NO -maximum-concurrent-test-simulator-destinations 1 \
  CODE_SIGNING_ALLOWED=YES CODE_SIGNING_IDENTITY=- CURRENT_PROJECT_VERSION=2 \
  PERSONA_PUSH_RELAY= PERSONA_GETTING_STARTED_URL= \
  -only-testing:PersonaTests test

~/.local/bin/xcode-slot xcodebuild -project Persona.xcodeproj -scheme Persona \
  -configuration Debug -destination 'id=<owned-stock-simulator>' \
  -parallel-testing-enabled NO -maximum-concurrent-test-simulator-destinations 1 \
  CODE_SIGNING_ALLOWED=YES CODE_SIGNING_IDENTITY=- CURRENT_PROJECT_VERSION=2 \
  PERSONA_PUSH_RELAY= PERSONA_GETTING_STARTED_URL= \
  -only-testing:PersonaUITests/OldHubCompatibilityUITests test

~/.local/bin/xcode-slot xcodebuild -project Persona.xcodeproj -scheme PersonaMac \
  -configuration Debug -destination 'platform=macOS' \
  CODE_SIGNING_ALLOWED=NO PERSONA_PUSH_RELAY= PERSONA_GETTING_STARTED_URL= \
  -only-testing:PersonaMacTests test
~/.local/bin/xcode-slot xcodebuild -project Persona.xcodeproj -scheme PersonaVision \
  -configuration Debug -destination 'generic/platform=visionOS Simulator' \
  CODE_SIGNING_ALLOWED=YES CODE_SIGNING_IDENTITY=- build
xcrun swift test --package-path apple/PersonaLink --scratch-path '<owned-link-build-directory>'
```

Actual runs use separate owned DerivedData/result bundles, isolated Mac test HOME and bounded process-group/inactivity watchdogs. Final private evidence labels:

- `ios-old-hub-full-signed-20261107-154545.xcresult`
- `ios-old-hub-gui-signed-20261107-154545.xcresult`
- `mac-final-integrated-20261107-155046.xcresult`
- `vision-final-integrated-20261107-155046.log`
- `link-final-integrated-20261107-155046.log`

The documentation-path integration is `b2281f3c60223c2092a8b4c3085b105da78eddf0`. It was subsequently recompiled with iOS/Watch/extensions build-for-testing (exit 0), rerun through the complete Mac suite (387/387), built for Vision/Share (exit 0), and tested with Persona Link (28/28). The iOS full/runtime and six GUI captures above belong to the stated 3768f2c4e4 freeze; they are not falsely described as another full iOS run on the later comment/help-path integration. Latest supplementary labels are `ios-doc-path-final-build`, `mac-final-integrated-20261107-161001`, `vision-final-integrated-20261107-161001` and `link-final-integrated-20261107-161001`.

Counts are read from completed xcresult summaries/test trees. Expanded parameterized test runs are not added to definition counts. Raw failures/skips remain visible; the comparison allows only the complete exact `PersonaTests` / `StateNamesTests/areDefinedOnce()` failure text already reproduced below. Required real old-hub tests must pass and only the four unchanged opt-in skips are accepted. GUI has no baseline exception.

Toolchain is Xcode 27.1. Xcode 26.0 is unavailable and **not compiled**; no minimum-compiler policy was raised. watchOS 10 is a compile/deployment-target boundary, not runtime validation on a watchOS 10 device.

## Exact existing baseline failures

### State-name duplication

Clean detached `origin/master` at `f3b760a85db187564c0c2a4010c97bd5ff0d9c9e` reproduces the same two WatchPet catalog entries. The original test constants/catalog scan predicate were replayed using native Foundation/NSRegularExpression, with only macro unwrap/assertion reporting changed to equivalent CLI guards. Compilation exits 0, replay exits 1; the checkout stays clean. This is **not a full Xcode master run**.

```text
%lld finished [Watch Pet]: %1$lld 个做完了
%lld waiting for you [Watch Pet]: %1$lld 个在等你
```

Copyable command/source/output is in [the independent reproduction](https://github.com/nwverse/persona/pull/6277#issuecomment-6703275514). Issue search found the exact regression already in #6272; [the existing owner was notified](https://github.com/nwverse/persona/issues/6272#issuecomment-6703278635), no duplicate issue was created. Neither compatibility lane changes WatchPet/catalog or weakens its scan. The user explicitly permits only this exact baseline failure to be excluded from the new-regression comparison without waiting for its fix. It remains a failed test in raw counts.

### External-service reference guard

The earlier `python3 scripts/test/test_external_service_refs.py` run reported **2 passed / 1 failed**, exit 1, with three existing WatchPet test/asset-script references; clean master reproduced the same hits. The subsequent independent documentation cleanup #6354 corrects local-reference recognition without a name/file exemption. On the final integrated tree the original suite now reports **4/4 passed**, exit 0. This lane did not modify the scanner or WatchPet; the earlier failure remains historical evidence, not a current waiver.

### Unchanged optional integrations

Four dedicated conditions were not supplied: real infinite-loop HTML-card recovery, relay-delivered notification artifact readback, real HTTP video range reading and native video playback. Their tests were not enabled/disabled differently, removed or weakened; skipped is not passed.

## Preserved failures and corrected interpretations

- The first unsigned iOS run had 2,371 passes, 8 failures and 4 skips; both GUI cases failed cold saved-pairing. Signing/setup and obsolete single-contract fixtures were investigated; persistence assertions were retained.
- A later signed full run genuinely hung in `AppModel.init → AppIconModel.live → UIApplication.alternateIconName`, waiting on the LaunchServices alternate-icon queue and its synchronous XPC setter reply. No Native/Keychain frame was in that blocking chain. The owned task was stopped, its device shut down and the unfinished result retained, not counted as passed.
- The merged native fix isolates only DEBUG injected unit hosts from the live system icon backend, retaining FakeSystem business assertions. Normal UI target apps and Release retain UIApplication; the native lane separately verified a real icon UI. The final complete suites did not hang.
- The first integrated GUI failed because the reused fixture's seed was outside its visible history tail. Its old same-name replies could also manufacture a later apparent send pass. Fresh per-appearance histories and unique text fixed the fixture without weakening history/cold/main assertions or deleting previous evidence. Final GUI passed and was visually inspected.
- A private fresh-fixture seed attempt returned 409 because the old runtime required `client_msg_id`; the helper was corrected to send a unique ID. The production client was not changed for this setup issue.
- After current-master source cleanup, truncated URL/JSON/Markdown fixtures stopped compilation. The independent two-file fix #6369/#6370 restores synthetic grammar and distinct docx/wiki/minutes/empty-date semantics without production or assertion changes. Native parsing, JSON preflight and actual affected tests passed.
- The reused host subsequently had four missing-scene/window failures in the full run (2,417 passed, 5 failed, 4 skipped). These were rejected, not waived. One independent fresh stock device then ran the unchanged tests: all four passed, leaving only the exact StateNames baseline. This demonstrates the accepted environment comparison, not a proven explanation of why the reused host lacked scenes. Both owned devices were shut down and failed evidence retained.
- Simulator signature evidence is separate from production access groups: `codesign --verify` succeeds, embedded entitlements/ordinary `.xcent` are empty, while `*-Simulated.xcent` has three configured Keychain access groups. An empty embedded dictionary alone does not establish ineffective simulator groups. Passing simulator Keychain/cold-pairing tests do not prove production access-group continuity.
- Purpose checker passes the built app and extensions: 13 app purpose strings and 3 app InfoPlist tables; 6 tables across App/watch have no empty or key-placeholder values. Actual app URL registration is exactly the approved two schemes.

## Inspected synthetic GUI evidence

All six latest images contain only synthetic fixture data and were individually inspected. Light uses the unique suffix `61FDBAD0`, dark `8AD2715E`; each suffix matches that appearance's send, cold-reconnect and main-conversation frames. The keyboard is visible in send/main frames, absent after cold relaunch, and no notification alert obscures content.

Following the module documentation cleanup, verification media is retained outside the checkout, not committed into this archive. Native PR attachment upload was not completed: the installed CLI has no attachment option and the browser is signed out. No login secret was requested and no browser upgrade was performed; the opened page is left available. The inspected PNGs remain in the private evidence directory under these names:

| Scenario | Light filename | Dark filename |
| --- | --- | --- |
| Unique send / reply | `old-hub-light-sent.png` | `old-hub-dark-sent.png` |
| Saved pairing / cold reconnect | `old-hub-light-cold-reconnect.png` | `old-hub-dark-cold-reconnect.png` |
| Main conversation | `old-hub-light-main-conversation.png` | `old-hub-dark-main-conversation.png` |

## Native-system and release limits

The [native reversible acceptance](persona-native-6152-reversible-acceptance.md) separately verifies retained preferences/unsent data, same-identity upgrade/reinstall and actual execution of one old saved Shortcut with unchanged action blob. Those are #6152's evidence, not a new independent system run here.

**Old Live Activity ID retention did not pass**: the simulator installer removed IDs even when reinstalling the same old build. Real nominal type registration or creating a new activity is not proof of old ID continuity. Real-device App Store upgrade, installed controls and watchOS 10 runtime continuity remain unverified. No new signing account/profile was created or altered.

GitHub Actions remain queued rather than executed. CI is **not run**, not passed. Local validation and the disclosed exact upstream failures support merging under the project's queued-runner exception and the explicit baseline decision; code/test regressions are not waived.

After merge, only owned devices, fixture credentials/containers/volumes/networks, build directories and worktrees are removed. Failed logs/samples/completed xcresults are retained privately. This task does not deploy the merged source or upload TestFlight.
