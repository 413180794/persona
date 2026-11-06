# Persona rename: local implementation acceptance and release boundary

## Current conclusion

As of 2026-11-03, the local migration implementation, the compatibility gap fixes, and the open PR audit against a fixed snapshot are done. The final code baseline is `761ce5473289be03808f7d941d8c17f10813cbaf`, which includes external environment file compatibility #837, the incremental audit archive #877, and the test database isolation fix #879.

**This does not mean #277 has nothing left.** Migration items in older feature PRs stay with their original owners; features not yet merged don't go into the final frozen version. Upgrades on real devices and continuity of system Shortcuts still need device acceptance. The final release must run the full pipeline on a separate, clean, fixed commit; an intermediate cycle build can't count as final acceptance.

## Compatibility boundaries in place

- The product shows as Persona; on the server `personad` is recommended, and the old `personad` entry point and artifacts keep working.
- The new default runtime root and build config directory are `~/.persona` and `~/.config/persona`; old directories stay compatible through existing data and missing-alias handling. Two independent directories are never merged or overwritten; a custom root doesn't change the global default alias.
- `PERSONA_*` definitions take precedence, including empty values; old `PERSONA_*` are still supported. Switching config recomputes the derived exports and doesn't carry over the previous environment's addresses or bundle settings.
- Custom roots in Go, the SDK and scripts are normalized lexically; the default socket can use the short old path of the same store. Service recovery distinguishes hub / node / link, and supports old units without HOME, environment resets, user drop-ins, XDG paths, and a HOME field in the environment file.
- Native, web / hub / connector inputs accept both the new and old schemes; generated links keep the old format so clients that haven't upgraded still work.
- Bundle, module, Intent / Control / Live Activity, serialization, cache, protocol, database, service, and runtime update identities are kept. Names of existing users or external resources are not product copy and must not be changed blindly.

Full rules in the [migration notes](rename.md); the original audit and the last increment in the [PR audit record](rename-pr-audit.md).

## Verification recorded per fixed version

| Scope | Evidence collected | What it does not show |
| --- | --- | --- |
| Default storage and upgrade / rollback | #718: temporary HOME, symlink / independent root / loop / cross-process init, new and old entry points, and upgrade / rollback with a real old binary | Does not replace a recovery drill for every production deployment users have |
| Native branding | #756: 632 iOS tests (630 passed, 2 skipped for environment), 151 Mac tests; 80 iPhone screenshots across two styles in light and dark, Mac and watch previews; usage-description string check on the artifacts | Not actually compiled with Xcode 26.0; the watch simulator can't switch light / dark, so two native dark screenshots don't count as different appearances |
| Android | #745: 981 JVM tests for the copy consolidation passed, 108 screenshots across three styles in light and dark; later, on fixed `f98a77df`, the full integration suite of 1,327 tests and the APK build passed | The later round is not a new baseline for the earlier screenshots; upgrading an old APK on a real device is not claimed as accepted |
| Scripts / runtime | #773: 193 script tests and 99 runtime tests locally; #837: final 128 script tests and 16 targeted tests, re-verified item by item after fixes for issues found by an independent report | Not extended to exhaustive verification of full systemd syntax or real service environments |
| Web / local Go | #786: 108 script tests, 448 web tests and the build; under the canonical short directory, 2,167 Go tests / subtests passed and 72 were explicitly skipped | Skipped database / platform / fixture tests don't count as passing against real Postgres |
| Test database isolation | #879: 225 Python tests and 41 flow tests locally; 38 anonymous real pgx parse subtests for each of the two layouts; independent re-verification of query / PG environment / profile / TLS / parser child | No database connection made; does not replace the full real-Postgres suite or TLS handshake acceptance on the final freeze |

Each result belongs to its PR and fixed head. Counts from different versions are not added up, and tests run by others are not written up as reruns by the docs lane.

## Test safety and incident boundary

#812 isolates the real runtime root and the test process environment. #879 goes further: it rejects database URL query parameter overrides, external service / target sources and duplicate keys, clears `PG*`, and explicitly selects the two-prefix variables for the same dedicated test database. Explicit TLS parameters are kept; implicit OS-user profile files are not used. Upstream pgx may probe the metadata of the OS user's default TLS paths before merging parameters, so we can't claim there is no path probing at all.

The fixes and the independent anonymous re-verification are merged. The earlier pause on real tests / releases is not lifted by documentation: only after the release owner confirms the frozen commit contains both isolation fixes, and the full suite passes with the new launcher, do later deploys / uploads continue.

The failed freeze, exit codes, and protected incident evidence from 1045 are kept. There was already evidence of production files being deleted, and the full extent of the impact is still unknown; the limited read-only assessment of recovery feasibility isn't enough to identify which objects were deleted or confirm the original contents can be recovered. No recovery was performed. A new version passing tests or being released does not mean the incident caused no loss, nor that data has been recovered.

## Acceptance conditions for the final freeze and release

1. Freeze from a clean, merged commit that includes #837, #877, #879 and this acceptance record; don't mix other unmerged features into the freeze, and don't reuse a failed build number.
2. Before release, run the full script / runtime / web / native verification; the server may only use the fixed isolated launcher and the dedicated Postgres test database. Stop on failure; don't let later commands overwrite the failing exit code.
3. For the archive, check the App / extension usage-description strings, the entitlements of all four bundles in the release IPA, and production APNs; record the actual Xcode version and anything not run.
4. Deployment and health checks pass for the hub and the nodes in the config; the versions, manifests and every download SHA of the website and public artifacts match.
5. TestFlight internal and external both complete; confirm build status and accessibility before giving the user the final build number.
6. In #277, add the fixed SHA, actual tests run / skipped, live checks, remaining PR coordination, and the incident recovery boundary. Unfinished items are not changed to passed.
