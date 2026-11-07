# Persona-only naming acceptance (#277)

Recorded on 2026-11-07 for PR #6079. This is source/local acceptance, not permission to migrate, deploy or release.

## Frozen verification stages

| Stage | Source | Actual result |
| --- | --- | --- |
| Complete Go build and real-Postgres suite | `2757cbde522ec17161fd507cabf87abe34fc817b` | Build and `go test -count 1 -p 1 -json ./...` exited 0; 85 test packages, 8,166 test/subtest passes, 38 opt-in/platform skips, 13 packages without tests; no failures or result-cache reuse |
| Final fixed-master integration | `330055247d5d6290747c9a2bfb37e2a16c18c396`, incorporating `5129429b13f527cd0e7736f1cb6c27d3d1ebd4ea` | Generate/build exited 0; 20 affected server packages, 6,073 test/subtest passes, 22 skips, no failures or result-cache reuse |
| SDK condition deduplication | `1b0130948ca30435f4eea95a33280e818dc30aa6` | SDK and server builds exited 0; four SDK packages, 190 tests (166 in wire) passed with vet enabled; 21 URL/View/HTML consumer cases passed |
| Canonical market index and skill digest cleanup | Final cleanup on top of the preceding source | Server build and three affected-package suites exited 0; 837 test/subtest passes, no failures/skips/result-cache reuse |
| Web full suite | `330055247d5d6290747c9a2bfb37e2a16c18c396` | Typecheck, 500 files / 4,560 tests, production build exited 0; no lint script exists |
| Android | Source tree `20324d8618a749747d325daba1867adc26ec6509`, present at `2757cbde522ec17161fd507cabf87abe34fc817b` | Debug/Release APKs, full unit-test graph, lint and lintRelease exited 0; 3,241 tests, no failures/errors/skips. No Android source changed in the final master integration; incremental Gradle tasks were reused |
| Scripts and Apple static checks | Final extension-name cleanup | 64 suites / 709 tests, no failed suites, nine explicit platform/tool skips; Apple naming guard: nine checks passed |
| Runtime integrations and TypeScript SDK | Unchanged integrations plus final generated SDK | 183 tests across seven integrations; 59 SDK tests, compiler rejection of 13 negative fixtures and Node portable-runtime check passed |
| Migration rehearsal | Isolated disposable databases/homes | Six typed SQL fixtures and historical-schema/new-Store acceptance passed; no live database migration |

Every Go command used its own new private HOME, state and temporary roots. Build/module caches and the isolated parent database configuration were the only reusable inputs. Parent runners/logs remained private; the test child used the fixture-compatible umask. Raw logs, previous failures, interruption records and sandboxes remain outside the repository.

## Failure history and row-lock comparison

A previous full run failed when the first interaction-history insert timed out before a row-lock fixture acquired its row. The requested comparison ran the single test three times on clean master `94706050392775af03d68cefd2497b830c8849df`, then three times on the naming branch `b110e06114232c8186baaf2f23483d7b007a1840`, each in a new sandbox/owned database with result caching disabled. All six actually ran and passed: the failure was not reproduced, not classified as a master failure, and no baseline waiver was requested. The complete 2757 suite passed it too. The 300ms budget, Store and assertion were not relaxed.

The subsequent localization ratchet failure was fixed by lowering its already reduced count from 6 to 5, never increasing a baseline. Other preliminary failures included an SDK equivalent duplicate condition caught by vet, untranslated new performance-test comments, outer-run timeouts and runner isolation mistakes. Their logs remain failures; later successful verification is recorded separately.

## Public naming and identities

Case-insensitive source, filename and archive-member scans have no retired product-name matches. Extension test settings use `PERSONA_EXTENSIONS_*` and the mock uses `MOCK_EXTENSIONS`; canonical marketplace directories read only `persona-extensions.json`, while explicit JSON URLs keep their exact address. Extension-skill digests use `.persona-extension-skills`. Public collaboration text is checked separately.

Actual registered Bundle IDs, applicationId, App Groups, iCloud containers, Keychain groups and APNs topics remain configuration-driven and unchanged. No retired-brand registration literal is embedded in the source. Health Connect's `ViewPermissionUsageActivity` remains as the platform permission entry, not a brand alias. Mochi stays `persona`; Visor is `persona-visor`.

## Native and operational limits

Apple changes are text/static only on Linux. Mac verification must compile XcodeGen/project settings, all app/extension targets, Info.plist principal classes, Swift symbols, AppIntents reflection metadata, watchOS 10 Shared and signed entitlements. Xcode 26.0 is not verified. Android shell pictures are Robolectric component renders, not emulator/real-device upgrade proof. Web light/dark pictures use synthetic sanitized mock data and were actually viewed.

The implementation and maintenance procedure are [the cutover plan](../../operations/persona-cutover.md) and `scripts/migrate/persona-cutover.py`. The tool defaults to plan-only; application requires approval, stopped writers and verified backups. Service names, roots, environment names, MCP registration/permissions, typed persisted IDs, client coordination, cryptographic-domain impact and whole-fleet rollback are documented. No migration, deployment or release was performed. An earlier fixture mistakenly installed a test extension; the user chose to keep it unchanged, and no cleanup or subsequent configuration/secret change was made.
