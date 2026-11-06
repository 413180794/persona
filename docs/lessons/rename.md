# Lessons: renaming the product and keeping compatible identities

Lessons from the Jarvis → Persona migration on 2026-11-03. The entry point and the list of what is kept are in [compatibility migration](../rename.md); this page does not replace each lane's actual build, on-device upgrade or release acceptance records.

## Freeze the commit first, then tell three kinds of names apart

- **Display names**: brand titles, buttons and default copy that Persona generates itself. These can be updated as the product requires.
- **Source names**: directories, files, pure helper types, the Go module, Xcode targets, private npm package names. These can be migrated, but every real read path and build reference has to be updated with them.
- **System / wire / stored identities**: Bundle ID, module, Siri / Control types, ActivityAttributes, serialized values, service units, cache keys and update package IDs. These must not change just because they contain the old brand.

Multiple worktrees share remote refs; every `git show origin/master:…` re-resolves a base that may have moved. Record the full SHA first, then read source and lists at that SHA; record the source commit of mechanical path mappings too. Old-directory files and module imports that others merged in later have to be rechecked after the final rebase; the first audit is not the final integration result.

Keep historical SHAs, source line numbers from that time, logs and acceptance numbers. A historical reference to an old path is not the default entry point for new installs; use the migration table when looking up the current implementation. Updating docs does not prove that the runtime migration, rollback or on-device upgrade succeeded.

## Renaming a target is not renaming the module

New Apple targets / projects use the Persona name, but `PRODUCT_MODULE_NAME` keeps the original module identity. Check the actual notification extension's `NSExtensionPrincipalClass` and the compiled AppIntents metadata: source that compiles does not mean the system can still load `JarvisNotify.NotificationService` or find the old Shortcuts types.

`JarvisActivityAttributes` is also the `attributes-type` for APNs push-to-start, not a plain Swift struct name you can change at will. Renaming it separately in the hub and the App breaks addressing for real pushes, and starting it manually in the simulator does not prove that path still works. Widget kinds, App Groups, keychain groups and signing profiles are likewise kept per their real contracts.

An old scheme alias does not replace the old `.xcodeproj` entry point; old scripts may hard-code both the project path and the scheme. The new project is `Persona.xcodeproj`, and the old project path is a symlink to it; old and new schemes all point at the new targets. `-only-testing:` uses the new test target, while `@testable import Jarvis` still uses the old module. Verify `PersonaLink`'s Swift target and the existing menu bar helper / process paths separately.

To change Info.plist copy, change the XcodeGen source `apple/project.yml` or the matching resource catalog; do not edit the generated project or build products. Keep source-language entries when editing a catalog; after archiving, check the App's / extensions' actual purpose strings. Seeing Persona in the source does not mean the system prompt has been updated.

## Parsers must also recognize old-brand input

If a parser for source titles or session headers only accepts `Jarvis`, then once the new version generates a `Persona` prefix, old special messages may turn into plain chat text. Accept `Jarvis|Persona` on input, change only the titles Persona controls on output, and test the new and old formats as well as plain text with no brand prefix.

Session names users chose, message content, contact names and external bot names are not product copy. Do not rewrite this data with a global replace; even in demo fixtures, only change the default brand fields Persona clearly controls.

URL schemes likewise need **registration / receiving** kept apart from **generation**: a migrating client has to accept both `persona://` and `jarvis://`, and keep generating the old `jarvis://` for installed clients. Check Apple registration and parser, Android intent-filter and routing, Web / admin input, and record pass or not-accepted for each entry point separately.

## Source package names do not change persisted names

After Android moved to `persona.android`, the default fully qualified class names used by kotlinx.serialization change too. Explicitly keep `@SerialName`, `AppTab.Jarvis` and existing rawValues; the OS must still find the old Activity / alias, receiver, HubService, notification PendingIntents and WorkManager Workers. Compiling the new source alone does not prove old notifications and queued work survive the upgrade.

A runtime's private npm `name` is maintained separately from its update package ID. DSH / pi's `jarvis-dsh-runtime` / `jarvis-pi-runtime` appear in the update manifest, the on-disk directory and the loader at the same time; changing only one side installs the new version as a second plugin or stops the old loader from updating. Package names and visible copy can be migrated; the update identity and the `jarvis-runtime.v1` wire subprotocol must not be blindly replaced.

## Service identity also decides the runtime directory

When a deploy script recovers the root directory, it has to target hub / node / link specifically; it must not treat the first service on the machine that has an environment variable as the root for every service. If an old unit exists but has no root Environment, keep using the original default storage; a neighboring service's custom root must not take it over. Cover systemd's `Environment=` reset, sorted drop-ins, XDG_CONFIG_HOME, and SameFile for old and new default aliases.

`EnvironmentFile` / `UnsetEnvironment` are not settings you can quietly ignore: the parser does not read external credentials and cannot guess the final environment, so it must require an explicit root. Resolving the runtime root and fetching credentials when the CLI actually makes a request are two separate jobs; "the former doesn't read credentials" does not mean the latter needs none. #773's implementation and regression tests are in `scripts/persona_identity.py` / `scripts/test/test_persona_script_identity.py`; the full rules are in [recovering the runtime directory per service](../rename.md#部署脚本按服务恢复运行目录).

## Mechanical mappings need format and boundary checks too

#786 makes `scripts/test/test_apple_rename.py` reject recreating the old Apple source directories and `mac/Sources/JarvisLink`, while keeping the old project symlink; when checking new files, don't only look at type names or the current target. Android's static guard also has to stay in sync with the merged shared link detection for hand-filled links, so the source isn't changed while the check still asserts old text, and a broad brand scan doesn't flag deliberately kept old OS identities as leftovers.

JSON paths in the token baseline may be encoded as `\/`; plain string replacement misses them. Map keys by the real source files, and keep the original format, every metric value and threshold; don't regenerate the baseline, let alone raise it.

Doc checks come in two kinds: references to current source / commands must point at the real new entry points; historical snapshots, modules a plan has not landed yet, and compatibility identities must not be altered just to get the path scan to zero. Relative Markdown links also have to be resolved against the document's own directory, not assumed to be relative to the repository root.
