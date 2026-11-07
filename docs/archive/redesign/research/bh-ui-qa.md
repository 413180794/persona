# Reference app design language, prototype review, and test acceptance (compared with Persona)

Research date: 2026-10-30. Read-only research; no builds, tests, or scripts run.

Conventions:
- Path prefixes: `BH:` = reference app repository root, `JV:` = Persona repository root. Evidence uses `prefix:relative-path:line`, or a document section name.
- **[Fact]** means directly verifiable in code/docs; **[Assessment]** means my inference or recommendation.
- Privacy: source hostnames, IPs, accounts, group names, internal project names, and reviewers are not copied; placeholders replace them. Performance figures are quoted only where the document explicitly identifies measurements, with sources. grep counts below are occurrence counts from this research, not performance data.

---

## 0. Bottom line (expanded below)

- What is worth borrowing from the reference app isn't its visual tokens (its `DesignSystem` is thin: 4 files, about 300 lines), but three things:
  ① a workflow of "clickable HTML prototype → review document → native implementation after user approval → acceptance record";
  ② consistent copy/icons for uncertain states such as "Awaiting confirmation / Needs verification", never faking success or silently resending anywhere in the flow;
  ③ staged initial setup: form, keychain, segmented connection progress, fingerprint checks at every hop, optional component installation. Every step says what it does and what's lost if skipped.
- Persona's tokens are more complete: styles × semantic colors, state palette, radii, spacing, font roles, motion. The problem is adoption: views use `Theme.Font` once and `Theme.Space` once. Redesign tokens should generate Swift and Kotlin from one JSON source, with scripts/tests guarding adoption and contrast.

---

## 1. Reference app design system [mostly facts]

### 1.1 Module boundaries and size

- Architecture assigns DesignSystem "colors, fonts, spacing, avatars, badges, loading/error components, common content blocks", and forbids network requests (`BH:reference-client/docs/ARCHITECTURE.md:45`, `:56`; dependency graph `:28-43`, Features → DesignSystem).
- In reality, 4 files total about 297 lines: `WorkspaceStyle.swift` (154), `HerdrStyle.swift` (61), `WorkspaceSheet.swift` (49), `ServiceStatusView.swift` (33) (`BH:Packages/<app-kit>/Sources/DesignSystem/`). **No font, spacing, or radius tokens**, contrary to documented responsibilities.
- [Fact, grep in this research, scope `BH:Packages/<app-kit>/Sources`]
  - Fixed `font(.system(size:` appears 567 times; text styles (`.headline`, `.subheadline`, etc.) 1781.
  - Literal `cornerRadius:` values by frequency: 10 (78), 12 (69), 7 (34), 8 (33), 9 (26), 6 (20), 5 (11), 16 (9): at least 8 levels.
  - Literal `Color(red:` 26 times; direct system `.orange` 69, `.red` 34.
  - `WorkspaceStyle.` referenced 830 times, `HerdrStyle.` 341.

### 1.2 Tokens

Two independent palettes selected by tab:

1. **WorkspaceStyle** (Lark-style blue-gray for Messages, Contacts, Workplace, Me), defined at `BH:.../DesignSystem/WorkspaceStyle.swift:4-34`:
   - fixed colors: `blue`, `green`, `red`;
   - light/dark pairs: `canvas`, `field`, `line`, `outgoing`; `surface` uses system background;
   - `adaptive(light:dark:)` turns hex values into appearance-following colors compatible with UIKit and AppKit.
2. **HerdrStyle** ("forest green + cream" for Agents), defined at `BH:.../DesignSystem/HerdrStyle.swift:4-17`:
   - fuller semantic palette: `canvas`, `surface`, `field`, `raised`, `line`, `outgoing`, `blue` (actually green), `green`, `ink`, `secondary`, `onAccent`, `orange`, `red`, all light/dark pairs;
   - comment: "Herdr's palette is independent of the Lark gateway workspace chrome" (`:3`).
- Tab-based switching (`BH:App/<app-entry>.swift:199-205`):
  - tint switches by tab: Agents uses `HerdrStyle.blue`, others `WorkspaceStyle.blue`;
  - Agents appearance doesn't follow the system: `@AppStorage("herdr.lightAppearance")`, dark by default (`:41`, `:203`);
  - appearance toggle in the host-panel toolbar (`BH:.../AgentsFeature/AgentFleetView.swift:345-348`).
- Fonts:
  - no tokens. Page chrome (top bar, tabs, rows) mostly uses fixed sizes, e.g. tab icons 22pt, labels 10pt (`BH:App/<app-entry>.swift:489`, `:498`), Me heading 23pt (`BH:.../ConnectionsFeature/ConnectionsView.swift:164`);
  - body/form text mostly uses styles such as `.subheadline`, `.footnote`, `.caption`;
  - [Assessment] fixed chrome sizes seem intentional to match Lark; the cost is no Dynamic Type there.
- Spacing and radii:
  - all literals. Common padding: 15, 16, 18, 20;
  - main panel radius: `herdrPanel` 14, buttons 12, rows 10 (`BH:.../HerdrStyle.swift:44-61`).
- Prototype tokens: three HTML prototypes each duplicate the palette in CSS variables, with inconsistent names.
  - `herdr-ui-review`: `--surface/--ink/--accent/--amber…`, dark via `.phone.dark`;
  - `herdr-onboarding`: `--bg/--surface2/--fg/--accent-ink…`;
  - `herdr-connection-progress`: `--green/--green-soft/--orange-soft…`.
  - See first rule of `BH:prototypes/*/style.css`. [Assessment] prototypes and native code share no token source; colors stay aligned manually.

### 1.3 Components

| Component | Location | Key points |
| --- | --- | --- |
| `WorkspaceAvatar` | `WorkspaceStyle.swift:36-54` | Hashes the name to choose one of 5 gradient circles, containing initials or an SF Symbol; `accessibilityHidden` |
| `WorkspaceStatus` | `:56-64` | 5pt dot plus 11pt secondary text, for states such as "Connected" |
| `WorkspaceIconButton` | `:66-75` | 40×40 hit area; requires `accessibilityLabel` |
| `WorkspaceChrome` + `.workspaceDetail()` | `:77-111` | Registers on detail entry, automatically hides the bottom bar, restores on exit |
| `.workspaceTabBar` / `AdaptiveWorkspaceTabs` | `:122-154` | From iOS 27.1, system handles vertical tabs (Duo outer screen, landscape sidebar); horizontal tabs use a custom Lark-style bottom bar |
| `.workspaceNavigationBarHidden()` | `:112-119` | Hides system navigation on root pages for a custom top bar |
| `HerdrPrimaryButtonStyle` | `HerdrStyle.swift:44-54` | Minimum 44pt height, radius 12, `onAccent` text; pressed opacity 0.75, disabled 0.4 |
| `.herdrPanel(cornerRadius:)` | `:56-61` | `field` background, 0.5pt `line` border, default radius 14 |
| `HerdrAgentMark` | `:34-42` | Claude session mark, orange asterisk |
| `ServiceStatusView` | `ServiceStatusView.swift:3-33` | Full-page state: icon, title, explanation, primary button |
| `WorkspaceSheet` | `WorkspaceSheet.swift:3-48` | Shared sheet header (58pt height, back, title, accessory slot, close), optionally system navigation style |

Some components remain scattered in Features rather than DesignSystem:

- `AgentNavigationBar` (`BH:.../AgentsFeature/AgentNavigationBar.swift:4-50`): system back button, custom principal title/subtitle, trailing actions.
- `AgentConnectionProgressView` (connection-path progress).
- `SSHKeyFormSection` and `SSHKeyErrorNotice` (`BH:.../SSHKeyFeature/SSHKeyLibraryView.swift:153-173`): grouped heading/panel and inline errors.
- `AgentListStatus` (`BH:.../AgentsFeature/AgentsView.swift:426-436`) nearly duplicates `WorkspaceStatus`.

[Assessment] extraction is incomplete: two implementations of "dot + status text"; generic form sections, inline errors, and verification lists remain in Features.

### 1.4 Navigation chrome (compact top bar, avatars, unified navigation)

- README describes "four Lark-style entry points—Messages, Contacts, Herdr, Me—with compact top bars, avatars, unified navigation" (`BH:README.md:11`). Acceptance says "unified blue-gray palette, compact top bars, custom bottom navigation; chat details hide bottom tabs" (`BH:reference-client/docs/VALIDATION.md`, "Lark style and reading interactions").
- **Root top bars** always hide system navigation and draw one row:
  - Messages (`BH:.../MessagingFeature/MessagingView.swift:342-393`): left 38pt account avatar with name (`.title3` semibold) and organization (`.caption`), all one button opening account/connection settings; accessibility reads both. Right search and "+" menu. Realtime disconnect adds `wifi.slash` (`:359`).
  - Filter strip (`:400-457`): left circular menu containing custom groups/tags; right gray capsule with three fixed scopes "Messages / Unread / Flagged", selected white background. Sticky row.
  - Herdr home (`BH:.../AgentsFeature/AgentFleetView.swift:123-145`): "Herdr" title, three trailing icon buttons (hosts/connections, notification center with unread count, new). `ViewThatFits` wraps to two rows if needed.
  - Me (`BH:.../ConnectionsFeature/ConnectionsView.swift:163-166`): 23pt title.
- **Details** use system navigation and explicitly keep system back; comment: "Keep the system back item so UIKit owns edge/content gestures and cancellation" (`BH:.../AgentNavigationBar.swift:5`). Host tests specifically ensure edge/content gesture delegates remain intact (`BH:Packages/<app-kit>/Tests/<app-kit-tests>/AgentNavigationBarTests.swift`).
- **Bottom bar**: five custom entries; selection uses filled icons and bold labels. Unread counts above 999 show "…", but accessibility reads the full count (`BH:App/<app-entry>.swift:473-503`; rules `BH:reference-client/docs/LARK_INBOX_UI_REVIEW.md:96`).
- **Demote host hierarchy**: after review, Herdr home became "tasks first". Hosts collapse to a "Hosts x/y connected" strip (`AgentFleetView.swift:310-325`); large host cards, connection details, keys, appearance move to a separate panel sheet (`:326-352`). Tapping tasks pushes the same NavigationStack; one back returns home (`BH:reference-client/docs/HERDR_CINDY_REVIEW.md:83-89`).
- [Comparison] Persona's Agent chat hides system navigation and uses custom `ChromeHeader` (collapses with scrolling), then `SwipeBack.swift` reenables `interactivePopGestureRecognizer` (`JV:reference-client/docs/apple/ios.md:131-139`). The reference app takes the other path: system back in details, protected by tests.

### 1.5 Accessibility (Dynamic Type and accessibility-size layouts)

- Layout changes at accessibility sizes (`dynamicTypeSize.isAccessibilitySize`): 28 grep hits, typical patterns:
  - horizontal segmented filters become vertical, swapping `AnyLayout(VStackLayout)` / `AnyLayout(HStackLayout)` and changing container radii (`AgentFleetView.swift:414-427`, `MessagingView.swift:424-443`);
  - hide leading decorative row icons (`AgentFleetView.swift:505-510`, `AgentsView.swift:157-159`);
  - relax line limits: titles from 2 to unlimited, paths from 1 to 3 (`AgentFleetView.swift:490`, `:513`);
  - top bars use `ViewThatFits` for one/two rows (`AgentFleetView.swift:124-135`);
  - README says confirmation buttons stay at the bottom so cancellation remains reachable at accessibility sizes; wide layouts place actions side by side when space permits (`BH:README.md:90`).
- Reduce Motion: connection-progress light and breathing animations stay at midpoint with `accessibilityReduceMotion` (`BH:.../AgentConnectionProgressView.swift:8`, `:26-27`, `:56`, `:68`). All three prototypes use `@media (prefers-reduced-motion: reduce)` to disable animations.
- Screen readers: icon buttons require labels (`WorkspaceIconButton`). Count badges read full numbers despite visual truncation. Connection node diagrams are hidden, exposing only one textual status line (`AgentConnectionProgressView.swift:37-40`).
- grep counts: `accessibilityLabel(` 329, `accessibilityIdentifier(` 187, `@ScaledMetric` 7, `ViewThatFits` 8.
- Documented limits: "Dynamic Type has adaptive layouts; real-device large text, real keyboards, edge gestures, and frame rate still need actual acceptance" (`BH:reference-client/docs/HERDR_CINDY_REVIEW.md:91`). Additional large-text verification only checked "all three primary buttons visible at maximum accessibility size", explicitly not full VoiceOver acceptance (`BH:reference-client/docs/LARK_INBOX_UI_REVIEW.md:73`).

### 1.6 Empty, error, loading, and "awaiting confirmation" states

| State | Approach | Evidence |
| --- | --- | --- |
| Full page: unconfigured / unreachable | `ServiceStatusView` (icon, title, explanation, primary button: "Configure connection", "Retry", "Go to Messages") | `BH:App/<app-entry>.swift:261`, `:272`; `MessagingView.swift:22`, `:91`; `AgentsView.swift:76` |
| Empty list | System `ContentUnavailableView` (98 grep hits). Copy differs by filter scope, often with an escape action such as "View all chats" | `AgentFleetView.swift:455-459`; no hosts: "Connect your development computer" + "Add your first computer" (`:63-66`) |
| Loading | Full-page `ProgressView("Connecting to Lark gateway…")`; local `ProgressView("Reading keychain…")`; button "Saving…" plus disabled | `MessagingView.swift:73`; `SSHKeyLibraryView.swift:41-42`; `SSHKeyImportView.swift:70-76` |
| Local error | Inline `Label(error, systemImage: "exclamationmark.circle/triangle")`, red/orange, followed by "Reload…"; only failed actions show an "Operation failed" alert | `ConnectionsView.swift:119-130`, `:188-190`; `SSHKeyLibraryView.swift:36-40`; `MessagingView.swift:222-239` |
| Cached | "Showing local chat history" or "Updating local chats…", with `internaldrive` | `MessagingView.swift:218-221` |
| Destructive confirmation | `confirmationDialog` explains consequence/scope ("Also deletes saved login credentials for this connection. Services and sessions on the computer continue running.") | `ConnectionsView.swift:170-183`; `SSHKeyDetailView.swift:124-129` lists computers using the key |
| **Awaiting confirmation / Needs verification** | Fixed vocabulary "…result awaiting confirmation", "Needs verification", "Verify … result". Fixed icons `clock.badge.questionmark`, `clock.badge.exclamationmark`, `exclamationmark.circle`, orange. Only read-only verification, **never automatic resend** | Orange "Needs verification" in settings (`ConnectionsView.swift:54`, `:91`, `:102`, `:113`); task "State needs verification" (`AgentFleetView.swift:518`); workspace "Operations to verify" ("Send result awaiting confirmation · chat name", etc., `AgentsView.swift:191-202`); "Verify session startup result", "Startup result checked on computer…" (`:203-218`); 10 queue states with fixed copy/icons (`AgentPromptQueueView.swift:168-187`); README: "Never automatically resend when send outcome is unknown" (`BH:README.md:37`) |

[Assessment] the reference app's strongest lesson: every uncertain outcome has a name, copy, icon, and read-only verification entry. The cost is long, engineering-heavy wording such as "Computer receipt awaiting confirmation", "Verification conclusion awaiting confirmation". Persona's user is its author; borrow the structure, shorten the copy.

### 1.7 Summary [Assessment]

- Visual tokens: weaker than Persona. No font, spacing, radius tokens; many literals; two palettes without a shared semantic layer.
- Components: system controls plus a few custom pieces scattered across Features.
- Strengths: reviewed information-architecture changes; consistent state/recovery language; concrete accessibility layout adaptations; explicit levels of what counts as passing.

---

## 2. Initial setup (connection config → private-key import → host fingerprint → progress → component install)

### 2.1 Reference app flow [Fact]

1. **Connection form** (**Me › Connect Herdr Host**, `BH:.../ConnectionsFeature/ConnectionsView.swift:280-328`), five sections, each "small heading + panel":
   - "Target host": name, address, port, username; authentication fixed to SSH private key.
   - "Connection path": "Connect through SSH jump host" toggle. **Explanation changes with the toggle**: on, "Target address is accessed from the jump host; 127.0.0.1 means the jump host itself; jump host must allow TCP forwarding"; off, "Device accesses target directly" (`:304`).
   - "Security": explanation only—verify host fingerprints separately, private keys in Keychain, retain existing credentials if no key selected (`:307-315`).
   - "SSH keys": choose from keychain; jump host can share or use an independent key (`:329-363`).
   - "Next: set up host components" notice: after SSH verification, confirm component installation or skip for terminal-only use (`:317-324`).
   - Inputs have specific screen-reader names ("Host address", "SSH port") and appropriate keyboard types (`:405-419`).
   - Messaging-service `http://` forms warn "HTTP transport is unencrypted; verify the target network and service configuration" (`:434-436`).
2. **Private-key import** (`BH:.../SSHKeyFeature/SSHKeyImportView.swift`):
   - segmented "Paste or type / Import file" (`:42-57`), system `PasteButton` (`:128-134`);
   - hides key content on focus loss, showing "Private key entered · content hidden" (`:142-149`); input `privacySensitive`;
   - clears in-memory key/passphrase on close (`:111-119`);
   - reads only first 1 MB of files, in cancellable background work (`:239-251`);
   - specific errors for public keys, non-OpenSSH format, missing BEGIN/END lines, literal `\n`, etc. (`BH:Packages/<app-kit>/Sources/HerdrKit/HerdrContracts/HostTransport.swift:60-75`);
   - key library (`SSHKeyLibraryView.swift`): empty "No saved keys" plus primary action (`:75-86`); rows show algorithm, fingerprint, and "Used by: host list" (`:88-124`);
   - details copy public keys; private-key copies stay in the local clipboard, expiring after 60 seconds (`SSHKeyDetailView.swift:185-207`).
3. **Segmented connection progress**:
   - stages: Prepare → Connect jump host → Authenticate jump host → Connect development host → Authenticate development host → SSH ready → Check components → Update components → Handshake → Read sessions → Ready (`BH:.../HerdrContracts/HostConnectionStage.swift:4-7`), each with fixed copy (`BH:.../AgentsFeature/AgentConnectionProgress.swift:18-32`);
   - two rows in the host card: small "Device → Jump host → Development host" node path (light moves along current connection segment, authenticating node breathes), textual status, "Details" button (`AgentConnectionProgressView.swift:5-73`);
   - details show stages/timestamps and "Cancel connection"; after completion "Enter workspace / Continue resolving connection" (`:75-107`);
   - prototype constraints: no fabricated percentages before completion; animations pause awaiting user confirmation; progress driven by real transport events, not generic `.connecting` (`BH:prototypes/herdr-connection-progress/README.md:17`, `:21`).
4. **Fingerprint verification per hop** (`BH:.../AgentsFeature/AgentsView.swift:135-150`):
   - distinguish "Verify SSH host fingerprint" from "SSH host key changed";
   - identify jump or target host and address;
   - selectable monospaced fingerprint;
   - "Compare with the host-provided fingerprint; SSH authentication is sent only after confirmation";
   - "Verified, trust and connect" / "Cancel". With a jump host, verify it before the target (`BH:README.md:156`);
   - messaging HTTPS certificates use the same page: hostname, subject, SHA-256 fingerprint, "Verified, trust this server certificate"; invalid certificates have no trust action (`MessagingView.swift:75-89`).
5. **Host-component install guidance** (`BH:.../AgentsFeature/AgentHostComponentView.swift`):
   - state-specific headings: "Let your phone understand your work.", "Prepare the host environment first.", "Host components are ready." (`:25-34`);
   - three benefits (`:56-60`);
   - dependency checklist, available/missing with explanations (`:70-87`);
   - fixed bottom actions: "Install and continue", "Recheck host environment", "Skip for now, terminal only" (`:170-194`); skipping reconfirms and explains lost capabilities (`:104-107`);
   - four install stages: Check → Upload → Install and verify → Ready; future steps say "Waiting for previous step" (`:138-168`);
   - "What will happen on my computer?" sheet explains paths, hooks, no new ports, preserved existing work (`:95-98`, `:210-219`).
6. **Error categories**:
   - SSH errors distinguish DNS, refused port, unreachable network, timeout, authentication, forwarding, fingerprint verification, missing jump-host credentials; each actionable (`HostTransport.swift:22-49`);
   - failures identify the hop (`hopFailed`);
   - README: network errors don't require reimporting private keys (`BH:README.md:72`).
7. This flow was **finalized in HTML first** (`BH:prototypes/herdr-onboarding/`: 01 New connection → 02 Install guidance → 03 Install/verify → 04 Start work, plus skip, old version, interrupted upload, missing dependencies), native only after user approval (`BH:prototypes/herdr-onboarding/README.md:1-38`).

### 2.2 Current Persona pairing [Fact]

- `WelcomeView`: App icon, slogan, four features, "Connect hub", then "Run `personad hub pair` on the hub machine to display a QR code" (`JV:ios/Persona/Views/Settings/SettingsView.swift:220-274`).
- `PairView`:
  - scans with `DataScannerViewController` and connects immediately on detection; "Manual entry" takes server address and Token (`:276-322`);
  - one failure message: "Connection failed: … Make sure the phone can reach this address (e.g. on the same network or VPN)." (`:333`).
- After pairing, the App exchanges the pairing token for a device-specific token and uses only that; hub stores only hashes (`JV:reference-client/docs/protocol/protocol.md:8-14`).

### 2.3 What Persona can borrow [Assessment]

1. **Segmented progress after scanning, not just a spinner**: Parse QR → Connect hub (DNS / TCP / TLS) → Validate token → Exchange device token → Read machines → Ready. Categorized failures:
   - DNS: "Can't find this address";
   - refusal/timeout: check hub is running and phone/hub share network or VPN;
   - 401 / 403: pairing code expired; rerun `personad hub pair`;
   - incompatible version;
   - Android uses the same stages/copy, strings in a shared table.
2. **Include hub identity in the QR**: certificate/public-key fingerprint in pairing URL. Compare automatically, showing a reference-app-style "Verify fingerprint" page on mismatch, clearly "changed / first time", never silently accepting. Explicitly warn in forms for HTTP (non-TLS), as the reference app does.
3. **Explain what pairing does**: "What this phone stores / what is added on the hub" sheet covering device token, Keychain, revocation in **Settings › Devices**, analogous to the reference app's Security panel and "What will happen on my computer?".
4. **Post-pairing readiness checklist**: machine online status, herdr/Claude versions, node/hub version match, notification permission. Each skippable with consequences, like the reference dependency list and "Skip for now, terminal only".
5. **Separate manual-entry fields**: address/token, distinct keyboards, screen-reader names; automatically split pasted `persona://pair?...` URLs.
6. **HTML prototype the whole setup first** (320 / 393 narrow screens, light/dark, Reduce Motion, all failure cases), then implement iOS/Android after user approval.

---

## 3. Prototype → review document → acceptance record

### 3.1 Prototype format [Fact]

- Four under `BH:prototypes/`:
  - `herdr-ui-review`: navigation, hierarchy, chat reading;
  - `herdr-onboarding`: connection plus component installation;
  - `herdr-connection-progress`: connection progress;
  - `herdr-moshi`: early Moshi-inspired single-file demo, 43 KB.
- Pure static `index.html + style.css + app.js`, **no dependencies or external requests**. Open directly or serve with `python3 -m http.server <port> --bind 127.0.0.1 --directory prototypes/<name>` (README introductions).
- Shared "studio" layout:
  - left design notes: one thesis, 2–3 principles ("tasks first, one fewer layer", "clear body, expandable process", "separate input and control"), inspirations, disclaimer "Interaction draft · all sample data · no host connections, approvals, or sends";
  - scenario buttons: normal flow plus slow connection, remote failure, confirmation wait, interrupted upload, missing dependencies;
  - right phone frame, 390×812 or 393×852, status bar and Dynamic Island, light/dark, "Narrow 320", "Restart" (`BH:prototypes/herdr-ui-review/index.html:5-27`, `herdr-onboarding/index.html:11-38`, `herdr-connection-progress/index.html:11-50`).
- Every README has four blocks (`BH:prototypes/*/README.md`):
  - **Scope**: "This is a … review draft, not a claim of completed native functionality";
  - **Try it**: clickable flows;
  - **Prototype/native boundary**: simulations, real events required, what doesn't count as acceptance;
  - **Verification record**: JS syntax, full flows in real browsers, measured `scrollWidth == clientWidth` at 393 dark / 320 light (no horizontal overflow), bottom actions always visible; screenshots in Git-ignored directories (`herdr-onboarding/README.md:34-38`).
- After approval, native results update prototype READMEs, e.g. "2026-10-26: approved version integrated into native App… this HTML remains an independently replayable synthetic demo, not real-connection evidence" (`herdr-connection-progress/README.md:21`).

### 3.2 Three review-document formats [Fact]

1. **Implementation study: an open-source reference** (`BH:reference-client/docs/HERDR_CINDY_REVIEW.md`)
   - Opening identifies the public GitHub Agent client Cindy, **pinned to a fixed commit**; read-only source review, no execution; "source/contract review, not a runtime-quality evaluation".
   - P0 / P1 table: "Implementation worth borrowing" and "Use and boundaries for this project".
   - "UI and interaction review" table: "Area / Current problem·goal / Concrete draft arrangement" (`:67-75`).
   - Borrow/not-borrow boundaries: content-first, stable navigation, composer/component consistency; not monochrome style, complex button morphing, or complete side drawers. The reference's own unverified areas aren't exemplars (`:77`).
   - Native order/regressions: navigation restoration, host isolation, drafts, Dynamic Type, keyboard (`:81`).
   - After approval, append "Native implementation": what changed and which protocols/gates stayed intact (`:83-91`).
2. **Same-device comparison: beside a reference App** (`BH:reference-client/docs/LARK_INBOX_UI_REVIEW.md`)
   - Opening gives baseline commit/method: inspect reference and own App on the same physical iPhone using device-control tooling, then compare Swift sources; "review only, no UI changes this round".
   - Differences: "Priority / Area / Reference device / Current / Direction", P0 / P1 / P2 (`:7-16`).
   - Main issue in one sentence: "Extra search field + taller rows = sparse first screen; fix structure/density before colors, weights, separators" (`:18`).
   - **Separate data questions from visual issues**: unread totals aren't fixed with copy alone; first-frame missing avatars don't prove loading failure; column counts differ by device width, not defects; compare colors under the same theme (`:20-25`).
   - "Existing capabilities and this round's boundary" identifies previous work and what isn't passed this time (`:27-33`).
   - Evidence list: screenshot filenames in ignored local directories, not committed (`:35-44`).
   - Source locations and recommended order (`:46-53`).
   - Dated "Native implementation progress" appended: work, test counts, isolated QA, unfinished list (`:58-107`).
3. **Content comparison and fix list** (`BH:reference-client/docs/LARK_CHAT_UI_REVIEW.md`)
   - Three columns: "Scope / Evidence and current state / Next step".
   - Repeated caveats: "don't mislabel our screenshots as originals", "don't claim pixel-perfect alignment", "don't claim FPS gains from screenshots" (`:3`, `:11`, `:31`, `:37`).

### 3.3 Acceptance records and levels of "passed" [Fact]

- **State levels** (`BH:reference-client/docs/FEISHU_PARITY.md:5`): "Implemented, protocol passed, simulator passed, real account passed, real device passed are different states; page resemblance or endpoint existence isn't passing".
- **Per-page checklist** (`:12`): top bar, navigation, type/line height, avatars, colors, spacing, separators, icons, hit areas, keyboard, scroll position, empty/loading/failure/offline, dark mode, Dynamic Type, VoiceOver.
- **Per-action checklist** (`:13`): entry, permissions, parameters, response, cross-device sync, repeated taps, failure/cancel/reconnect, account switch.
- Parity table: "Item / Current status / Remaining acceptance focus·upstream basis" (`:34-39`).
- **Individual acceptance records** (`BH:reference-client/docs/VALIDATION.md`, ~1 MB, ~206 second-level headings; same format in `BH:reference-client/docs/HERDR_PROGRESS.md`):
  - heading "Topic (date)";
  - fixed sections: scope, config/model, actual protocol, real service, simulator actions, cleanup, device package, boundaries (e.g. opening "Single-hop SSH connection" in `VALIDATION.md`);
  - every record includes:
    - counts as "iOS 18.5 / 27.1, N tests / M suites each";
    - log paths;
    - evidence in Git-ignored `.local-services/validation/<topic>/`, with `QA.md`, screenshots, `*-stats.json`;
    - **what this pass doesn't cover** ("doesn't mean…", "still pending…");
    - QA simulator data restored to pre-test SHA256;
    - synthetic-service write counts (e.g. empty `writeAttempts` proves no accidental real-data writes).
  - rules:
    - write failing regression first, confirm failure, then fix (`LARK_INBOX_UI_REVIEW.md:73`, `:89`);
    - wrong selector running zero tests isn't verification (`:73`);
    - early failing screenshots aren't final passing evidence (`VALIDATION.md`, "Urgent original message…");
    - automation tables count tests, not completed features (`VALIDATION.md`, "Automated verification").
- A separate "Pending acceptance" list covers real devices, network switches, lock-screen recovery, keyboard, rotation, etc. (`VALIDATION.md`, "Pending acceptance").

### 3.4 Assessment

- [Assessment] this workflow suits projects with frequently changing user requirements and reference-App visual parity: prototypes change quickly, reviews explain what/why/not, acceptance levels stop "visible in simulator" becoming "done".
- [Assessment] weaknesses:
  - very long/dense records: VALIDATION.md is 1 MB, expensive to search;
  - evidence in ignored local directories isn't available on another machine. `.local-services/` wasn't found locally this time; QA.md templates could only be inferred from docs;
  - prototype tokens copied manually.
- [Assessment] for Persona, one review md plus prototype directory per review suffices; acceptance as "level table + paragraph per item"; evidence stays uncommitted, but fixed-path conventions belong in `CLAUDE.local.md`.

---

## 4. Test pyramid and fixtures

### 4.1 Reference app layers [Fact]

| Layer | Example | Description |
| --- | --- | --- |
| Pure presentation / rule units | `ConversationUnreadBadgePresentationTests.swift` | Parameterized Swift Testing covers badge boundaries 99 / 100 / 601 / 999 / 1000 / Int.max, preferences, muted, pinned surfaces |
| Protocol client units (fake transport) | `NavigationUnreadClientTests.swift` | Request paths/query parameters; account checked before and after; malformed responses (`""`, `{}`, `"7"`, `-1`, `1.5`, overflow) **must never become 0**; reject oversized payloads |
| Model / workspace / journal units | `*ModelTests`, `*WorkspaceTests`, `*JournalTests`, `AgentSessionTests` (over 10,000 lines) | State machines, recovery, unknown receipts, account isolation |
| Hosted SwiftUI tests (in-process, not XCUITest) | `AgentNavigationBarTests.swift` | Real SwiftUI NavigationStack in UIWindow, checks UINavigationController hierarchy, back item, unchanged edge/content gesture delegates; `.serialized`, `.timeLimit`; explicitly "no touches sent, not proof of interactive device swipes" |
| Local real-service integration | `SSHIntegrationTests.swift` | Loopback NIOSSH server, actual `ssh-keygen` keys, fingerprint-before-auth, timeout/cancel isolation; host component separately runs `node --test host/<bridge>/test/*.test.mjs` (`BH:README.md:47-53`) |
| Pinned-upstream contracts | `BH:scripts/check-*-upstream.{mts,mjs}` (22) | Direct imports of services/routes from pinned upstream source, fastify `inject` on real routes, final evidence JSON such as `{passed, httpReads, httpMutations, realNetworkCalls:0, …}` (e.g. `check-workplace-upstream.mts`) |
| Fixture self-checks | `BH:scripts/check-*-fixture.mjs` (60) | Validate fault modes (`applied503` / `unapplied503` / `missingReceipt` / `missing`), print evidence JSON (e.g. `check-workplace-fixture.mjs`) |
| Synthetic UI acceptance | 72 modules under `BH:scripts/fixtures/` (gateway UI fixture `*-ui.mjs` entries); Debug arguments: 23 `--<app>-*` found, 17 Agent-related, plus voice/video/scroll debugging | Details in 4.2 |
| Real environment | Read-only real accounts, real SSH hosts, device overlay installs | Same-device comparison with device-control tooling; `VALIDATION.md` "Real environment verified" |

- Every layer runs on iOS 18.5 and 27.1, plus macOS. Latest full regression (`VALIDATION.md`, "Automated verification"):
  - macOS Swift Package: 2882 tests / 271 suites;
  - iOS 18.5, 27.1: 2353 / 191 each;
  - later phases run targeted suites, counted separately.
- **No XCUITest target** (`git ls-files` has no UITests), **no screenshot comparison library**: `BH:Packages/<app-kit>/Package.swift:36-41` depends only on swift-markdown, Citadel, SwiftTerm, swift-nio, swift-nio-ssh, swift-crypto. UI acceptance uses human/Cua actions and screenshots in isolated QA simulators, plus in-process hosted tests.

### 4.2 Fixture approach [Fact]

- **Synthetic service** (`BH:scripts/fixtures/README.md:1-20`):
  - fictional people, organizations, messages only; binds 127.0.0.1, default port 3334;
  - records each request in `<app>-ui-fixture-stats.json` to check repeat taps and operation order;
  - control endpoints (`POST /__fixture/...`) switch faults such as first 503, delay, lost receipt, external deletion;
  - environment scenarios such as `<APP>_UI_*_FAIL_FIRST=1`, `*_DELAY_MS`;
  - 180 messages, 501-person member windows, cursor `+` / `=` encoding traps, etc.;
  - generated media: silent Ogg Opus, color-bar video, blue/orange test PNGs.
- **Magic input**: isolated Agent fixtures respond to `100100` with Markdown samples; text containing `900900` simulates unknown outcome (`fixtures/README.md`, "Isolated Herdr recovery acceptance").
- **Isolated state files**: each fixture argument has separate runtime / state / reading / navigation files, never real data (`BH:App/<app-entry>.swift:157-166`); Release disables these arguments.
- **Fault injection**: `DebugAgentReplyFault` works only in simulator, only loopback hosts, only explicit arguments, once; drops one "submitted" receipt to create unknown outcome (`BH:App/DebugAgentFixture.swift:8-60`).
- **Pinned upstream**: protocol basis fixed to a commit, endpoint inventory generated via AST (`BH:reference-client/docs/FEISHU_PARITY.md`, "Audit baseline"; gateway audits `BH:scripts/audit-*.mjs`).

### 4.3 Persona equivalents [Fact]

- Units: 24 Swift files under `JV:ios/PersonaTests`, plus recorded `terminal-*.jsonl` frames; Go server tests, anonymized transcript goldens/corpus comparisons (`JV:reference-client/docs/operations/testing.md:5-15`, `JV:reference-client/docs/archive/integrations/msg-render-verification.md`).
- UI: 16 XCUITest files under `JV:ios/PersonaUITests`, e.g. `MessageMenuTests`, `SendSafetyTests`, `ConnectionTests`, `TerminalKeysTests`, `MachinePagesTests`, `LiveActivityShots`.
  - Most connect to `scripts/mock/mock_hub.py`; `MOCK_*` scenarios cover disconnects, delay, gaps, terminal streams, uploads; assertions read `GET /mock/resolved|sent|keys|uploads|hits` (`JV:reference-client/docs/operations/testing.md:79-139`).
  - `MachinePagesTests` screenshots each step in light/dark, attaching screenshots/view trees on failure (`:129-130`).
- Mock hub was built for screenshot privacy: three fictional machines, Agents, Inbox; DEBUG arguments jump directly to pages (`JV:reference-client/docs/operations/testing.md:40-77`, `JV:reference-client/docs/apple/ios.md:299-305`).
- App probes: `-showFoldProbe`, `-showFreezeProbe`, `-showAutoScroll` write `Documents/*.txt|json` (`JV:reference-client/docs/operations/testing.md:61-63`, `:161-173`).
- Rules: run UI regressions against unfixed code first and confirm failure (`JV:reference-client/docs/operations/testing.md:85`); privacy scanning (`:149-159`).
- UI checklist has only 5 lines, coarser than the reference app's per-page/per-action lists (`JV:reference-client/docs/operations/testing.md:30-36`); no acceptance levels either.

---

## 5. Persona design system today, compared item by item with the reference app

### 5.1 Persona today [Fact]

- **Styles × semantic colors**: 5 `AppStyle`s (Liquid Glass / Claude / Clay / Island / Lark) (`JV:ios/Shared/Theme.swift:5-54`).
  - style-dependent: `accent`, `userBubble`, `userBubbleText`, `pageBackground`, `card`, `cardStroke` (`:66-77`, `:219-269`);
  - style-independent: `brand`, `danger`, `positive`, `caution` / `cautionText`, `ai`, `loop`, `countBadge`, `monogramFill`, plus `Diff`, `Hue` (`:80-112`, `:211-216`).
- **Readability floors as rules** (`JV:reference-client/docs/apple/ios.md:223-225`):
  - white text on solid buttons/selected capsules ≥ 3.5:1;
  - white text on user bubbles ≥ 4.5:1;
  - primary-color links against pages ≥ 3:1;
  - gradients only darken, never brighten.
- **State palette** (Needs you / Working / Unread / Seen / Not running): each has `dot / text / tint`, shared across App, watch, Live Activity (`JV:ios/Shared/StatusPalette.swift:7-30`), rules `JV:reference-client/docs/apple/ios.md:232`.
- **Structural tokens**:
  - five `Radius` levels: 6 / 10 / 14 / 20 / 22 (`Theme.swift:116-127`);
  - `Space`: 2 / 4 / 8 / 12 / 16 / 20 / 24 / 32 (`:130-139`);
  - `Fill`: well, chip, pressed, mapping to system quaternary/tertiary/secondary fill (`:144-154`);
  - `Font`: role-based Dynamic Type: panelTitle, rowTitle, body, cardBody, preview, meta, chip, micro, code, greeting (`:158-180`);
  - `Motion`: state `.snappy 0.25`, layout `.smooth 0.3` (`:205-208`);
  - separate `Watch` sizes (`:184-201`).
- **Styled modifiers**: `surface(tint:interactive:in:)`, `themedButton(prominent:)`, `themedPage()`, `themedGroupedPage()`, `themedRow()`, plus Clay/Island `SoftButtonStyle`, at least 44pt (`JV:ios/Persona/Views/Styling.swift:6-137`). RootView rebuilds with style as id when switching, so static tokens suffice (`JV:reference-client/docs/apple/ios.md:229`).
- **Components** (`JV:ios/Persona/Views/Components.swift`):
  - `StatusDot`: Needs you breathes, stops under Reduce Motion (`:6-38`);
  - `SessionAvatar` (`:41-69`);
  - `Chip`: neutral / tinted / outlined (`:75-128`);
  - `CountBadge`: 99+ above 99 (`:132-165`);
  - `StatusLabel` (`:167-178`);
  - `EmptyStateView`: icon, title, explanation, optional action (`:180-206`);
  - `.card()` (`:208-215`);
  - `CloseAgentSheet`: destructive confirmation identifies Agent, location, consequences (`:228-333`).
- **Connection status** in the title, no banner (`JV:reference-client/docs/apple/ios.md:273-297`).
- **Offline actions**: messages stored in Outbox and auto-sent on reconnect with the same `client_msg_id`; one-off approvals/answers aren't queued, failures restore the card with a blue notice (`:288-293`).
- **Adoption** [Fact, grep scoped to `JV:ios`, 165 Swift files, 41,816 lines]:
  - `Theme.Font.` once, `Theme.Space.` once;
  - `Theme.Radius.` 74, but 70 literal `cornerRadius:` values;
  - many literal `spacing:` values: 6 appears 113 times, 8 appears 87; 464 literal `.padding(number)`;
  - 37 view `Color(light:` calls outside Theme / StatusPalette, in Lark rendering, machine charts, OSBadge, Styling, Live Activity;
  - 49 direct system `.orange/.red/.green/.blue`;
  - 60 fixed `font(.system(size:`, 646 text styles;
  - zero `isAccessibilitySize`, 16 `dynamicTypeSize(` caps, 4 `ViewThatFits`;
  - 15 `accessibilityIdentifier(`, 124 `accessibilityLabel(`;
  - 303 `lineLimit(1)`, 26 `minimumScaleFactor` (matching "button labels never wrap").

### 5.2 Item-by-item comparison

| Dimension | Reference app | Persona | [Assessment] Better approach / lesson |
| --- | --- | --- | --- |
| Color tokens | Two palettes (Lark blue-gray, Herdr forest green), no shared semantic layer | 5 styles × semantic colors, separate state palette, written contrast rules | Persona fuller; needs one machine-readable source and automated contrast checks |
| Type | No tokens; fixed chrome sizes, body text styles | Role tokens barely used; few fixed sizes | Persona's direction is right; adopt roles and lint them |
| Spacing / radii | All literals, ~8 radius levels | Tokens partially adopted | Both need tokens plus checks |
| Components | Thin, duplicate functions (two status rows) | One drawing each for `Chip` / `CountBadge`, documented rules (`JV:reference-client/docs/apple/ios.md:244-246`) | Preserve Persona's "one thing, one drawing", extend to states/forms |
| Navigation | Custom compact root chrome, system back in details, hosted gesture tests | Custom collapsible chat chrome, manually restored edge back | Android should use system predictive back, not custom gestures |
| Accessibility | 28 accessibility-size layout switches, Reduce Motion, full badge values spoken | Good text styles/icon caps, no accessibility-size layout changes | Borrow vertical layout / hide decoration / relax line limits |
| Empty / error / loading | `ServiceStatusView` + system `ContentUnavailableView`, nearby errors/retry | `EmptyStateView` + system controls, hides cancellation/offline errors, cache first/no spinner | Shared page-state component family, same names/parameters on both platforms |
| Uncertain outcomes | Consistent words/icons/read-only verification, never auto-resends | Outbox auto-resends idempotently via `client_msg_id`; one-off actions not queued | Different premise: Persona hub guarantees idempotency, allowing resend. Borrow named uncertain states in a component: "Sending / Send when connected / Not applied" |
| Prototypes | HTML + README boundaries/verification | None (only `reference-client/docs/screenshots`) | Introduce |
| Reviews | Reference study / same-device comparison / fix list | Lane docs focus on data paths (`JV:reference-client/docs/archive/lanes/`) | Introduce UI review templates |
| Acceptance levels | Implemented → protocol → simulator → real account → device | None, 5-line UI checklist | Introduce levels and per-page checklist (one column per platform) |
| UI automation | No XCUITest; hosted tests, isolated QA simulator, Cua screenshots | 16 XCUITests + mock hub, step screenshots in light/dark | Persona more automated; add in-process component snapshots |
| Fixtures | Synthetic services, `/__fixture` controls, request stats, faults, magic input, pinned upstream contracts | `mock_hub.py`, `MOCK_*`, `/mock/*` readback, anonymized real goldens | Similar approaches; add mock contracts tested with real hub, e.g. Go validates mock response schemas |

---

## 6. Recommendations for Persona redesign and Android parity [all assessments]

### 6.1 Design tokens: one JSON source generates Swift / Kotlin / CSS

- **Single source** in `design/tokens/*.json`, following W3C Design Tokens Community Group (DTCG) conventions (`$value` / `$type`); confirm exact version at implementation. Layers:
  1. `color.semantic`:
     - style × light/dark: `accent`, `userBubble`, `userBubbleText`, `pageBackground`, `card`, `cardStroke`;
     - style-independent: `brand`, `danger`, `positive`, `caution`, `cautionText`, `ai`, `loop`, `countBadge`, `diff.*`, `hue.*`;
     - `fill.well`, `fill.chip`, `fill.pressed`: iOS system fills need explicit Android colors.
  2. `color.status`: five states × `dot / text / tint`, shared by iOS App/watch/Live Activity and Android notification accents/widgets.
  3. `dimension`:
     - `radius`: 6 / 10 / 14 / 20 / 22;
     - `space`: 2…32;
     - `minTouch`: **platform-specific**, iOS 44pt, Android Material-recommended 48dp;
     - separate watch dimensions.
  4. `type`: roles such as `rowTitle` = "default 17, semibold". iOS maps to text styles (`.headline`) with Dynamic Type; Android to sp `TextStyle` with fontScale. Keep existing fixed-size allowlist: terminal, Dynamic Island, share cards, Lark cards (`JV:reference-client/docs/apple/ios.md:238`).
  5. `motion`: state 0.25s snappy, layout 0.3s smooth, with Reduce Motion semantics.
  6. `rules`: **machine-checkable contrast floors**, e.g. `onAccent / accent ≥ 3.5`, `userBubbleText / userBubble ≥ 4.5`, `accent / page ≥ 3`, `status.text / page ≥ 4.5`. Check every style × light/dark; fail generation below floors. Turns `JV:reference-client/docs/apple/ios.md:223-225` prose into checks.
- **Generator**: `scripts/assets/gen_tokens.py`, matching existing Python tooling. Outputs:
  - `ios/Shared/Generated/Tokens.swift`: pure-data enums `nonisolated`, per CLAUDE.md's `ios/Shared` default MainActor lesson;
  - `android/core/designsystem/.../Tokens.kt`: `PersonaColors`, `LocalPersonaColors`, `Dp` / `TextStyle`, mapped to `MaterialTheme.colorScheme`;
  - `prototypes/_tokens.css`: HTML tokens, removing manual prototype color duplication;
  - `reference-client/docs/design-tokens.md`: generated palettes/dimensions;
  - `--check`: regenerate/compare repository files plus contrast checks, in pre-commit or release scripts;
  - alternative Style Dictionary (Node) supports Swift/Compose outputs but adds another toolchain. Persona tooling is Python, so I'd favor a custom generator under a few hundred lines.
- **Guard adoption**: simple grep rules plus an allowlist, pre-commit/CI, forbid outside token files:
  - `Color(red:` / `Color(light:`, direct system `.orange` / `.red`, etc.;
  - `cornerRadius: <nonzero literal>`;
  - `font(.system(size:` (allowlist exceptions);
  - Kotlin `Color(0x…)`, literal `.dp` radii, literal `.sp`;
  - baseline counts from 5.1, only decreasing.
- **Android styles**: Liquid Glass is iOS 26 material. Make surface treatment a token (`glass / solid / soft / hairline`) implemented per platform: Android translucent blur (Android 12+ RenderEffect) or tonal layering. Don't seek pixel parity, only matching semantics/contrast. First Android release may do 2–3 styles, e.g. Lark/Claude, after checking user preference.

### 6.2 Component library

- iOS: extract `Theme`, `StatusPalette`, `Styling`, `Components` into a local Swift Package (e.g. `PersonaDesignSystem`), following reference-app boundaries: no AppModel dependency, no network (`BH:reference-client/docs/ARCHITECTURE.md:45`). AppModel-dependent pieces like `CloseAgentSheet` become pure views with callbacks.
- Android: `:core:designsystem`. **Names, parameters, states match iOS one to one**; one spec per component (dimensions, states, spoken-label rules, minimum hit area, wrapping), under `reference-client/docs/components/`.
- Both get a **component catalog**: iOS DEBUG `-openCatalog YES`, Android debug Activity, for human review and screenshot tests.
- First components combine both projects' strengths:
  - **Foundations**: Surface (style-specific treatment), Card, Panel, page backgrounds, hairline separators.
  - **Buttons**: Primary / Secondary / Destructive / IconButton. Single-line labels (CLAUDE.md), minimum hit areas, required spoken IconButton labels (reference `WorkspaceIconButton`), button loading such as "Saving…" plus disabled.
  - **States**: StatusDot, StatusLabel, Chip (neutral/tinted/outlined), CountBadge (99+ or ellipsis visually, **full count spoken**, from reference), SessionAvatar, MonogramAvatar (name-hash colors, reference `WorkspaceAvatar`).
  - **Page-state family**: EmptyState, ServiceState (unpaired/unreachable hub), InlineError (nearby retry), Notice (caution/info), CachedNote ("Local cache · N minutes ago"), ConnectionTitle, plus **PendingState**: shared copy/icon table for "Sending / Send when connected / Not applied / Needs verification".
  - **Flows**: StepProgress (stages/timestamps, no fake percentages, no animation under Reduce Motion, pause awaiting confirmation), ConfirmSheet (`CloseAgentSheet`: what/where/consequences/red primary/cancel), FingerprintCard (monospaced, copyable, first/changed distinction).
  - **Forms**: FormSection (heading/panel, reference `SSHKeyFormSection`), LabeledField (spoken name/keyboard), SecretField (hide on focus loss, `privacySensitive`, clear on exit).
  - **Product pieces** remain in Features but only use tokens: ComposerRow, WorkLine, four interaction cards, DiffView, MonoText, TerminalKeyBar.

### 6.3 Cross-platform navigation and accessibility

- Roots may have compact custom chrome, connection status in title (already Persona). Details use system navigation/back:
  - keep current iOS behavior; new pages should preserve system back like the reference, with hosted gesture tests;
  - Android system predictive back + Navigation Compose, no custom edge gestures.
- One shared accessibility-size rule in component specs:
  - iOS `isAccessibilitySize`, Android fontScale above a measured threshold (TBD);
  - vertical segmented controls, hide leading decorations, relax title/path limits; buttons that don't fit use icons, shorter labels, or menus (never wrap).
- Reduce Motion: iOS `accessibilityReduceMotion`; Android reads system "Remove animations", exposes `LocalReduceMotion` in designsystem.
- Screen readers: hide decoration, speak full counts, progress speaks one status line (like reference connection progress).

### 6.4 Tests and screenshot acceptance

| Layer | iOS | Android | Shared |
| --- | --- | --- | --- |
| Tokens | `gen_tokens.py --check` + contrast | Same | Token JSON |
| Presentation rules | Parameterized Swift Testing, like reference badges | Parameterized JUnit | **Shared golden JSON**: item/event inputs, row/copy outputs; badge rules, `Fmt.relative`, `chatLines` / WorkLine splitting; same corpus aligns logic |
| Component snapshots | Catalog × {light,dark} × {chosen styles} × {default,large,accessibility} × {narrow 320,regular}; render UIWindow or swift-snapshot-testing. `ImageRenderer` can't render ScrollView (`JV:reference-client/docs/apple/ios.md:126`) | Same parameters; JVM Roborazzi (Robolectric, interactions/dialogs), ComposablePreviewScanner scans `@Preview`; alternatives Paparazzi (layoutlib) or official Compose Preview Screenshot Testing (search says still alpha). Verify versions at implementation | Fictional data only (privacy); explicit baseline updates, e.g. `record` |
| Flow UI | Existing XCUITest + `mock_hub.py`, `/mock/*` readback | Compose UI test, JVM in-process fake hub; instrumentation via `adb reverse` to the same `mock_hub.py` | **Shared mock hub/switches**; Go validates responses against real hub schema to prevent drift (reference pinned-upstream contract approach) |
| Real chain | Local test hub (fake HOME, separate DB), no production hub | Same | `JV:reference-client/docs/operations/testing.md:131-136` flow |
| Performance | App probes (fold/freeze/frames) | JankStats or Macrobenchmark (verify at implementation) | **Measured numbers only**, sample ≥ 10 for baselines |
| Human acceptance | Per-page checklist + levels | Same | See 6.5 |

- Screenshot reviews: each UI change runs light/dark × type size × platforms, assembled into a comparison for user approval before baseline updates. Mock-only committed screenshots; real-data screenshots stay in ignored local directories, as in the reference app.
- Preserve Persona's good rules: failing UI regression first; DEBUG direct-page arguments (Android `adb shell am start` extras or debug deep links); privacy scanning covers Android.

### 6.5 Review and acceptance workflow (Persona template)

1. **Prototype**: `prototypes/<topic>/`, using `_tokens.css`; iPhone 393 and Android ~360dp frames, light/dark, narrow screens, Reduce Motion, fault-scenario buttons. README: scope, try it, native boundary, verification record.
2. **Review**: `reference-client/docs/reviews/<date>-<topic>.md`.
   - Basis: reference and pinned version.
   - P0 / P1 / P2 table: "Area / Current / Reference / Direction".
   - Separate data from visual issues.
   - State what not to borrow, what doesn't pass this round, implementation order.
3. Implement iOS/Android after **user approval**.
4. **Parity/acceptance table**: `reference-client/docs/parity.md`, one page/feature per row.
   - iOS level, Android level, evidence, pending checks.
   - Levels: implemented → units passed → mock screenshots passed (light/dark × type size) → test hub passed → device passed (iPhone / Android / watch).
   - Use the reference per-page checklist (3.3), adding cross-platform consistency, single-line buttons, in-App links.
5. **Evidence** in a fixed directory outside the repo, path in `CLAUDE.local.md`; commit only numbers and mock screenshots.

---

## 7. Unresolved / unverified

- Reference `.local-services/validation/*/QA.md`, screenshots, stats aren't present locally; QA.md format inferred from citations. No repository scripts show how Cua operates QA simulators, so reproducibility is unknown.
- Reference HTML prototypes weren't run in browsers, viewed, or screenshotted; only sources/READMEs read.
- Adaptive iOS 27.1 tabs (`toolbarVerticalEdge`) weren't tested; based on code and VALIDATION "Duo main tabs" record only.
- No deep review of reference `MessagingFeature` bubbles/rich text or PeopleFeature and other Lark visual details.
- Persona's themes-fix contrast report (`JV:reference-client/docs/apple/ios.md:223`) isn't in the repo; contrast numbers not rechecked.
- Android screenshot-tool versions/maturity (Roborazzi / Paparazzi / Compose Preview Screenshot Testing) researched with one web search; third-party speed comparisons conflict, **no measurements**, no speed figures quoted. DTCG and Style Dictionary status also unverified.
- Persona watch/Live Activity design details weren't reviewed; only known to share `StatusPalette`.

## Appendix: external references (web search, 2026-10-30)

- Compose screenshot-library comparisons (third-party articles, differing conclusions): <https://medium.com/@natalia.kulbaka/comparing-snapshot-testing-libraries-paparazzi-roborazzi-compose-previews-screenshot-testing-b7c3b47f7f59>, <https://medium.com/@papppali2000/comparison-of-android-snapshot-testing-libraries-paparazzi-vs-roborazzi-vs-compose-preview-3e506d2d9c05>
- Screenshot playground (listed versions: CPST 0.0.1-alpha16, Paparazzi 2.0.0-alpha04, Roborazzi 1.70.0): <https://github.com/sergio-sastre/Android-screenshot-testing-playground>
- ComposablePreviewScanner: <https://github.com/sergio-sastre/ComposablePreviewScanner/wiki>

## Appendix: reference `scripts/` grouped by purpose [Fact, `git ls-files`]

157 files total:
- 84 top-level scripts:
  - 60 `check-*-fixture.mjs`: synthetic-service behavior/fault consistency, evidence JSON;
  - 22 `check-*-upstream.{mts,mjs}`: directly invoke pinned upstream source with fastify inject for contracts;
  - gateway audits (`audit-*.mjs`): AST scans generate inventories into `reference-client/docs/audits/*.json`;
  - `make-qr-ui-fixtures.swift`: QR UI fixtures.
- `fixtures/`: 72 synthetic-service modules plus 1 `README.md` explaining scenario switches, controls, statistics.
