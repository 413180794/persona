# Apple HTML: normal Mac matrix for the latest Coordinator (2026-11-06)

Related to #2025. This record covers normal / bounded probes only; it does not represent complete Mac HTML execution-safety acceptance.

## Source and scope

- Baseline: `8756ab8789dc97048e5e81515c7748979b9fac6c`, plus this termination-callback regression.
- Full `AssistantHTMLCard.swift` SHA256: `a839dae75bb23c48553f83a227db124f6884570b3c3a7b9df779a27e777766a5`.
- Request types lines 11–98 and Coordinator lines 295–525 extracted unchanged; extraction SHA256: `7bda24c4dca715a1c23ab139399b938bd2d72504501c81466090f67977ba0522`.
- Apple safety-test source SHA256: `8b07569eba13dd3cff5741a5a9c6e62f432662e87b3280ce9dab7c37dfb98007`.
- Only UIKit window replaced with hidden, nonactivating AppKit window; current tests reused, no second sandbox implementation. Separate Mac network positive control.

## Actual results

`python3 scripts/test/mac_html_security.py --output /tmp/richui-final-mac-security-v2`: build exit 0, normal tests exit 0, 13 methods / 21 parameter-expanded cases passed in 38.241 seconds. Swift Testing XML groups by method: 13 testcase, 0 failure, 0 error, 0 skipped.

Covers GET / POST forms, redirects, external scripts / fonts / CSS / SVG / media, system prompts, isolated storage, unknown handler / kind, child frames / parent bridge, current nonce, recovery through public termination callback, and page-removal cleanup of bounded work / timers / four-MiB buffers. Idle-listener and resource probes each run five rounds. Outside-sandbox network positive control passed; inside-sandbox zero-connection assertion retained.

New regression actually checks: termination callback before load gives no notification, duplicate callback notifies once, late callback after dismantle gives none. Existing same-HTML retry, new-nonce acceptance / old-nonce rejection passed too.

Worker PPID 1; no real node stopped, User-Agent request filtering, or real-service modification. No window activated / foregrounded. Build / tests through `xcode-slot` using temporary task HOME / caches.

Xcode 27.1; 26.0 unavailable, not compiled. Minimum compatibility policy unchanged.

## Failed run and evidence

First preparation directory checked extraction / adaptation only, no tests. First full build failed because Mac `callAsyncJavaScript` callback overload used async parameter labels. Changed callback to public `in:in:completionHandler:`, without altering probes, positive control, or deadlines. Failed `/tmp/richui-final-mac-security` retained, must not count as passed. Successful v 2 directory contains manifest, generated source, `build.log`, `test.log`, `results-swift-testing.xml`, status files.

## Latest-mainline revalidation

Before push, mainline added `String(localized:)` to isolation-rule failure wording. Network policy unchanged, but old hash cannot stand in for latest source. Rebased to `ee3c0dd62adcd5defa0c5f49acf65b5254ff03dd`, then fully reran candidate `9ce9da7d289c6e396f28b6caeb0d9aa9b7fe4849` in new `/tmp/richui-final-mac-security-v3`.

- Build / normal tests exit 0; 13 methods / 21 expanded cases passed, 38.155 seconds; XML 13 methods, 0 failure / error / skipped.
- Full-file SHA256: `4cbf445016c7e6c609a31d8aa6cfa3dba42919f1a239ed251fbd8d6cd609fc1c`.
- Unchanged extraction SHA256: `5fb60664a5f1904fc0be270caf014c02df7a293142d01c7100ee9b1fc997d37d`.
- Adapted-test SHA256: `147394d3a8b65456d21f7e7183d24eea6f6cc091ff4f11098122a2e0e5fd738c`.
- Mac network-test SHA256: `1abc9dbdab13933dd8ef67453e36195e9d0ee7704cfe8831e8485647b4ff148f`.

## Outstanding boundaries

- Normal matrix validates Coordinator, not full SwiftUI card / `NSViewRepresentable` wiring.
- Termination callback uses public delegate fault injection, not actual OS process kill.
- Literal loop not run in shared Mac login session; requires disposable externally destroyable Mac VM / test login session.
- No guarantee of immediate CPU / physical-memory recovery; no hard resource-quota claim.
- Mac execution gate still false; production switches untouched, no real main-chat writes, deployment, or TestFlight.

#2025 retains these full-execution acceptance boundaries and is not closed by normal-matrix success.
