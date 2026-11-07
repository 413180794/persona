# CLAUDE.md

Persona: a personal iPhone + Apple Watch assistant. The **Persona** tab is an AI assistant with access to personal and work data (a Claude in a dedicated directory on the hub machine, with an always-on main chat).
The knowledge base `~/.persona/data` distills chats, people, groups and projects from everywhere for it and for the AI twin.
The **Agents** tab drives Claude Code / Codex in herdr on each computer through a chat UI on the phone (live streaming of what they do,
answering questions / approving plans / granting tools from the phone), and lets Claude read and write local iPhone data (contacts, calendar, reminders, photos, health, location…) and mail
(Gmail / any IMAP+SMTP). The hub of the Go server `personad` runs on an always-on machine and reaches the other computers over SSH. Distributed through TestFlight.

**The specifics of this dev machine and this person (machines, addresses, accounts, personal working style) are in `CLAUDE.local.md`** (not committed); without it, set things up from `docs/operations/config.md` first.

**Names: Persona only**: the product is Persona, the server is `personad`, the connector is `persona-link`, the runtime directory is `~/.persona`, and the build config is `~/.config/persona/config.json`. Only `PERSONA_*` environment names and the `persona` MCP server are supported; no previous-brand aliases, fallback reads, or dual writes. Preserve the actual registered bundle / application IDs, App Groups, iCloud containers and keychain access groups from private configuration. Mochi is `persona`, Visor is `persona-visor`. Source merges do not authorize online migration, deployment or release: finish Mac verification and obtain explicit approval for the coordinated cutover in `docs/operations/persona-cutover.md`; see `docs/operations/rename.md` for the contract.

## Conventions (mandatory)

### Workflow

- **Every change goes issue → claim → PR → self-merge** (steps in "Development workflow" below): features and fixes all start from an issue.
  Every change in the repository (code, docs, config) is committed on its own branch / worktree, pushed only to that branch, and goes through a PR linked to the issue. Never commit or push directly to the main branch.
  This replaces the old flow of committing and pushing straight to the main branch in `CLAUDE.local.md` and elsewhere; automatic commits and releases still follow their conventions, but must be merged through a PR first.
- **Claim an issue before you start** (several people and their Agents work at once; two people have fixed the same bug): before starting, look for open issues / PRs on the same thing.
  If someone else has claimed it and updated it in the last 24 hours, leave it alone (adding information is fine). A claim is an issue comment; whoever wrote the comment (their GitHub account) owns the work, no assigning.
  A PR that links no issue is closed by an Action. Workflow, comment templates and gh commands: `docs/operations/collaboration.md`.
- **Mark feedback that comes from TestFlight** (requested 2026-11-04): issues created from crash reports or testers' text / screenshot feedback always get the `testflight` label; without permission to add labels, prefix the title with `[TestFlight]`.
  Scrub the body (no tester identity, device / crash identifiers, paths, memory addresses or raw attachments) and end it with a dedupe marker; see `docs/operations/collaboration.md` ("TestFlight feedback").
- **English for code, docs and collaboration**: see "Language" below.
- **Write down lessons as you go**: pitfalls and odd behavior go into `docs/lessons/<topic>.md` on the spot (index: `docs/lessons/README.md`), design trade-offs into `docs/<topic>.md`.
  The cross-module ones people hit most also get one line in "Most common pitfalls" in this file. This file holds only conventions and the most essential things; don't pile details into it. Keep this rule itself too.
  One-off lane plans, verification / acceptance reports and audit snapshots go to `docs/archive/` when done, not the top level or the docs index. When you move a file, update the paths in the docs at the same time, and list new top-level docs in the docs index below (`scripts/test/test_doc_paths.py` checks both).
- **Quality bar**: design and test to the standard of a top App Store app. Every screen gets checked in a simulator screenshot (dark / light), watch screens too; compiling is not enough.

### Open source

This repository is about to go public (requested 2026-11-06); from now on everyone and every Agent maintains it to the standard of a public repository.

- **Write everything as if anyone can read it**: code, docs, comments, test fixtures, file names, commit messages, and the titles, descriptions and comments of issues and PRs.
- **No internal names**: names of internal company services, internal repositories, internal tools or intranet access setups never appear. Capabilities that need them are plugged in as extensions;
  the main repository keeps only generic extension points and generic names (such as "Lark connector", "WeChat connector", "usage extension", "private extension repository").
- **No internal information**: intranet domains, private IPs, information about internal staff, accounts, tokens and keys are never written (see also "Privacy and secrets" below).
- **Automated patrols check this**: anything found in the code gets a PR that removes it; in an issue title, the issue gets a comment and is closed, and deleted after it has been closed for 24 hours;
  in a PR's title, description or code, the PR gets a warning comment and can only merge once it is fixed.
- **Don't write it in the first place**: the edit history of issues and comments is public; changing the text does not make it disappear.

### Language

Prepare the repository for a public, international audience (requested 2026-11-06).

- **English for everything people read in the repository**: code comments, docs (`docs/`, module READMEs, this file), commit titles and bodies, PR titles and bodies
  (squash merges turn the PR title and body into the commit on master), and issues and their comments. Write natural, concise English, not word-for-word translation.
- **The README is bilingual**: `README.md` (English) and `README.zh-CN.md` (Chinese) have the same content and a language switch at the top, and are changed together in the same PR.
  Their pictures come from `scripts/assets/make_readme_assets.py` in both languages (`banner-light.svg` / `banner-light.zh-CN.svg`).
  Docs under `docs/` exist in English only.
- **User-facing text always goes through each platform's localization resources** (`docs/protocol/i18n.md`): Apple `apple/Shared/Localization/Localizable.xcstrings`,
  web `web/src/i18n/locales/<lang>/`, Android `res/values*/strings.xml`, hub `server/internal/i18n/locales/<lang>/`. Never hard-code text a user sees in code.
  Every platform ships at least `en`, `zh-Hans` and `ja`; every new string is added in all three at once (the checks in `docs/protocol/i18n.md` catch gaps).
  Adding a language requires only another localization resource file, not changes to screens or language-specific branches.
- Chinese stays only where it is data that must match something real (text matched against hub or third-party output, prompts sent to a model, test fixtures),
  marked `// i18n-ignore` where the checks ask for it.

### Privacy and secrets

- **Make it configurable; never hard-code anything personal**: machine names, domains, accounts, mail addresses, paths, launchers, internal company tools and personal preferences all go through config
  (build / deploy: `~/.config/persona/config.json`; runtime: the hub's `~/.persona/hub.json`; see `docs/operations/config.md`). No specific person's values appear in code, scripts,
  UI text, tests, docs or this file (intranet domains / IPs, host names, internal project names, personal accounts and key IDs all count); the UI says "the hub machine" and "your computers". This machine's specifics go into `CLAUDE.local.md`.
- **Direct connection over the internet** (requested 2026-10-30): the phone must be able to reach the hub directly over the internet, probing for and preferring IPv6; VPNs are the user's own setup. The internet uses TLS, the LAN stays compatible with existing clients; see `docs/operations/network.md`.
- **Never commit intranet or personal information** (requested 2026-10-28; checked automatically by `scripts/privacy_check.py`, see `docs/operations/testing.md` ("Privacy scan")): code, test data, docs, screenshots and commit messages never contain intranet domains / IPs / URLs,
  host names, internal project / repository names, real company or personal data (messages, document links, people's names), personal accounts, mail addresses, or key / project IDs. Examples always use placeholders
  (`<hub address>`, `me@example.com`); real values go only into config or `CLAUDE.local.md`. Public third-party product names (Lark, WeChat, GitHub, etc.) are fine; names of internal services, internal tools and intranet access setups are forbidden by "Open source" above.
- **Secrets never go into the repository**: the hub token and the database password live only in `~/.persona/hub.json` on the hub machine (0600); mail credentials are AES-GCM encrypted in Postgres, and the key file stays out of the database.

### Server and data

- **All server state lives in the Postgres container** `persona-postgres` on the hub machine (`docs/server/server.md`); don't write JSON state files into `~/.persona/` any more.
  The exception is the knowledge base `~/.persona/data`: all data ends up in `.persona`, files first, with the database as the index (`docs/assistant/knowledge.md`).
- **Prompts always live in `server/internal/prompts/`** (including MCP tool descriptions and parameter docs, under `mcp/`): one prompt per file (subdirectories by scenario), an Agent's several parts composed with templates, never `fmt` / `+` prompts in Go code; callers only prepare the data. When you change a prompt, update the golden tests and look at the diff; see `docs/assistant/prompts.md`.

### Product and UI

- **Don't build a second Lark**: the app collects information from everywhere and hands it to AI; it does not move Lark's feature pages over. From Lark it keeps only
  messages (into the Inbox) and in-app rendering of links (docs, single Moments posts); everything else exists only on the hub / AI side (search, reading as context); see `docs/extensions/feishu.md`.
  A narrow exception the user explicitly allowed (#392): the **Show messages on home** switch in Lark settings, off by default, which adds a **Messages** page for browsing all direct and group chats.
  It reuses the Inbox's accounts, chat page, outbox and sync, with no separate client, and does not bring back pages like contacts or workplace. See `docs/extensions/lark-migration.md`.
  The same goes for other data sources: don't build a set of browsing pages for one source.
- **Links open inside the app**: any link in a conversation (Lark docs, Moments, web pages…) opens in the current app, Telegram-style:
  a card rising from the bottom that fills the screen when opened (not half the screen); dragged down to 0.6 it no longer blocks working with the Agent (the chat scrolls and takes input as usual); dragging further closes it. Opening speed and smoothness come first, local storage is no concern: preloading, local caching,
  reusing WebViews and the like are all used. Text selected in a page can be sent with **AI edit** (the selection plus an edit instruction go to the Agent) or **Use as context** (attached as context to the next message);
  context travels in the message as XML tags, which clients parse to show the matching UI.
- **Never change how an existing screen looks directly**:
  a different visual style (component library, design language) is always a new **style** (**Settings › Appearance › Style**, `AppStyle`), a different color set a new **theme color** (`ThemeColor`).
  It only applies when selected; the default and every other option stay pixel-identical. Bug fixes and changes to a screen the user explicitly asked for don't count.
  On 2026-10-31 Android was rebuilt at the user's request into three styles (Glass by default, Brief, Board): pages are written once, and all differences live in the design system's components and shell; see `docs/android/android-design.md`.
- **The UI uses only the design system** (requested 2026-10-31: the design system is a constraint of the repository): font sizes, spacing, corner radii, sizes, animation, colors, icons, brand marks and styles all come from the design system;
  pages never hard-code numbers, system font sizes, SF Symbol names or hex colors. If something is missing, add a token first. See "Design system (iOS)" below; for Android, `docs/android/android-design.md`.
- **Button text never wraps**: button labels are always one line (`.lineLimit(1)`; when space is tight, `.minimumScaleFactor`, shorter text, or an icon).
  Horizontal rows of buttons such as key bars use icons or short words; what doesn't fit goes into a menu / popover, never squeezed onto two lines.

## Development workflow

People and Agents both follow this workflow. Everyone and their Agents use their own GitHub account; **whoever wrote the claim comment in the issue owns the work (the comment's author is the owner), no assigning**.
Claim details, comment templates and copy-paste gh commands: `docs/operations/collaboration.md`.

1. **Look for duplicates first**: `gh issue list --state all --search "<keywords>"` / `gh pr list --state all --search "<keywords>"`, for issues and PRs on the same thing
   (closed and merged ones too: don't redo what is already done).
2. **Claim an existing issue; create one only if there is none**: if you find an issue meaning the same thing (whoever created it), claim it (step 3; if someone else holds it, step 4) instead of creating another.
   Only when there really is none, `gh issue create`, saying what to do and why; features get the `enhancement` label, fixes `bug`.
3. **Claim before you start**: comment in the issue "Claim: what you are doing / Plan: how you will change it / ETA: how long"; an Agent adds a line "Agent: yes (<machine id> / <branch or session>)".
   The machine id is the id the config gives the machine, never the host name.
4. **Already claimed by someone else** (the latest Claim / Take over comment is not yours and has not been abandoned):
   - If the owner has updated it in the last 24 hours (a new comment in the issue, a push to or comment on the linked PR), leave it alone; you can add information, reproductions or leads below.
   - With no update for more than 24 hours it counts as abandoned: comment "Take over:" with the reason, then carry on. If you are still working on it yourself, post a "Progress:" comment to renew.
5. **Do it**: change things on your own branch / worktree, with the issue number in the branch name (such as `fix/123-xxx`). If you have to give up, comment "Abandon:" in the issue saying how far you got.
6. **Open a PR**: one PR per issue, title and body in English (they become the commit on master), with `Closes #<number>` in the body (`Fixes #<number>` for fixes, `Refs #<number>` to link without closing).
   GitHub closes the issue on merge. A PR whose description links no issue gets a comment and is closed by an Action (`.github/workflows/pr-needs-issue.yml`), and reopens automatically once the link is added.
7. **Merge it yourself**: no review from others is needed; **you own the quality**: run the tests in "Build and verify", look at screenshots for UI changes, and normally wait for CI to pass, then
   `scripts/hooks/merge-pr.sh <number>` (it first scans the PR's changed files, commits, title and body with this machine's private values, and only then squash-merges and deletes the branch; CI does not have those values, #3696). Make sure the issue is closed.
   **When the CI environment is broken, passing local verification is enough to merge** (for example runners, billing / quota or service outages that keep CI from running normally; **CI that never gets scheduled**, with jobs queued for a long time or a runner backlog, also counts, requested 2026-11-04). Don't wait for CI to recover or ask for an exemption each time;
   state the cause, the local verification commands and results, and what was not run in the PR, and never report CI that did not run as passed. Failures in the code or tests themselves must still be fixed; they can't be skipped as an environment problem.

## Build and verify

- Server: `./scripts/test/test-server.sh` (runs all tests against the real Postgres on the hub machine); deploy with `./scripts/deploy/deploy.sh [machine id…]` (all machines in the config by default).
- iOS: `./scripts/app/sim.sh [-openSession <machine>/<session>]` builds, installs to the simulator in the config (booted slimmed through `sim-boot.sh`) and pairs with the hub automatically; screenshots with `xcrun simctl io <udid> screenshot`.
  Before using xcodegen / Xcode directly, run `python3 scripts/persona_config.py xcconfig` (generates the bundle id and team).
- Android: `./scripts/app/android.sh [install|apk|test|lint]` (`docs/android/android.md`).
- Web: `cd web && npm test && npm run build`; deploy with `./scripts/deploy/deploy.sh web` (`docs/web/web.md`).
- Releases: `docs/operations/release.md`. **Every TestFlight release goes to internal and external testers together** (requested 2026-10-30): after `release.sh` uploads, run
  `python3 scripts/release/asc_testflight.py wait 45 <build number>` (into the internal group), then `python3 scripts/release/asc_testflight.py external`
  (adds it to the external group, submits it for Beta review and prints the public link); only when both are done, tell the user the build number and review status.

## Most common pitfalls

The full list is in `docs/lessons/` (read the file for the area you touch first); only the cross-module ones that cost half a day are kept here.

- **Persona-only cutover**: remove previous-brand aliases and self-recursive wrappers; Mochi is `persona` and Visor is `persona-visor`, with all extension wire code regenerated centrally. Live migration still requires explicit approval (`docs/operations/persona-cutover.md`, `docs/lessons/rename.md`).
- Several Agents work in this repository at once: each uses its own simulator and mock hub port. Check the port with `lsof` before starting a mock, and stop a mock by its port (never `pkill -f mock_hub.py`).
  When the main checkout has someone else's uncommitted changes, work in `git worktree add --detach /tmp/x origin/master`; stage only your own files, bring the branch up to date by merging origin/master (preferred over a rebase, `docs/operations/collaboration.md`) and resolve only the conflict hunks,
  and before pushing, `git diff origin/master...HEAD --stat` must list only your files. Look at every worktree before picking a database migration number. (`workflow.md`)
- Source anonymization preserves wire/serialization identifiers, public verification pins, JSON escaping, and security-test grammar; removing a presentation codec never removes transport encryption. Verify contextual exceptions, retained trees, and behavior separately. (`lessons/source-anonymization.md`)
- Scripts that wait for background jobs / logs always have a timeout, and exit with a one-line verdict on success, failure and timeout alike.
- Before a manual `xcodebuild`, run `eval "$(python3 scripts/persona_config.py env)"` (`DEVELOPER_DIR`); otherwise an old Xcode is used and reports a pile of errors that look like someone else broke the code. (`testing.md`)
- **Always create / boot simulators through `scripts/app/sim-boot.sh`** (`SIM=$(scripts/app/sim-boot.sh "<name>")`, `--profile default|watch|live-activity|stock` by purpose, slimmed with simslim by default, about 11 GB → 2.3 GB). Never `simctl create/boot` directly or let xcodebuild boot one with `-destination name=…`. It does not reboot a simulator that is already running (it may be someone else's); `--reslim` only your own. (`testing.md` ("Simulators"))
- If a test fails only on a slimmed simulator (`sim-boot.sh` prints slim), rerun it on a fresh stock one with `--profile stock` before treating it as a code problem. Conversely, a test asserting "no system prompt appeared" (`PasswordPrompt.assertNone`) passing on a slim simulator proves nothing; run it on a stock one. Never turn on `simslim watch` (it changes every simulator on the machine). (`testing.md` ("Simulator slimming"))
- UI tests always launch with `app.launchPinned()`: a pinned style (Liquid Glass by default; `TEST_RUNNER_PERSONA_UI_STYLE=beautiful` switches the whole suite), same layout as `launchPersona()` (no previous-brand launch entry is kept). (`testing.md`)
- A DEBUG launch argument's name must not equal any Swift identifier (type member, stored property), or the strings check in `release.sh` blocks TestFlight; name new switches `open…` / `show…` and add them to the list in release.sh. (`ios.md`)
- Release's `SWIFT_REFLECTION_METADATA_LEVEL` must be `all`: App Intents pass values to `@Parameter` by the property names in reflection metadata. Without the names every Shortcuts / Siri parameter is empty and the app crashes, which never shows in Debug or the simulator (`lessons/ios.md` ("Running Ask Persona from Shortcuts")).
- A keychain group shared by the app and its extensions (the Dynamic Island's avatar, the push private key) that is missing silently falls back to defaults, yet always works in the simulator: release ipas must pass `scripts/release/entitlements.py check`, and Info.plist names the group with `$(DEVELOPMENT_TEAM).`, not `$(AppIdentifierPrefix)`. (`release.md`)
- Everything a Live Activity draws goes into the ContentState (the hub's pushes carry it too), and images too big for it go into files in the App Group container: the Live Activity extension can't read or write the keychain on a phone, though it can in the simulator (`live-activity.md` ("Why build 103's avatar pack was still wrong")).
- Don't write frequently changing state to UserDefaults: every write recomputes every `@AppStorage` view in the app (2026-11-01: written on every push, the Agents list reordered and its content jumped while scrolling); put it in `StateCache`. On the Mac, split views write once per frame while a window is dragged, so heavy views use `@StoredValue` (recomputes only for its own key, #1337). (`ios.md`, `lessons/macos.md`)
- App targets default to MainActor isolation: closures the system calls back on another queue are written `{ @Sendable … in`, or they crash at runtime; plain data types in `apple/Shared` are marked `nonisolated`. (`ios.md`)
- Polling and read receipts only happen while the page is in front: in the new shell's two pages and in containers that keep views attached, hidden pages never get `onDisappear`; use `.tabActiveTask` and `.onTabVisibility`. Message subscriptions, dictation and timers don't. (`ios.md`)
- The Apple apps' UI has three languages, Chinese, English and Japanese (`docs/apple/ios.md` ("UI languages")): new UI text goes through `String(localized:)` / `Text`; after a build, `scripts/i18n/apple_l10n.py sync <DerivedData>` adds it to the catalog, then fill in en and ja; `check` catches gaps. Chinese that is not UI (sent to an Agent, matched against hub text) is marked `// i18n-ignore`. Tests run in Chinese (the scheme pins zh-Hans).
- Verify with screenshots through DEBUG launch arguments + `simctl io screenshot`, not computer-use; for UI changes you must actually see the screenshot (if you can't, the gateway dropped the image; see `testing.md`).
- A constantly running `TimelineView(.animation)` in a shared page can relayout the whole window every frame on the Mac (measured: the breathing ring of the "Needs you" dot kept 14% of a core busy on an idle Agents page): on the Mac, hand persistent animations to Core Animation (`MacBreathingRing`). (`lessons/macos.md`)
- **The hub is only a communication center**: new business logic (endpoints, tables, model calls) goes into the managers (`server/internal/assistant`, `devices`, `extmgr`), and the hub only forwards. `hub/architecture_guard_test.go` pins the hub's routes, dependencies and Store; when it fails, follow its hint and move the logic into a manager. Only things that truly belong to the hub (connections, pairing, admin, push, relaying) get added to its lists. (`architecture.md` ("The hub is only a communication center"))
- The hub's in-memory state can be wiped by someone else's redeploy at any time: flows that span requests must be self-contained or persisted. (`server.md`)
- The hub's port belongs to systemd's `personad.socket` (socket activation): before switching back to an old binary that doesn't understand `LISTEN_FDS`, run `PERSONA_HUB_SOCKET=0 scripts/deploy/hub-systemd.sh`, or it can't get the port and restarts over and over. After `stop personad` the next connection starts it again; to stop it, stop `personad.socket` too. (`hub-restart.md`)
- On Linux / macOS, `personad hub run` is the entry point and the hub itself is its slot (`hub slot`): switch versions blue-green with `hub switch`, no more exec or restart. The entry point itself is replaced only when the service restarts; to go back to the hub listening by itself, use `PERSONA_HUB_EDGE=0`. (`hub-restart.md` ("Blue-green switch"))
- When trusting by source address: connections coming into a container through a port mapping often all appear as the bridge gateway (a private address, even when they come from the internet), and a reverse proxy on the same machine makes internet requests look like loopback. Exclude the gateway with `containerGateway`, use forwarding headers only to tighten, and turn on `security.proxy_protocol` for same-machine TCP forwarding / passthrough. (`server.md` ("Admin UI"), `network.md` ("PROXY protocol"))
- Claude Code's config directory isn't always `~/.claude` (`CLAUDE_CONFIG_DIR`), and the same session id can exist in two directories; when looking up by id, always take the most recently modified. (`agents.md`)
- Verify changes to transcript parsing or the sync protocol against real transcripts; when the item structure changes, bump the app's `ChatCache` directory version. (`transcript.md`)
- **The minimum supported compiler is still Xcode 26.0 (Swift 6.2)**: keep the compatibility targets for iOS, Mac, watch and extensions; building with a newer Xcode does not count as verifying 26.0.
  APIs only in newer SDKs (`isDynamicIslandLimitedInWidth`, `toolbarVerticalEdge`, `ShowsSnippetView` / `view:` all bit us) are wrapped in `#if compiler(>=6.4)` (Xcode 26.4–26.6 is Swift 6.3 + the iOS 26 SDK, so 6.3 can't be the test, #453), with the old behavior in `#else`.
  `#available` / `@available` only cover runtime; a symbol missing at compile time is still an error. **When an Xcode 26.0 environment is available, build with it before merging; without one, a real build is not required and does not block merging or releasing**:
  build and test with the toolchain you have, note the actual Xcode version and "26.0 not verified" in the PR, no need to ask for an exemption each time (`DEVELOPER_DIR`: see `persona_config.py env`).
  Raising the minimum is still a product decision: ask the user first, and update this item and `docs/lessons/ios.md` together. (`ios.md` ("Minimum supported compiler"))
- The watch app's minimum is watchOS 10 (many older watches stop at 10.6; on 2026-11-02 the user's watch couldn't install it), and all of `Shared/` is compiled into the watch app: it can only use APIs that exist in watchOS 10,
  with `#available` fallbacks for newer ones (`Color.mix` is watchOS 11; use `Color.blended`).

## Design system (iOS)

Every number, color and icon in the UI comes from the design system; pages contain no literals. Details and each step's values: `docs/apple/ios.md` ("Design language").

| What you need | What to use (`apple/Shared/Design`, also compiled for the watch / widgets) |
| --- | --- |
| Font size, emphasis, line height | `Theme.Font` levels (display / title / heading / body / secondary / caption / sectionHeader / label / code) and emphasis tokens (`labelStrong`, `secondaryMedium`…), `.lineHeight(_:)` |
| Spacing | `Theme.Space`: a 4pt grid xxs 2 · xs 4 · sm 6 · s 8 · m 12 · l 16 · xl 24 · xxl 32; the page's side margin `page` (16; search fields, filter chips, section headers and cards all align to it) |
| Corner radius | `Theme.Radius`: badge 6 · control 10 · card 14 · bubble 20 · panel 22 |
| Size | `Theme.Size` + `.scaledFrame(…)`: icon frames, dots, avatars and tap targets scale with the font size (up to 1.6×); lines and bars of 4pt or less use a plain `frame` |
| Animation | `Theme.Motion`: `standard` / `state` (0.25 s), `layout` (0.3 s), `blink` (the default avatar blinking); content unfolding in a chat uses `reveal` (0.28 s) / `conceal` (0.2 s), through `.unfolding` |
| Color | `Theme`'s role colors (`accent`, `danger`, `positive`, `caution`, `ai`, the avatar's main color `persona`…), `Theme.Fill`, `Theme.Diff`, `Theme.Hue`; base colors `Palette`; Agent state colors `AgentState.tone` (`StatusPalette`) |
| Icons, logos | `Theme.Icon` (one SF Symbol per meaning), `Theme.Brand` (official logos, used as is; always drawn through `BrandMark` / `BrandTile` / `BrandBadge`, same size, same center) |
| Style, theme color | `StyleSpec.current`: one value combining the style (`ComponentSpec`) × the theme color (`ColorSpec`); one file per style in `Shared/Design/Styles/` (with its own colors); the avatar's colors are a theme color option, **Match avatar** (`docs/apple/persona.md`) |

- **Components and recipes** live in `apple/Persona/DesignSystem`: surfaces `.surface(…)`, buttons `.themedButton(…)`, pages `.themedPage()` / `.themedGroupedPage()`, plain list rows `.themedRow()`;
  `Chip`, `CountBadge`, `EmptyStateView`, `Monogram`, `Loadable`, `SecretField` (tokens, API keys, secrets: no "Save password?" prompt on iOS; never write `SecureField` directly). Never write `glassEffect` / `.glass` directly, and never assemble a lookalike small component inside a page.
- **Add what's missing first**: add a named token or component to the design system, then use it. A new style is a `ComponentSpec` plus an `AppStyle` case; a new theme color is a `ColorSpec` plus a `ThemeColor` case.
  Everything else only reads `StyleSpec.current`, never `switch AppStyle` / `ThemeColor`.
  A different visual style can only be a new style (see "Never change how an existing screen looks directly" above); BeautifulUI components are used only under the BeautifulUI style, and the library's code is not changed inside Persona.
  How: the view picks one of two by `AppStyle.current.usesBeautifulUI`, keeping the original drawing as it is (`plain`). Whatever fits into a design system component goes there,
  so pages don't decide for themselves: under the BeautifulUI style, `Chip`, `.card()`, `EmptyStateView` and `LoadableContent` draw `BUIPill`, a BUI card, `BUIEmptyState` and `LoadingState`.
- **Checks** (PersonaTests scan the source): `TypeScaleTests` (font levels, emphasis, vertical spacing on the grid), `IconVocabularyTests` (icon registry),
  `TokenRatchetTests` (each file's hard-coded padding, horizontal spacing, corner radii, hex colors, animations and fixed sizes can only go down; after reducing them, delete `TokenBaseline.json` and rerun to record the new baseline),
  `StyleSpecTests` (contrast for every style × every theme color, dark and light). Never add exemptions or raise a baseline to make a test pass.
- **Global**: scroll indicators are hidden (RootView's `.scrollIndicators(.hidden)`; UIKit scroll views turn theirs off themselves); font sizes follow Dynamic Type and the small / standard / large setting (`TextSize`).
- **Android** has its own design system (`android/core/designsystem`, three styles, `docs/android/android-design.md`): pages use only its components and tokens, never hard-coded numbers, colors or font sizes.
  `scripts/app/android_token_baseline.py` keeps each module's baseline of hard-coded values (it can only go down; `docs/android/android.md` ("Design token adoption baseline")). For UI changes, take screenshots with
  `./scripts/app/android.sh shots` (a dedicated emulator + mock hub) and look at all three styles × dark and light.
- **Logic layer**: pages that "load → show → edit → save" use MVVM: an `@Observable` `XxxModel` next to the page, dependencies passed in from `Deps`, unit tests with `FakeHub`.
  Where events are many (the chat page, paging), pull the pure computation out first (`ChatLines`, `AgentListOrder`). See `docs/apple/ios.md` ("Choosing the logic layer").

## Layout

| Path | Contents |
| --- | --- |
| `server/` | The Go server `personad` (four modes: hub / node / hook / mcp); see `docs/server/server.md` |
| `apple/` | XcodeGen project: Persona's iOS / macOS / watch apps; targets / schemes `Persona`, `PersonaMac` (`Mac/`, `docs/apple/mac.md`), `PersonaVision` (`Vision/`, `docs/apple/vision.md`), `PersonaWatch`; the old `Persona` schemes and project entry are kept; shared code in `Shared/`, unit tests |
| `apple/PersonaLink/` | The Mac menu bar app **Persona Link** (a different thing from the Mac app `PersonaMac`; a standalone SwiftPM package, not in `project.yml`, built by `scripts/app/mac-link.sh`): it bundles persona-link (`server/cmd/persona-link`) and personad, the Mac's node service (connecting to the hub, or letting the hub connect to it); see `docs/operations/link.md` |
| `android/` | Kotlin + Compose, multi-module: `:app` wires things up, `core` holds protocol / networking / storage / lists / design system, `feature` holds pages, `platform` holds notifications and phone tools; some extension modules are not wired into the app yet; see `docs/android/android.md` |
| `web/` | The web app (Vite + React + Tailwind, Beautiful UI's design language), served by the hub itself; see `docs/web/web.md` |
| `integrations/` | Agent runtime integrations: `runtime-sdk/` is the general SDK (protocol client, daemon listen mode, test tools, an echo example), `dsh/` is the Web plugin for DSH (DeepSeek Harness), `pi/` the extension and companion process for pi / pi-web, `opencode/` the opencode plugin, `paseo/` the bridge for the Paseo engine (run by the node itself); see `docs/integrations/runtimes.md`, `docs/integrations/daemons.md`, `docs/integrations/dsh.md`, `docs/integrations/pi.md`, `docs/integrations/opencode.md`, `docs/integrations/paseo.md` |
| `scripts/` | Deploying (`deploy/deploy.sh`), TestFlight releases (`release/release.sh`, `release/asc_*.py`) and the public personad (`release/publish.sh`, `deploy/install.sh`), reading the config (`persona_config.py`; the old `persona_config.py` is a compatibility symlink), the mock hub for screenshots (`mock/mock_hub.py`) |
| `config.example.json` | Template for the build / deploy config (`docs/operations/config.md`) |
| `docs/` | Design docs; index below |
| `deploy/docker/` | Docker deployment of the hub: `compose.yaml`, `.env.example`, the image's entry script (the image is the `Dockerfile` at the repository root, built and pushed to ghcr by `.github/workflows/hub-image.yml`); see `docs/operations/docker.md` |
| `site/` | Website + privacy policy + terms of service (static, hosted on Cloudflare Workers; Google's OAuth consent screen requires a privacy policy). Screenshots only show demo data taken in demo mode (`-demo.on YES`), one dark and one light, in `site/public/img/` |

## Docs index

### Overview

| Doc | Contents |
| --- | --- |
| `docs/README.md` | Module navigation, document lifecycle and required production-media policy |
| `docs/operations/collaboration.md` | Working with several people / Agents: check for duplicates before starting, claim in the issue before working, 24 hours without updates counts as abandoned (renew / take over / abandon), PRs must link an issue (an Action closes those that don't), comment templates and gh commands |
| `docs/protocol/i18n.md` | Internationalization: three languages (zh-Hans / en / ja), fallback rules, glossary (zh / en / ja), each platform's mechanism, how the protocol carries the language, future work |
| `docs/operations/getting-started.md` | "Getting started" for people who don't use a terminal: the three parts (hub, your computers, phone / web) and models, choosing a starting point by what you have, installing the hub with one command, pairing by QR code, adding computers in the web app, connecting a model from a preset, what the main chat's cards mean and how to fix them, what isn't possible yet |
| `docs/assistant/onboarding.md` | New-user onboarding (#287): the mental model and shortest path for people who can't use a command line, `personad hub setup`, adding computers from the phone (pending confirmation, keys sent only to confirmed ones), three cases of connecting a model, the main chat readiness API and degradation, error codes → plain words, presets, do / not yet / decided, PR order and Mac work items, tests with real people |
| `docs/operations/persona-cutover.md` | Approval-gated service, data, environment, MCP and Visor cutover with backups, verification and rollback; no online execution before approval |
| `docs/operations/config.md` | Configuration: the build / deploy config (config.json), the hub's runtime config (hub.json), which values must never be written into code |
| `docs/operations/rename.md` | Naming contracts, registered identity preservation, canonical source/runtime names and regression checks; current rolling-upgrade guarantees are documented separately |
| `docs/server/architecture.md` | Overall architecture, data flow, why it is designed this way (herdr + hooks + transcript) |
| `docs/protocol/protocol.md` | The hub's HTTP / WebSocket API, hub↔node RPC, events and the interaction model (the front-end / back-end contract) |
| `docs/operations/network.md` | Direct connection over the internet: TLS, key pinning, inbound IPv6, pairing, source checks and rate limits; iOS / Mac reaching the hub through an SSH tunnel (the fallback when a direct connection fails) |

### Server and deployment

| Doc | Contents |
| --- | --- |
| `docs/server/server.md` | personad's modes, configuration, deployment, operations |
| `docs/operations/site.md` | The website: Worker hosting, the mainland China entry through Alibaba Cloud Hong Kong (nginx forwarding to the Worker's origin domain), the entry secret and client addresses, certificates, why not a CDN / Function Compute / load balancer, switching to split routing after moving away from the registrar |
| `docs/operations/docker.md` | The hub's two deployment methods (native binary / Docker), running the hub in Docker (NAS): compose, image tags, PUID / PGID, environment variables, migration |
| `docs/operations/hub-restart.md` | Hub restarts clients don't notice: socket activation with zero gap, planned downtime, event sequence numbers and resuming (`event_seq`), warm start, the resident node hosting the assistant's chats, silent client reconnects and idempotent replay, the repeated-restart verification script |
| `docs/operations/hub-update.md` | Hub versions, update checks and automatic updates (design): skills moved out of the binary, a single binary, a signed version manifest, automatic releases and staged rollout, download → verify → wait for idle → restart → roll back, what the app shows |
| `docs/operations/push-relay.md` | Push relay (end-to-end encrypted): a hub without an APNs key pushes through the publisher's `personad relay`, with the body encrypted to the phone's public key and decrypted by the notification service extension; who knows what, abuse prevention, API, deployment |
| `docs/extensions/extensions.md` | Extensions (formerly "plugins", #1843): terms (extension / extension point / contribution), naming and compatibility (old names keep working, and until when), three sources (built-in / hub.json / installed), extension points, the manifest `persona-extension.json` (fields, the schema subset for config forms, permissions), install / upgrade / roll back / uninstall and `personad extension`, extension markets and automatic upgrades, on-demand activation and crash degradation, how config and secrets reach an extension, the `/api/v1/extensions` API, the process extension protocol and conformance tests; the contract between voice extensions and the app; the built-in GPT-Live voice extension `personad voice` |
| `docs/extensions/extension-messaging.md` | The `messaging` extension point (#2138): one generic set of native messaging components in the app, with conversations, messages and actions provided by an extension; manifest, capabilities, API, sending, composer modes, outbox / drafts / accounts, live events and the Inbox, version compatibility, how Lark (the Lark extension) maps onto it |
| `docs/operations/ports.md` | Port forwarding: the node finds the ports an Agent listens on, the app recognizes localhost links in answers, the phone listens locally and forwards over WebSocket through the hub to that machine; trade-offs and security boundaries |
| `docs/assistant/prompts.md` | Prompts: the layout of `server/internal/prompts/`, what each prompt is for, template rules, how to change / add one, how to update the golden tests |
| `docs/server/machines.md` | Machine monitoring: OS detection, how CPU / memory / network / disk are measured (which network interfaces count), measured overhead, storing / downsampling / pruning 24 hours of history, the app's **Machines** page (Agents and monitoring together) and machine details |
| `docs/server/usage-windows.md` | Subscription usage windows (#2432): `rate_limits` from Codex rollouts, `rate_limits` saved by Claude's status line (opt-in), why not account APIs with OAuth / login tokens; node collection, the hub merging one account by reset time, account-pool gateways (relays, `usage.sources`) as a source and why that is not an extension, cadence and countdown math, the thin bar and popover at the bottom of the web sidebar, the iOS Machines page and the bottom of the Mac's split column |
| `docs/operations/admin.md` | The hub's admin UI (`/admin`): access scope `admin.access` (loopback / lan / cidrs / any) and source checks, admin password / sessions / CSRF / rate limits, editing hub.json, pages and API, what to choose when deploying |
| `docs/server/mesh.md` | Linking hubs (single primary): pairing with a link code, links, the primary and its term (joining a group, becoming primary, primary unreachable), members handing app requests and computers to the primary, credential handover, deduplication and route choice for a computer with several routes, kind service (the hub dials a connector), migrating from ssh, admin API and CLI, how to verify |
| `docs/operations/link.md` | The Persona Link connector (command `persona-link`; the old name persona-link still works) and the node service: one command / one app installs a resident node + connector; active mode (the computer's link to the hub) and passive mode (the hub connects to the computer: listening, pairing codes, API, rate limits, security boundaries); the Mac menu bar app **Persona Link**; `persona-link install / status / mode / node / logs / uninstall` for headless Linux |
| `docs/operations/windows.md` | The node on Windows (Orca engine): current state and what isn't supported, `persona-link install` as a scheduled task started at login (a windowless daemon, restarted on crash), how the phone connects when the hub runs in Docker Desktop on the same machine, running it by hand, AF_UNIX sockets, Orca's named pipes, PEB process scanning and "in the foreground", Git Bash, verification notes |
| `docs/operations/persona-link-install.md` | A guide for others in the repository: building and installing Persona Link on a Mac, pairing, upgrading, uninstalling, FAQ |
| `docs/operations/node-update.md` | Updating the resident node through Persona Link: the update capability, the first local upgrade, chunked verification, idle scheduling, restart and rollback, confirming it is back online |

### Agents, runtimes and the terminal

| Doc | Contents |
| --- | --- |
| `docs/integrations/claude-integration.md` | How to start / drive Claude in herdr, and measured findings on answering AskUserQuestion / ExitPlanMode through hooks |
| `docs/integrations/script-cli.md` | The script commands `personad ask` (blocks until answered on the phone, exit codes 0/1/2/3/4) and `personad notify` (best effort / `--strict`): over node.sock, reusing the question / permission cards and `POST /api/v1/notify` delivery, attributed to the session of the pane they run in, terminal fallback, resuming across restarts |
| `docs/integrations/codex.md` | Codex support (and Codex forks): how it is detected, how the rollout format maps to a chat, question and approval cards (reading the rollout and the screen, answering in each UI state), research on moving to app-server and approval hooks, what isn't supported yet |
| `docs/integrations/codex-app-server.md` | Research on connecting several Codex app-server instances: taking in several existing app-servers and threads on one machine, the desktop app keeps working, reconnects and authorization boundaries |
| `docs/integrations/runtimes.md` | The spec for Agent runtime integrations: how they differ from hosts, how several machines are aggregated, what a driver implements, content and interaction conventions, naming, SDK, acceptance checklist |
| `docs/integrations/dsh.md` | DSH (DeepSeek Harness) support: the first runtime; installing the DSH Web plugin, event mapping, how permissions / questions reach the phone, limits, end-to-end verification |
| `docs/integrations/daemons.md` | Daemons: runtimes that are resident services themselves and can't reach the local runtime.sock (another user, a container, another machine); the node connects to their WebSocket endpoint per `daemons.json` (token + `persona-runtime.v1`): spec, config, keeping names and session ids from colliding, isolation, the SDK's listen mode, acceptance |
| `docs/integrations/pi.md` | pi and pi-web support: pi in herdr is a herdr session (the pane belongs to herdr, content and control to the extension), pi-web's go through pi-web; extension + companion process, where it continues after going offline, install, config, mapping, end-to-end verification |
| `docs/integrations/opencode.md` | opencode support: opencode in herdr is a herdr session (pane and state belong to herdr, chat and control to the plugin in the server process), how a plugin loaded per directory connects only once, reading chats from opencode.db and why only finished messages are pushed, permission cards and questions, terminal rows of other agents, install, config, mapping, pitfalls, end-to-end verification |
| `docs/protocol/agent-states.md` | Unified Agent states (needs you / working / done, unseen / seen) and how they map to herdr; the space / slot model |
| `docs/integrations/agent-goal.md` | Agent goals (Claude Code's `/goal`, Codex's thread goal): the two formats, how the node reads them (incrementally, the whole file, together with `/loop`), `Session.goal`, the goal bar at the top of the chat page and the goal-reached notification |
| `docs/operations/auth-relay.md` | Auth relay: when an Agent stops at an authorization link its own command printed, an extension that names that kind of link (extension point `auth_relay`, permission `auth_relay`, off by default) completes it with the account it is signed in with and the Agent goes on; everything else still goes on a card. Which kinds can be completed, the checks, the extension contract, security trade-offs |
| `docs/assistant/agent-work.md` | An Agent's work state (working / blocked on a named precondition / done): hub triage after a turn stops (rules + a cheap model), automatic "continue" on transient errors, wake-ups when a deploy / CI / another Agent is due, cards for what you need to decide; safety boundaries (pushing code, deploying, sending messages and touching other people's MRs only ever ask you), not fighting manual actions, the evaluation API |
| `docs/integrations/engines.md` | Engines (herdr / Herdr App / Orca / Paseo): one engine per machine, how to switch, what happens after switching, what each engine can do, what to check when one can't be chosen |
| `docs/integrations/paseo.md` | The Paseo engine: the bridge (runtime) the node runs, how Paseo is found, session ids and transcripts, mapping cards / new sessions / takeover, limits, end-to-end verification, troubleshooting |
| `docs/integrations/terminal.md` | The real terminal in the terminal tab: herdr's observe stream (no attach, pane size unchanged), resuming, line wrapping, why iOS uses its own VT subset; the web app's **Full UI** (the node runs herdr's own client in a PTY; the default, can switch back to a single pane), focus sync (herdr's own sidebar and Persona's sidebar move together, one client per computer) |
| `docs/integrations/launchers.md` | Launchers and **Settings › Model providers**: automatic sync / hiding from ~/.zshrc, Persona-managed connections (keys encrypted on the hub), direct API vs launchers, API |
| `docs/server/launcher-health.md` | Launcher health checks and automatic repair: testing every launcher on every computer periodically and when it comes online, also recording failed Agent starts, error classes, routing work around broken ones, upgrading the CLI / repairing the Agent, blocking and the API for the board |
| `docs/server/agent-sandbox.md` | The agent sandbox on macOS (off by default, a file in the data directory turns it on): which agent starts run under `sandbox-exec`, the rules (everything allowed but the data directory's secrets, the same list as the node's path guard), when a start is not wrapped (the agent's own sandbox, cannot tell), what it does not stop (Docker, `launcher.env`, your own terminals), side effects (setuid programs such as `ps` and `sudo`), the switch and rolling back |
| `docs/assistant/model-config.md` | Unified model configuration: choosing the provider, model and options per purpose, inheritance, persistence, taking effect on the next run, the boundary between tasks and plugins |

### Assistant, data sources and knowledge base

| Doc | Contents |
| --- | --- |
| `docs/assistant/assistant.md` | The assistant in the **Persona** tab (main chat, knowledge tools): ask_user, scheduled tasks / follow-ups, Agents and Lark tools, live snapshots, warm-up, memory, waking the phone in the background |
| `docs/assistant/managers.md` | Managers (peers, each a complete assistant, no supervisor): the default manager, ownership, roster, installation codes, revocation, rollout steps |
| `docs/assistant/assistant-views.md` | Rich UI in the main chat (#2017): a unified View, fallback, actions and live cards, the HTML sandbox's security boundaries, validation, self-repair and staged acceptance |
| `docs/assistant/unified-entry.md` | A unified main-chat entry (#930): dispatching, watching and answering all tasks in the assistant; three layers (chat stream / task dock / needs you), plan cards and three auto-start levels, tasks = to-dos, chat activity, hooks, rollout steps; prototype `docs/assistant/prototypes/unified-entry.html`. The Agents page stays unchanged |
| `docs/assistant/voice.md` | Voice mode (#3489): GPT-Live (the built-in voice extension) is the fast layer for immediate answers and speech; the chat open when voice starts (main chat or any Agent) is the slow layer that does the work; extension / app protocol 2 (target binding, run / ask / link frames), tracking a turn to read the full answer, the user's speaking threshold, answering questions by voice but approvals only by tapping cards, pausing upstream while waiting for an Agent, configuring upstream in the app (keys encrypted on the hub), the iOS call bar and audio pipeline, work split and acceptance |
| `docs/assistant/todos.md` | To-dos: recording work and dividing it among Agents, watching their progress in to-dos, grouping the Agents page by to-do; whole-group suspension, following derived sessions, herdr markers, derived states and API; closing an Agent when the task is done, resuming the session with the next message |
| `docs/assistant/health.md` | Health space (inspired by ChatGPT Health, #2691): health chats in their own `~/.persona/health` directory with their own CLAUDE.md and memory, the `space: "health"` API, chat tools never cross spaces, the web **Health** page, what is excluded from the first version and what comes later |
| `docs/assistant/knowledge.md` | Knowledge base `~/.persona/data`: full chat archives, compressed context, people / groups / projects, tagging, how Persona / the AI twin / Agents share it |
| `docs/extensions/connectors.md` | Connectors: current structure (interfaces, three ways to run, gates, Inbox, knowledge base, settings pages, adding one), v2 design (capability manifest, unified deltas, a versioned protocol, structured Inbox attributes, identity hints, web authorization login, writes only through tools and gates), migration order V1–V7 (V2 and V7 excluded) and decisions |
| `docs/protocol/connector-protocol.md` | Connector protocol v2: one JSON object per line between subprocess / third-party connectors and the hub; handshake and manifest, each method's arguments and results, program calls to the hub's `secrets.*` and `changed`, error codes, mandatory rules, version rules, conformance tests |
| `docs/assistant/inbox.md` | Inbox: Lark and mail in one list, one row per thing, AI triage, marking done, handing to AI; the AI twin (triage → judge → an Agent handles it and drafts a reply, send with a tap) |
| `docs/extensions/mail.md` | Mail: IMAP/SMTP, Gmail (Google account login, OAuth2 + XOAUTH2), Lark mail (the larkoffice.com / feishu.cn server sets, choosing the first successful login when adding), credential storage |
| `docs/extensions/lark-migration.md` | Authorized scope of the Lark migration: the optional Messages page shares existing Inbox capabilities |
| `docs/extensions/feishu.md` | Lark's role in Persona (not a second Lark): what is kept, Lark through an extension (`/api/v1/lark/…` is provided by the extension), API quick reference for AI |
| `docs/extensions/lark-attachments.md` | Sending images and files in iOS Lark chats (#410): picking, previewing, sending, confirming |
| `docs/extensions/lark-encryption.md` | Encrypted Lark messages (#689): reading and display (`olk` projections, public-facing display when decryption fails), sending encrypted text after unlocking the phone, deployment and authorization, unsupported versions, retries and local storage, verification |
| `docs/extensions/channels.md` | Channels: inventory and a unified abstraction for every way to talk to Persona (app, WeChat, Lark bot, Lark /agent, voice); channel interfaces and capabilities, the hub's unified pipeline (into the chat, tracking a turn, returning answers, text / card questions, deduplication, source tags), the `channel` extension point and protocol, compatibility migration, `/api/v1/channels`, **Settings › Extensions › Channels** |
| `docs/extensions/bots.md` | Lark bots: direct messages to the bot are the Persona main chat; answers return to Lark, questions / approvals use Lark cards, pushes sync with the main chat, driving Agents on your computers from Lark; the platform's long connection, pairing, owner-only access, card JSON 2.0 pitfalls |
| `docs/extensions/wechat.md` | WeChat bot (built-in extension `personad-wechat`, Tencent iLink): QR-code binding, direct messages talk to the main chat, answers return to WeChat, questions / approvals use text round trips; the extension only talks to WeChat (`channel` extension point), everything else uses the hub's channel pipeline; comparison with the Lark bot |
| `docs/extensions/lark-agent.md` | Sending `/agent …` yourself in Lark (the Lark extension does it, the hub provides chat creation and knowledge-context APIs): collecting context from the chat / thread / quotes / docs / knowledge base, starting an Agent, replying as you |
| `docs/apple/phone-tools.md` | Phone data tools (phone_*), permissions, writes needing confirmation, iOS limits |
| `docs/extensions/computer-use.md` | Computer control: built-in extension `cua` (off by default, enabled per machine), the assistant looking at windows, clicking and taking screenshots through hub → node → Cua Driver (`computer_state` / `computer_run`), capability discovery, sessions, screenshots, write logs and confirmation; one-click installation and live screen (screen sharing through the hub tunnel) design |
| `docs/assistant/office.md` | Generating Word / PowerPoint / Excel (`personad office`, M9): Markdown to docx / pptx, JSON / CSV to xlsx (formula values computed at write time), check reading the structure back; why it lives in personad, supported features, QuickLook limits |
| `docs/apple/recording.md` | Phone and Apple Watch recordings into the knowledge base: chunks, local transcription, reliable return, text-only sync (idempotency, backfill, deletion), retention and verification boundaries |
| `docs/apple/meta-glasses.md` | Meta smart glasses: the glasses' microphone (Bluetooth HFP) as a recording input, photos through the Meta DAT SDK configured as an option, why not layer it onto rolling recording, Xcode 26.4 and review risks, applications on Meta's side |

### Clients

| Doc | Contents |
| --- | --- |
| `docs/protocol/rolling-upgrade-compatibility.md` | Requested Chinese contract matrix for bidirectional rolling upgrades: exact aliases, capability negotiation, stable cryptographic and persistence identities, isolated binary/client verification and remaining native boundaries |
| `docs/apple/ios.md` | The iOS / watchOS app: information architecture, design language, phone data tools |
| `docs/apple/ios-state.md` | App state and redraws: how a push reaches the screen, the cost of one push, root causes, staged fixes; standards and probes for accuracy / timeliness (`ScreenProbe`) |
| `docs/apple/intents.md` | Siri / Shortcuts (App Intents): ask Claude, status, approve the first pending request |
| `docs/apple/siri.md` | Deep Siri integration: ask Persona (the assistant's main chat), send work to a personal Agent, unread items, Control Center / Action button, Spotlight indexing |
| `docs/apple/carplay.md` | CarPlay long voice conversations: full-duplex in the car over the hub's voice extension via `VoiceConversation(surface: .carPlay)`, audio session and mic ownership, Apple's limits, how to turn on the entitlement (`app.carplay_voice`), simulator checks |
| `docs/apple/live-activity.md` | Lock Screen / Dynamic Island Live Activities (Fleet): what is shown, sharing Dynamic Island with other apps (limited width / minimal mode), ContentState, the hub's liveactivity pushes and push-to-start, simulator verification |
| `docs/apple/shell.md` | The new shell's state model: `ShellState` as the single source of which page is in front, synchronizing segments / bars / routes, pages not in front, acceptance standards and tests for consistency and timeliness |
| `docs/apple/persona.md` | Avatars: names, portraits, the top bar's dynamic state dot, packaging colors (the hub's `~/.persona/personas`), connecting color families to design tokens, the script for adding avatars (Codex image generation) |
| `docs/protocol/multi-hub.md` | One device connected to several hub groups: pairing registry and migration, grouping by mesh, one connection and routing per group, group-scoped keys (the default group unchanged), merged display and group filters, group routing for pushes / Live Activities / watch / Siri |
| `docs/apple/mac.md` | macOS app: sharing logic and content views with iOS, the Mac shell (`apple/Mac/`), isolation and compatibility for iOS-only APIs, feature parity checklist |
| `docs/apple/mac-design.md` | Mac design language: each column's header is its title bar, density, reading width, saying each thing once; rules for sidebars / lists / details / cards / buttons / selections / empty states, design drafts and their generation |
| `docs/apple/vision.md` | visionOS app (`PersonaVision`): sharing logic and content views with iOS, the visionOS shell (`apple/Vision/`), mapping to iOS, simulator builds and screenshots |
| `docs/android/android.md` | Android app: module responsibilities and dependencies, entry wiring, resident-connection notifications, multi-hub (grouping, group suffixes on session keys, route switches within a group), build / test / mock-hub screenshot commands |
| `docs/android/android-design.md` | Android's three styles (Glass / Brief / Board), design tokens, components, page-writing rules, Glass fallback |
| `docs/web/web.md` | Web app: hub hosting, login exchanged for a device token, pages (Inbox, terminal / files / Git side panels, links, linked-hub labels), connections and gap recovery, details, the gap from mobile |
| `docs/web/workspace-board.md` | Web workspace board (#1646): off by default, switch in Settings, each card binds a code directory on a computer and its Agents; state, persistence, permissions and refresh, upgrade compatibility |

### Testing and releases

| Doc | Contents |
| --- | --- |
| `docs/operations/testing.md` | Test commands, simulator screenshot checklist, the simulator entry `scripts/app/sim-boot.sh` (simslim slimming, profiles by purpose) |
| `docs/operations/runtime-cli.md` | The DEBUG build's command channel (`scripts/app/personactl.py`, compatible with the old `personactl.py`): reading state, waiting for the screen to settle, screenshots, protocol and safety constraints |
| `docs/operations/release.md` | Signing and the TestFlight release workflow |

### Plans and lessons

| Doc | Contents |
| --- | --- |
| `docs/plans/plan-orca-support.md` | Plan: Orca support (in progress): first separate hosts (herdr, Orca) from Agents, extract host interfaces, then add the Orca source; each step's scope, Orca's capabilities, risks |
| `docs/plans/plan-tmux-support.md` | Plan: tmux engine (#1180, undecided): finding several servers, mapping session / window / pane to space / slot, screen reads and control-mode live streams, input only through paste-buffer (copy mode, synchronized-input pitfalls), state sources, botmux deduplication, measured findings, tests, decisions for the maintainer |
| `docs/plans/plan-connectors.md` | Plan: connectors (GitHub and iMessage first): the optional-capability Connector interface, built-in / external-process / node-local forms, Lark and mail migration steps, privacy and decisions |
| `docs/plans/plan-lark-cli.md` | Plan: using the official lark-cli when there is no Lark gateway: capability comparison and fallback, coexistence of the two sources, making it a connector, gates recognizing lark-cli, Inbox intake, phased plan and decisions made |
| `docs/plans/plan-discord.md` | Plan: Discord connector (#2828): only your own bot is allowed (no self-bots), server channels visible to the bot + messages explicitly sent to Persona, extension form, login, knowledge base (no people records), Inbox, tools, network, Discord developer-policy constraints, steps and decisions |
| `docs/plans/plan-x.md` | Plan: X (Twitter) extension (#5363): official API only (no scraping or cookies), the owner's own developer app with OAuth 1.0a tokens, pay-per-use cost and the daily budget, following, feed and short retention, a deterministic organized feed + assistant skill, mention Inbox, read-only tools, X's rules (deletion, no training, no profiling or off-X matching, so nothing in the knowledge base yet), steps and open questions |
| `docs/plans/plan-ego-browser.md` | Plan: ego lite browser plugin (#1304): the Cua plugin pattern (off by default, per-Mac enablement, installation validation, first-run setup), division between the assistant's `browser_*` tools and Agents' bundled skill, browser viewing (live screen / periodic screenshots / CDP casting), cookie import sources, storage and security, writing, decisions needed |
| `docs/plans/plan-antigravity.md` | Plan: Antigravity CLI support (#281, step 0 research, undecided): integration points (TUI, `agy -p`, `agy acp`, hooks, MCP), session storage, herdr Agent or runtime, headless capability table, account and terms risks, decisions for the maintainer |
| `docs/plans/plan-extensions.md` | Plan: extension system (#1101, renamed in #1843): key trade-offs (installed only on the hub machine, permissions are not a sandbox, form subset), gaps between implementation and design, phased progress, migrating voice and connector v2 |
| `docs/lessons/` | Lessons (pitfalls) by topic: collaboration, Agents, assistant, server, transcripts, Lark and mail, terminal, iOS, Android, macOS, testing; index `docs/lessons/README.md` |
| `docs/archive/` | Completed lane plans, real-data verification, stage-status tables and acceptance records (live updates interrupting actions, messaging pipeline, session-read verification, Android P0), referenced only as evidence, not updated with the code; put new one-off records here when done |
