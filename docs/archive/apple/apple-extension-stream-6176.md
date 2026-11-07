# Apple native extension-stream acceptance (#6176)

Point-in-time evidence for PR #6243, recorded on 2026-11-07. Scope: isolated local hub, generated wire, native Apple rendering and legacy HTTP fallback. No production deployment or TestFlight authorization.

## Frozen inputs and limits

- Final complete build/unit/UI matrix used mainline `c5fc87ae5` and Apple code at `5ae135823a`. This base includes #6198, schema/Apple cleanup, #6314/#6322's synthetic transcripts, #6296's DEBUG unit-host isolation and #6402's owned WatchPet state-name correction. Both lab hub/manager binaries were rebuilt on this base; the newer `HubNaming.feature` negotiation was preserved alongside private-stream cursor isolation.
- Earlier mainlines `868c71dc2` and `950e9ddf2e` completed full suites/UI with the then-unfixed StateNames failure. Those actual historical failures remain recorded below; no baseline waiver was used to pass the final gate.
- The lab hub/manager were independent Linux ARM64 instances rebuilt from the tested base, with the `persona_dev_resources` tag. Deno 2.9.6 was pinned. Development resource fallback was enabled only on the isolated manager; database, HOME, volume and networks were independent. No production configuration, user data, machines or push credentials were copied.
- Showcase used staged upstream roots/services/locale and the official SDK with an explicitly local-only `componentPage` registration adapter. The synthetic `native-contract` page exercised focus, code lines and review consent without filesystem access. These are not released contributions and do not establish source-package installation/upgrade release gates.
- Mac GUI runner could not connect while the session was locked. The user explicitly waived Mac XCUITest only, without unlocking or enabling DevToolsSecurity, in favor of full Mac units and offscreen snapshots. GUI follow-up: #6250. Release entitlements were unchanged.
- Actual Xcode: 27.1. Xcode 26.0 was unavailable and not verified; minimum-compiler policy was not raised.

## Actual authenticated transport

The test App used a device credential formally issued by the isolated instance. Authenticated `hello.features` contained `extension_ui_stream_v1`, and open negotiated `context.stream` with explicit state. The smoke was actual Deno → manager → hub App socket, not an injected renderer fixture:

```text
Deno → manager → app WebSocket: snapshot version 1
Empty callback fence: operations 0 version 2
Correlated receipt: accepted version 2 toast true
Local showcase socket smoke: PASS
```

This probe does not replace the Apple UI transport assertions.

## Completed freeze: results and retained failures

| Gate | Recorded result |
| --- | --- |
| Complete Mac units, final base | 418 total: 417 passed, 0 failed, 1 opt-in skipped; 76 suites, 13.734 seconds. All five actual source/HTTP/accessory integrations executed |
| Historical Mac attempts on earlier freezes | Complete reruns passed 402/402 in 13.680 and 14.354 seconds. One preceding attempt had 398 passed / 4 source-page failures overlapping the development source's 300-second restart. Failed log/result retained; no assertion or production limit weakened |
| Historical pre-test readiness failure (950e9ddf2e) | Mac build passed, but no tests executed before the source validator rejected the copied executable's `0700` permissions. Canonical launcher stderr identified `Permission denied`. Correcting this nonsecret artifact to `0755` made the unchanged validator return `ok`, then actual full matrices ran. Failed readiness probe retained; no namespace/resource-policy changes |
| Canonical tree/session tests | Passed within the full runs, including bounded read-only retry, late allocation retirement with omitted manager, event-correlated snapshots, exact numeric validation, frozen consent, matching host gate and no native-to-HTTP write retry |
| Mac offscreen native snapshots | Inspected NSHostingView light/dark PNGs from the actual hub; native v1/v2/v3 fences, reconnect, drafts, legacy HTTP, accessories and hidden-service cases passed. Latest passing output matches the previously inspected PNG bytes |
| Complete iOS units, final base | 2459 total: 2454 passed, 0 failed, 5 opt-in skipped. Swift Testing completed 2447 cases / 329 suites in 90.792 seconds; XCTest added 12 passing cases. The unchanged StateNames guard passed after #6402 was rebased in |
| Actual iOS native UI, final base | All 4 cases passed, 0 failures/skips, 129.213 seconds. Consent/cancellation, focus/drafts, file lines, review cancellation, segments, pagination/search and event-snapshot assertions retained |
| Historical iOS runs, earlier freezes | 950e9ddf2e: 2436 total, 2431 passed, the sole StateNames failure, 4 skips; UI 4/4 passed in 128.821 seconds. 868c71dc2: 2442 total, 2437 passed, the same sole failure, 4 skips; two full UI rounds passed all 8 executions in 131.371/135.159 seconds. Not counted as final-base executions |
| Latest lightweight gates | Canonical schema 12/12, codegen 7/7, SDK bundle 4/4, generation `--check`, document paths 12/12, Go document paths 3/3, README language checks 8/8, privacy and whitespace checks passed |
| Mac XCUITest | Did not execute cases; locked GUI runner connection unavailable. Only this gap was explicitly waived and handed to #6250 |
| Inspected media | Actual iOS light/dark, Mac offscreen light/dark and native watch dark. Owned watch simulator was deleted. watchOS simctl light-mode switching was unsupported; no light image was fabricated |
| Hosted CI | Pending/queued, not a passing conclusion |
| Production deployment, online validation, TestFlight | Not performed |

The skipped optional external old-hub probe is `OldHubCompatibilityIntegrationTests.realOldHubSupportsHelloSessionsStreamsReceiptsAndPermissionCards` on both Mac and iOS. The additional iOS skips are `AssistantHTMLSecurityTests.aRealInfiniteLoopCanBeDiscardedWithoutBlockingANewCard`, `NotificationServiceTests.opensWhatTheRelayDelivered`, `VideoFileResourceTests.realHTTPReadsMiddleTailAndRejectsChangedRevision`, and `VideoFileTests.nativeVideoPlaysSeeksAndCleansUp`. These were not executed and are not counted as passes. This task did not obtain production old-hub credentials or access production to turn on that probe.

Sanitized historical sole iOS failure (resolved by #6402, not waived):

```text
StateNamesTests.areDefinedOnce():
%lld finished [Watch Pet]: %1$lld 个做完了
%lld waiting for you [Watch Pet]: %1$lld 个在等你
```

## Positive readiness and lifecycle diagnosis

UI cases require a positive committed `stream:<revision>` and an actual no-visual-change callback advancing `stream:1` to `stream:2`. A loaded page could be legacy HTTP; a retained native tree can still be inactive.

The actual diagnostic image showed normal mounted/tab flags but SpringBoard's Persona icon-change prompt leaving the scene inactive. This was not a missing Form mount. Production visibility remains `onTabVisibility` plus `scenePhase`; the harness acknowledges only known Persona system icon/notification prompts and then waits for snapshot recovery. It never answers App/extension confirmation automatically.

Screenshots contain synthetic data only. iOS images come from actual UI-test captures; Mac images are NSHostingView offscreen renders of actual stream data, not window/gesture/focus evidence. Raw logs, xcresults, diagnostic images and pairing credentials stay private. Verification media is not retained in the final source tree; inspected historical images are referenced in the PR.

## Reproducible Apple commands

All variables below refer to an isolated local environment. `PAIR` is the lab's formally issued full-access pairing URL; pass it through the environment only, never print it or publish raw launch logs. Build first, check the actual source immediately before testing, and preserve any earlier failed attempt.

```sh
export PERSONA_CONFIG="$LOCAL_BUILD_CONFIG"
eval "$(python3 scripts/persona_config.py env)"
python3 scripts/persona_config.py xcconfig
xcodegen generate --spec apple/project.yml

TEST_RUNNER_PERSONA_STREAM_TEST_PAIR="$PAIR" \
TEST_RUNNER_PERSONA_STREAM_SHOTS="$SHOTS" \
~/.local/bin/xcode-slot xcodebuild test \
  -project apple/Persona.xcodeproj -scheme PersonaMac \
  -destination 'platform=macOS' -derivedDataPath "$MAC_DD" \
  -parallel-testing-enabled NO -disableAutomaticPackageResolution \
  -only-testing:PersonaMacTests CODE_SIGNING_ALLOWED=NO

~/.local/bin/xcode-slot xcodebuild test \
  -project apple/Persona.xcodeproj -scheme Persona \
  -destination "id=$OWN_STOCK_SIM" -derivedDataPath "$IOS_DD" \
  -parallel-testing-enabled NO -disableAutomaticPackageResolution \
  -only-testing:PersonaTests \
  CODE_SIGNING_ALLOWED=YES CODE_SIGN_IDENTITY=- CODE_SIGN_STYLE=Manual AppIdentifierPrefix=ABCDE12345.

TEST_RUNNER_PERSONA_STREAM_TEST_PAIR="$PAIR" \
TEST_RUNNER_PERSONA_STREAM_SHOTS="$SHOTS" \
~/.local/bin/xcode-slot xcodebuild test \
  -project apple/Persona.xcodeproj -scheme Persona \
  -destination "id=$OWN_STOCK_SIM" -derivedDataPath "$IOS_DD" \
  -parallel-testing-enabled NO -disableAutomaticPackageResolution \
  -only-testing:PersonaUITests/ExtensionStreamUITests \
  CODE_SIGNING_ALLOWED=YES CODE_SIGN_IDENTITY=- CODE_SIGN_STYLE=Manual AppIdentifierPrefix=ABCDE12345.
```

Wrap every wait/build with a timeout. Start only owned stock simulators, obey the global two-device limit and shut them down afterward. Local adapters, signing files and temporary credentials never enter Release/production configuration.

## Remaining acceptance

Apple's configured local complete unit suites and actual four-case native UI matrix passed with no failures after the owned correction was rebased. PR #6243 and issue #6176 record the merge/closure evidence. Mac GUI focus/gesture verification remains #6250 under the user's explicit locked-session waiver. The skipped optional integrations are listed above. Hacker News source release/install compatibility, Web/Android integration and production kernel resource guarantees remain separate; this record does not mark them passed.
