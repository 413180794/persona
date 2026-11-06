# Persona naming cutover

The repository uses **Persona** exclusively. The #277 cutover has no previous-brand command aliases, environment fallbacks, directory aliases, dual writes, URL schemes, or compatibility build targets. It changes runtime contracts, not just display text. **Merging this code does not authorize a deployment or release.** Follow the approval-gated [operational cutover](persona-cutover.md) before starting the new binaries on an existing installation.

## Registered application identities remain unchanged

Keep the actual values in private configuration for `app.bundle_id`, `app.app_group`, `android.application_id`, iCloud containers, keychain access groups, and App Store / Google registration. Only the variables that carry them change. Examples in the repository are placeholders, not registration values. Do not register a new app, regenerate keys, or discard pairing credentials.

Provisioning profile display names and service labels are operational configuration, not registered app IDs. Make sure the configured signing profiles exist before archiving on a Mac. The old app's pending notifications, queued workers, saved Shortcuts and caches are not guaranteed to survive renamed class/type/storage identities; test and migrate them explicitly during the maintenance window.

<a id="源码位置构建入口与兼容名字"></a>

## Canonical source and build entry points

| Category | Canonical name |
| --- | --- |
| Go module | `github.com/nwverse/persona/server` |
| Hub / node / hook / MCP binary | `personad`, source `server/cmd/personad` |
| Connector / built-in program | `persona-link`, `personad-wechat` |
| Linux services | `personad.service`, `personad.socket`, `personad-node.service`, `persona-link.service` |
| macOS LaunchAgents | `com.persona.hub`, `com.persona.node` (custom connector jobs are inventoried separately) |
| Runtime root / build config | `~/.persona`, `~/.config/persona/config.json` |
| Config override / runtime variables | `PERSONA_CONFIG`, `PERSONA_*` |
| Config / simulator command tools | `scripts/persona_config.py`, `scripts/app/personactl.py`, `PERSONACTL_*` |
| Apple project / modules | `apple/Persona.xcodeproj`, `Persona`, `PersonaWidgets`, `PersonaNotify`, `PersonaLink` |
| Android source / build conventions | `persona.*`, `personaApplicationId`, `persona.android.application` |
| MCP server / tool prefix | `persona`, `mcp__persona__*`, `persona_settings`, `ask_persona` |
| URL scheme / assistant route | `persona://`, `/persona`, `/persona/<id>`, `/persona/new` |
| Message context / file tags | `persona-context`, `persona-file` |
| Header / runtime subprotocol | `X-Persona-*`, `persona-runtime.v1` |
| Default database / container names for new installs | `persona`, `persona_test`, `persona-postgres` |

The `/` front door still opens the main conversation. Previous short assistant URLs are not aliases; previously sent links stop working. The existing avatar data model and `personas/` directory remain. Mochi's built-in ID is `persona`; Visor's is **`persona-visor`**; Circuit's is `persona-circuit`. Visor records in configuration, persisted settings, installed extensions and device preferences require the one-time data migration, not a runtime fallback.

## Runtime root: one directory, no automatic migration

The runtime reads only `PERSONA_HOME`, lexically resolves an absolute path, `~/…`, or a path relative to HOME, and otherwise uses `~/.persona`. A query does not create files. Initialization creates that one directory and never creates a compatibility symlink or merges another store. An existing valid custom symlink may be used; a dangling or cyclic link is refused rather than repaired into an empty store. A custom root does not create default-directory aliases.

`PERSONA_CONFIG` selects the build/deploy config; otherwise only `~/.config/persona/config.json` is read. The config script emits only `PERSONA_*` build variables and uses the existing configured registration values unchanged. It does not export tokens or bridge environment names.

<a id="部署脚本按服务恢复运行目录"></a>

## Recover the runtime directory per service

`scripts/persona_identity.py --service hub|node|link` reads only the matching canonical service definition. It never borrows a colocated service's root when the selected role has a unit of its own. A `PERSONA_HOME` already present in the environment, including an empty value, takes precedence.

The resolver supports systemd unit resets, sorted drop-ins, `XDG_CONFIG_HOME`, `%h` / `%%`, external `EnvironmentFile` overrides and `UnsetEnvironment`. It extracts only `PERSONA_HOME`, never executes shell text and never returns other credentials. Missing required environment files fail closed. Auto mode rejects different independent roots; explicit role selection avoids ambiguity. `--prepare` creates only the selected directory. `--prefix` resolves the remote root remotely and exports only `PERSONA_HOME`, not the caller's local root.

## Verification boundaries

- Scan tracked file contents and names, including generated schema bundles.
- Run Go build and the full suite against a dedicated real Postgres test database using `PERSONA_TEST_DATABASE_URL`.
- Run the web typecheck, unit tests, production build and available source checks; explicitly report if no lint command is configured.
- Compile and unit-test Android with the available Linux SDK; keep the Health Connect `ViewPermissionUsageActivity` alias, which is a platform permission entry rather than a previous-brand alias.
- On this Linux lane, Apple changes are text-only. Validate plist/YAML/symbol references statically, then build the project, all apps/extensions, tests, AppIntents and signing artifacts on a Mac. Screenshots and real-device upgrade behavior are separate acceptance checks.
- Generate extension wire code centrally with `python3 scripts/gen/extension_schema.py`; `--check` must show no drift. Do not hand-edit generated Swift/Kotlin/Go/TypeScript.
- Rebase onto the latest master before the single PR is pushed, rename any newly merged previous-brand additions, rerun affected verification, and scan the merged `origin/master` again.

Historical [acceptance](rename-acceptance.md) and [audit](rename-pr-audit.md) records describe earlier stages, not authorization to run this incompatible cutover. Previous compatibility guarantees no longer apply to this version. Do not deploy, publish, restart live services, move live data or change live MCP configuration until the user approves the operational plan.
