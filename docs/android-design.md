# Android interface styles and design system

Since 2026-10-31, the Android App's visuals and interaction have been redone wholesale. As the user asked, all three directions became themes, switched in **Settings › Interface style**; the default is **Glass**.
The old design (the parts of `docs/redesign/` about appearance, layout and interaction, and the HTML prototypes in `prototypes/android/`) is superseded by this document.

## 1. Three styles

Pages are written once. Each style provides its own design tokens (palette, typography, shapes, motion), component skins, and the App shell and navigation. Interaction differences live in components rendered per style; page code does not branch.

| | Glass (default) | Brief | Board |
| --- | --- | --- | --- |
| In one line | A layer of frosted glass floating over the "status light" | Persona speaks first: ink on white paper, one highlighter | A control tower's flight strips: one strip per Agent |
| Background | Night blue `#060A14` / morning mist `#E9EEF6`, with light on top that changes with state | White paper `#FFFFFF` / ink night `#0E1117` | Board grey `#E6E9ED` / graphite `#0E1114` |
| Accent colors | Amber = waiting for you, blue = running, mint = done / connected | Only the highlighter `#FFE15A`, marking only Persona's suggestions and things waiting for you | The color block on the strip's left is the state: amber / blue / green / grey |
| Primary button | Solid white (dark) / solid ink (light), capsule | Solid ink capsule, inverted in dark mode | Solid ink, small 8dp corners |
| Typeface (Latin and digits) | Sora (large titles, numbers) | Manrope (everything) | Barlow Semi Condensed (numbers, fields, counts) |
| Shapes | Cards 28dp, overlays 30–36dp capsules | Cards 24dp, everything else rows without a fill | Small 10dp corners plus a 1dp stroke |
| Motion | Springs; the status light fades over about 1 second and drifts slowly, splitting and merging, the rest of the time (see section 3, "Status light motion") | Gentle 240ms transitions | Crisp 160ms |
| Navigation (the same four tabs: Persona, Agents, Inbox, Settings, icons with always-visible labels) | A floating glass dock at the bottom | A paper bottom bar flush with the edge, with a thin line on top | A four-cell bottom bar, the selected cell with a bar on top |
| Things for you to decide | Glass cards in lists; in chats a floating glass panel with an amber edge | Light grey cards in lists, Persona's suggestions highlighted; in chats a decision panel docked at the bottom | A strip inside an amber block, expanding in place on tap; in chats a clearance bar under an amber line |
| Root page title | The large title scrolls away with the content, leaving only floating round buttons in the top bar; a title capsule appears after scrolling | Home is a greeting and a one-line summary ("Good morning, 3 things need your decision."), other pages have large titles | A compact title bar with the title always in it |

Dark and light still follow the system, independent of the style. Chinese always uses the phone's own system font; Latin letters and digits use the bundled typefaces (OFL, licenses in `core/designsystem/src/main/assets/licenses/`).
Icons are all Phosphor (MIT), generated into `PhIcons` by `scripts/assets/gen_phosphor_icons.py`; `JIcon` picks the weight per style: regular for Glass and Brief, bold for Board, fill for selected states, and duotone for the leading icons of settings rows in Glass.

### 1.1 Switching and saving

- Entry: Settings › Interface style (`AppearanceSettings`). Each of the three options has a small preview drawn with that style's real components; tapping one applies it immediately, no restart. The preview is a one-third-size Agents page: one computer, one of its spaces and three sessions, with the style's navigation underneath (Glass's floating dock is drawn as an opaque panel, since the preview has no content to refract).
- The choice is stored on the device (`AppStyleStore`, SharedPreferences `persona-appearance`), not on the hub, and survives re-pairing.
- Debug builds can set it at launch: `am start … --es debug.style glass|brief|board` (used by the screenshot script).

### 1.2 Glass transparency and fallbacks

| Situation | Effect |
| --- | --- |
| Android 13 and later, transparency "Auto" | Refractive glass (Haze 2's glass, `GlassLevel.Liquid`) |
| Android 12 | Blurred glass (`GlassLevel.Frosted`) |
| Transparency set to "Reduce transparency", or the phone is in battery saver | Opaque panels (`GlassLevel.Solid`) |
| Sustained dropped frames while scrolling (`GlassFrameMonitor`: over 30% of 240 frames exceed 1.5× the frame budget) | Drops one level for this run: refractive → blurred → opaque |
| Terminal page (always) | Opaque panels: the terminal changes every frame (every redraw by the program, every scrolled line), and glass over it would have to recompute the blur of the top bar, key bar and edges every frame (2026-11-01) |

Only pieces floating over scrolling content (top bar, floating dock, input field, decision panel) use refraction or blur; cards inside the page are a translucent fill plus a gradient stroke with no live blur, so scrolling costs nothing extra.
Under the full height of the top bar and of the bottom overlays (floating dock, input field) there is a "scroll edge": content sliding in is blurred and washed toward the background across the whole overlay area, fading out only over the last 28dp next to the content,
so the gaps between overlays (between the title capsule and the task bar, between the status line and the input field) never show legible text. It washes toward the page's own background layer (the same status light, `PageBackdrop`), not a solid color:
with no content underneath the edge is invisible, and empty pages (e.g. a chat just created in Persona) get no colored band at the top or bottom (the hard cut the user pointed out on 2026-11-01).
List pages draw the edge only when content has actually scrolled under an overlay (not while resting at the top, saving one blur); pages without a top bar (a document embedded in a link card) draw no top edge. The opaque level does not blur; it washes the content from dense to light toward the background.
A page embedded in another (the chat inside the Persona page) fills the whole page and slides under the host's top bar and floating dock: the host tells it how much to leave via `ChromeInsets(top, bottom)`, and it draws the edges itself (the host uses `JPage(edges = false)`).
Implementation note: Compose's `Canvas` does not clip by default, and glow centers laid out relative to the window often fall outside this page, so `AmbientLight` must `clipToBounds()`, or it paints onto the neighboring column.
The blur input is quarter resolution (`LocalHazePerformanceMode` = `Performance` in `PersonaTheme`): measured on a phone, full resolution doubled the chat page's frame time, and the reduced version looks no different.
Performance measurements are in section 5.

## 2. Design system (`core:designsystem`)

### 2.1 Tokens

| Accessor | Contents |
| --- | --- |
| `PersonaTheme.style` | current style `AppStyle` |
| `PersonaTheme.palette` | `PersonaPalette`: background, surface (translucent in Glass), surfaceOpaque, surfaceStrong, sunken, line, ink / ink2 / ink3, action / onAction, accent, marker, per-state `StatusColors(dot, text, tint, onDot)`, diff and terminal colors |
| `PersonaTheme.type` | `PersonaType`: largeTitle, title, headline, body, bodyStrong, callout, caption, micro (tabular digits), label, labelSmall, number, voice (the line Persona speaks), code |
| `PersonaTheme.shapes` | corner radii for cards, panels, controls, chips, buttons, bottom sheets |
| `PersonaTheme.motion` | durations and curves (springs in Glass); `count` is the duration for numbers rolling to a new value and chart bars growing to a new height (#2811) |
| `PersonaTheme.glass` | the glass level actually rendered |

Material 3's ColorScheme, Typography and Shapes are also generated from the current style, so Material components not yet replaced change colors and fonts along with it, but pages should switch to the components below.
The old `PersonaTypography.*`, `statusTone(...)`, `PrimaryButton` / `SecondaryButton` / `Card` / `EmptyState` still work and now map to the current style internally; new code uses the new components directly.

### 2.2 Components

| Component | Use |
| --- | --- |
| `JPage` | A page with custom content. Pass the `PaddingValues` it gives you as the scroll container's contentPadding: in Glass the top bar, input field and floating dock all float over the content, and this is what leaves room for them |
| `JListPage` | List pages (root pages and most second-level pages). Includes a large title, pull to refresh and content padding; `summary` is Persona's one line about this page, `greet` adds the date and a greeting (home only) |
| `JTopBar` | Page top bar; already included in `JPage` / `JListPage`, used alone for special pages. In Glass, while the large title is still on screen (`showTitle = false`), back and actions are separate floating round buttons; only after the large title scrolls away do they merge into a capsule with the title |
| `JFullScreen` | A full page over the current page (in-session search, full-text reading, task progress, spaces, past sessions): extends under the system bars, status bar icons follow dark/light. Do not build full pages with Compose's `Dialog` directly |
| `JBottomBar` | A row of actions at the bottom of a page (paging, load more): a floating capsule in Glass, a bar flush with the bottom in Brief and Board |
| `ChatTopBar` | Chat page top bar: title, one status line (state, which computer and directory), actions |
| `JSectionHeader` | Section heading within a page, optionally with a count and an action |
| `JGroupItem` + `GroupPosition` | One item in a group. In lazy lists, one item per entry; the component draws a continuous container by position (glass panel, rows with dividers, a bordered table); `separate = true` makes them independent cards |
| `JGroup` | Small groups that need no lazy loading (settings pages) |
| `JRow` / `JSwitchRow` | Settings-style rows: icon, title, subtitle, value, switch, chevron. For multi-line content in the middle (wrapping addresses, tags, test results, a leading status dot) use the slot form `JRow(onClick = …, trailing = …) { … }`: horizontal padding, minimum height, tap and long press, selection, dimming when disabled, trailing controls and chevron are the same as a normal row and align automatically in every style. `rowPadding` is the row's horizontal padding (Glass 16, Brief 0, Board 14); use it for content hung under a row that must align with the row's text (long values, explanations) |
| `JCard` | Card; `status` gives the glass stroke the state color, or adds a color block in Board |
| `JNotice` | In-page notice (connection lost, permission needed), with one action |
| `JEmptyState` / `JLoading` | Empty state, loading |
| `JButton` (Primary / Secondary / Quiet / Destructive, Large / Medium / Small), `JTextButton`, `JIconButton` | Buttons. Text is always one line |
| `Modifier.stableAlpha` | Changing opacity (dimming when disabled). Do not use `Modifier.alpha`: switching between 1 and another value adds or removes a layer, and placed after `clip` + `background` the background color gets lost (`docs/android.md` lessons) |
| `JTextField` / `JSearchField` | Input fields |
| `JSegmented`, `JChip`, `QuickFilterBar` | Mutually exclusive filters, chips, quick filters at the top of a list (chips in Glass, underlined text tabs in Brief, a big-number count board in Board) |
| `JSwitch` / `JCheckbox` / `JRadio` | Switch, checkbox, radio |
| `JBadge` / `JTag` | Count of things waiting for you, small static tags (draft, demo) |
| `JChangingText` | Values that change on redraw: numbers count to the new value, other text fades in (numbers in extension Views, row values) |
| `JStatusDot` / `JStatusMark` / `StatusLabel` / `StatusLine` / `PendingBadge` | State: small dot, the mark before a session row (glowing in Glass, spinner and check mark in Brief, square in Board), status text, the status line in a chat |
| `JAvatar` | People, groups, sources; a corner badge marks Lark / mail |
| `JSwipeActions` | Left/right swipe actions on rows (Inbox: swipe right for done, left to hand to Persona) |
| `JCodeBlock` | Commands and code |
| `JSheet` / `JActionSheet` / `JMenu` / `JDialog` | Bottom sheet, long-press action sheet (instead of long dropdowns), small anchored menu, confirmation dialog |
| `JActivityBar` | The line under the chat top bar for "what the Agent keeps working on" (goal bar, `docs/agent-goal.md`): icon + one line of text + trailing state, colors from a set of `StatusColors`; Glass and Brief use a light tint of the state color, Board uses the surface color with a thin line; tap to open details |
| `DecisionCard` | "Things for you to decide" in lists |
| `DecisionDock` | Pending requests in a chat, replacing the input field |
| `DecisionOption` / `DecisionButtons` / `DecisionAdvice` | Options (the recommended one is emphasized differently in each of the three styles), answer buttons, Persona's suggestion |
| `MachineChip` | Computer chips at the top of the Agents page (all machines + each computer): the style's filter chip, an online dot on the corner of the device icon, a count after the name |
| `AgentTreeHeader` / `AgentSpaceHeader` / `AgentRow` / `AgentTabRow` / `AgentIdleRow` | The Agents tree (see section 2.4): the first level (computers, also used for the pinned, by-state and past-projects sections), spaces (also repos and worktrees when grouped by repo, with `icon`), session rows (a row on the background in Glass and Brief, a flight strip in Board; row flags `RowFlag`, facts `RowFact`; the initiator's `launcher` mark, the branch's thin `branch` line), the tab row when one tab has several Agents ("N agents" at the row end collapses / expands), "N more idle". Small icon buttons in headers (gauge, a space's `⋯`) are `HeadingAction`. The Agents page, the space page, the component catalog and the interface style previews all use this set; nothing is written separately |
| `UserBubbleFrame` / `UserBubble` / `WorkGroup` / `WorkStep` / `PlanCard` / `TimelineMark` | Chat: user messages (your own messages fill `palette.userBubble` in all three styles, i.e. iOS Liquid Glass's light blue `#DCEAFF` / `#1D3557`; messages from other sessions, forwarded from other chats, and from Lark pass `own = false` and keep each style's original background; the check is `isOwnMessage` in `core/domain`, #2643), the collapsed work process (`peek`: the latest few steps shown while running), each step, plan cards (Step N/M), dividers |
| `ComposerFrame` / `ComposerTextField` / `SendButton` / `JumpToLatest` | The input field's frame, text entry, send / stop, jump to latest |
| `ComposerExpandButton` / `ExpandedComposer` / `rememberComposerGrowLines` | The phone composer's grow-then-expand (#5688): the field grows to a line limit from the height above the keyboard (22% of it, 3 to 8 lines, as iOS), then scrolls and shows ↗↙ at the text's top right (the text insets by `ComposerExpandInset` only then); Expand opens the draft full screen on the same text and caret |
| `MessageItem` / `DraftPreview` | An Inbox item, the reply Persona drafted |
| `HubCard` | hub status at the top of the settings page |
| `AppNavigation` / `HubAvatar` / `PersonaOrb` | Navigation of the App shell (used by `:app`) |
| `PageBackdrop` | The background layer shared by pages, bars and the shell: the page background color, plus in Glass the status light laid out over the whole window. Adjacent bars, nested pages and the gaps between them draw as one piece; the page scaffold and shell already use it; places that draw their own background (e.g. the chat list in wide-screen Persona) use it too, not a separate `surface`-style backing |
| `PaneDivider` | The handle between two panes on wide screens: a translucent ink pill in Glass, a thin ink line in Brief, a small stroked cell in Board; dragging, hover cursor, TalkBack's "widen / narrow / collapse / expand left pane"; used by the shell (see section 3.1) |
| `VoiceOrb` / `VoiceStatus` / `VoiceCaption` / `VoiceControl` / `VoiceControlBar` | Voice chat page: the orb (connecting, listening, speaking, muted, ended), status line, captions, round labeled buttons at the bottom (used by `feature:voice`, `docs/android.md` ("Voice chat")) |
| `AmbientLight` / `floatingSurface` / `glassSource` | Glass's status light (`inWindow` lays it out by window, thumbnail previews by their own frame), overlays, the content overlays sample (handled by the page scaffold; pages normally do not use these directly) |

When an extension's View redraws (refresh, `ui.invalidate`, a button returning a new View), it shows what changed (#2811, same as web #2757): for the same key,
`metric` / `stat_grid` / row values where only the number changed count to the new value (`JChangingText`: only the digits move, each frame written in the original value's format, `2/8 → 6/8`, `¥1.20 → ¥36.20`),
other text changes fade in; a row badge changing tone transitions its color (`JTag(…, animated = true)`); in lists, blocks added by item key fade in, removed ones fade out, the rest move to their new positions;
when chart labels and series count are unchanged, bars / lines move to the new values and the axis ticks switch directly. The first draw does not animate; with system animations off (animation scale 0) nothing moves, and what is shown at rest is exactly the text the extension sent.

### 2.3 Rules for writing pages

1. Assemble pages only from the components above. Do not use Material 3 visual components directly: `Scaffold`, `TopAppBar`, `NavigationBar`, the `Button` family, `Card`, `OutlinedTextField`, `FilterChip`, `AlertDialog`, `ModalBottomSheet`, or multi-item `DropdownMenu`. Primitives such as `Text`, `LazyColumn`, `Box` / `Row` / `Column` are fine.
2. Colors come only from `PersonaTheme.palette`, font sizes only from `PersonaTheme.type`, corner radii only from `PersonaTheme.shapes`; no `Color(0x…)` or numeric font sizes. Icons use `PhIcons` + `JIcon`, not material-icons.
3. Page structure: root pages use `JListPage` (without `onBack`, automatically a large title); secondary pages use `JListPage(onBack = …)`; non-list content (chat, terminal, reader) uses `JPage`.
4. Always use the padding provided by `JPage` as the scroll container's contentPadding, not just padding on the outer container (otherwise content in Glass does not slide under the overlays).
5. Groups use `JGroupItem(GroupPosition.of(i, n))` or row components with a `position` argument, one row per lazy item; do not wrap many rows in one item (long lists will stutter).
6. "Things for you to decide": `DecisionCard` in lists, `DecisionDock` in chats, `DecisionOption` for options (`recommended = true` for the recommended one), `DecisionButtons` for buttons, `DecisionAdvice` for Persona's suggestion.
7. Use `JActionSheet` for long-press menus, `JMenu` for anchored menus with two or three items. Full-screen pages use `JFullScreen`, bottom action bars use `JBottomBar`; do not float text buttons directly over content.
8. Do not join metadata strings with "·": pass machines, directories, Agent types, loops and background work as component parameters (`machine`, `folder`, `agent`, `facts`).
9. No `if (PersonaTheme.style == …)` in pages. Style-dependent behavior belongs in components; if a component lacks a parameter, say so in the report and have the design system owner add it.
10. Button text stays on one line; shorten it or use an icon when space is tight (project rule).

### 2.4 Agents list (computer → space → session)

Redone on 2026-11-02 following user feedback (the original complaint: devices, workspaces and sessions looked disconnected, the hierarchy felt wrong, screen space was wasted; All machines should not be a collapsible section; idle spaces gave no clue what they were for).
Components live in `Agents.kt` in `core:designsystem` (`MachineChip`, `AgentTreeHeader`, `AgentSpaceHeader`, `AgentRow`, `AgentIdleRow`); flattening the tree into lazy rows lives in `agentTreeEntries` in `feature:agents` (`AgentsTree.kt`, unit tested). The "Agents" section of the debug component catalog and the previews in Settings › Interface style use these same components and look like the real list (moved from `feature:agents` on 2026-11-02, replacing the old components that put a backing panel under every level).

- The page background is one continuous surface, with no backing panels that "are not business cards but split the background into pieces": none of the three tree levels has a backing panel. Only business cards (pending request cards, connection notices; Board's strips, each representing an Agent) float above the background.
- Hierarchy comes from indentation, type size/weight and thin lines:
  - Computers stand out most: bold `title`, preceded by a collapse arrow and device icon (server / desktop / laptop, from the hub's `icon`), a small online dot after the name ("Offline" only when offline), "N spaces, M sessions" on the next line, state counts and a gauge button for machine details on the right. A thin line separates computers (a 2dp ink line in Board).
  - Spaces come next: one level of indentation (22dp), `label` size, collapse arrow, pinned / muted marks, state counts, `⋯` menu (open space, mark all read, pin / mute / rename space). The former separate "›" was merged into the menu.
  - Session rows are compact: the state mark aligns with the space arrow; the title starts after the arrow. First line: title (bold for Needs you and Unread) + Agent type (only Codex / compatible Codex forks) + time. Second line: colored state text + latest line. A third line appears only for facts such as loops, background work, drafts and pending sends. Rows in the tree no longer repeat machine and space (the headers already say them); rows outside the tree in pinned, by-state and history sections show "machine / space" in the facts line. Glass and Brief rows sit directly on the background, with a thin line between rows starting at the title; Board keeps the flight strip (left state color block + glyph), compressed to two lines with 6dp between strips.
- Idle spaces (all their sessions are Seen) no longer go into a separate "Idle spaces" group: they stay under their computer, after active spaces, collapsed by default, with the name in `callout` and `ink2` and "N sessions" on the right; search expands them automatically.
- When viewing only one computer (its chip is selected), the tree does not draw that computer's header (the title and chip already identify it); spaces move to the first level.
- By repository (#2252, the default if grouping has never been chosen, matching iOS #2161; web also has this grouping): computer → repository → worktree → session, using the same components. Repositories and worktrees both use `AgentSpaceHeader`, distinguished by a leading small icon (`icon`: folder for repository, house for the main worktree, fork for other worktrees) and one extra indentation level; repositories with only the main worktree omit the worktree level and hang sessions directly under the repository. Inactive repositories behave like idle spaces: placed last, collapsed by default, labeled "N sessions"; inactive worktrees also collapse but keep their position (main worktree first, others by name). TalkBack reads the level: arrows say "expand / collapse repository (main worktree, worktree)", icons read the level name, and headers include "expanded / collapsed" (`kind` in `AgentSpaceHeader`). The computer header's second line says "N repositories, M sessions". Flattening still uses `agentTreeEntries`: a worktree group's `under` points to its repository, and collapsing a repository skips its worktrees.
- Several Agents in one herdr tab (#2515, only when grouped by machine): beneath the space comes a tab row (`AgentTabRow`, drawn like a session row: the most urgent Agent's state mark, tab name + its title, state text and latest line, trailing "N agents" + collapse / expand arrow). Each Agent appears below, indented one level; a `JTag` "Lead" follows the initiator's title, and panes it branches off indent one more level, with a thin line descending from the lead's state mark (`AgentRow(branch = true)`; in Board it descends from the center of the strip's color block). When collapsed, the tab row represents the group. Details in `docs/android.md` ("Several Agents in the same tab").
- At the top of the page, in order: search field, computer chips (All machines + each computer: online dot at the device icon's corner, name, count, horizontally scrollable; tapping the selected computer again returns to all), quick filters. A chip's count is how many sessions the list would show when selected (following scope, search and quick filters).
- Machine monitoring: "Machine monitor" in the "List settings" panel opens the machine list; the gauge button in a computer header opens that computer's details.

## 3. App shell

`app/…/ui/Root.kt`:

- Four fixed bottom tabs: Persona / Agents / Inbox / Settings, in the same order in all three styles, each with an icon and an always-visible label; the dock / bottom bar follows each style (`AppNavigation`).
  Settings is the fourth tab, not an entry hidden elsewhere; there are no unlabeled buttons.
- Each tab has its own back stack and badge. Visited tabs stay composed: switching away only stops placement (no drawing or touch), and disables back handling for that tab
  (`rememberNavigationEventDispatcherOwner(enabled = false)`). Switching back leaves the page exactly as it was, with no recreation, reload, empty-state flash or spinner.
- The dock belongs to each tab's first page (`NavEntryDecorator` wraps the root page); pushed pages (chats, documents, secondary settings) cover it and have no dock.
  On back, the dock appears with the root page, never first on the chat page being dismissed. In Glass each page samples its own content, never blurring another page.
- Transitions: on push, the entire opaque new page slides in from the right with the old page underneath; on back, the top page slides away to reveal the one below. No crossfades (two translucent pages overlap their text);
  tab switches do not animate.
- Back on the first page of Agents, Inbox or Settings goes to the Persona tab; only another back on Persona home exits the App (requested 2026-11-01: back to Persona, then back to exit).
  With a chat open in Persona, back first goes to Persona home (handled by the assistant page itself), then another back exits.
- Glass's floating dock floats over the root page and reports its height through `LocalChromeInsets`; `JPage` / `JListPage` automatically adds that height to content padding. Brief and Board bottom bars occupy their own space.
- There is only one background layer (requested 2026-11-01: one continuous background, divided by business cards above it): the shell, every page and detail placeholders draw `PageBackdrop`; Glass's status light is laid out across the whole window,
  so pages draw identical pixels at the same position. There are no seams between panes or between pages and embedded pages, and no separate backing panels; session cards, message cards and settings groups provide separation.
  In Glass, the top bar, input field and dock float over this background, and scroll edges wash toward it (section 1.2). Brief is white paper throughout, with a paper bottom bar and thin line; Board is board grey throughout,
  with white flight-strip cards; its bottom bar, input area and page-bottom action bar are also board grey with a line (the input field itself is a white card), no longer an entire white panel.
- The shell supplies `LocalAmbience` based on global state (waiting for you → amber; an Agent running → blue; cannot connect → red), which Glass uses for its background light. Each page shows connection state in its own title area.
- Status light motion (requested 2026-11-03, #959): each state-colored light slowly drifts near its resting position (12% of the short edge), with slight changes in size and brightness,
  and its color leaning a little toward a neighboring state color (blue ↔ mint, amber ↔ red). Roughly every two minutes a small blob splits off, swims toward the screen center, and later swims back to merge with the main light;
  small blobs of different colors blend into a transition color when they meet in the center. Periods range from tens of seconds to minutes and are not multiples of each other, so no loop is apparent. Unit tests (`AmbientLightTest`) bound the speed
  (no more than 5% of the short edge per second) and the small blobs' fades. The implementation is a pure function of absolute frame time (`Pool.blobs`), so adjacent panes, scroll edges and nested pages stay aligned even when each draws a copy;
  it redraws only 12 times per second (the light is too slow for visible steps, and each background change requires the glass above to blur again). With system animations disabled (reduced motion) or battery saver on, the light stays at its resting position,
  matching the pre-change behavior; `android.sh shots` sets animation scale to 0, so batch screenshots remain static.

## 3.1 Tablets and foldables

Layout depends only on the window, not the device model: the shell uses material3-adaptive's `currentWindowAdaptiveInfo()` (WindowSizeClass plus posture from Jetpack WindowManager's FoldingFeature)
to calculate a `WindowLayout` for the design system; pages do not measure their own width. Split-screen, freeform windows, rotation, folding and unfolding only recompose
(`MainActivity` declares `configChanges`, without recreating the Activity); the current tab, open session, scroll position and input draft survive.

| Width | Navigation | List and detail |
| --- | --- | --- |
| compact (phone, folded foldable) | Bottom dock, four labeled tabs | One pane |
| medium (portrait tablet, unfolded portrait foldable) | Bottom dock (under the list pane) | Two panes at width ≥ 760dp, otherwise one |
| expanded (landscape tablet, unfolded landscape foldable) | Left navigation rail, the same four tabs, rendered per style | Two panes |

- Expanded uses a left rail: initially our choice on the user's behalf (the explicit request was to retain the bottom dock for compact / medium); on 2026-11-01 the user confirmed keeping it. To return to a bottom dock later, change only the `expanded` branch in `Root.kt`.
- Two panes use Navigation 3's list-detail scene (`rememberListDetailSceneStrategy`); the shell centrally assigns list and detail roles (`paneMetadata` in `Root.kt`):
  Agents' "computer → space → session" tree | chat (terminal takeover also in the right pane); Inbox list | Lark conversation / email; settings categories | settings subpage (all settings subpages therefore use navigation).
  Persona's main chat does not split. An unselected right pane shows a placeholder.
- Panes share the shell's single background layer (section 3): the left rail has no panel of its own (Glass no longer has a floating capsule column; Brief and Board have no vertical line or white backing either).
  Only an 8dp gap (`horizontalPartitionSpacerSize`) separates panes, with no divider line or second backing layer; right-pane placeholders and the wide-screen Persona chat list draw the same background.
- In two-pane mode, the item currently displayed on the right is highlighted in its card on the left (`LocalDetailKey` + the component's `selected`), calculated by the shell from the route (`detailOf`). Left-pane taps, deep links, notifications, debug entries and back all stay synchronized; switching between one and two panes uses the same back stack and retains selection.
  Back in two-pane mode first closes the right detail, returning to "list + placeholder"; another back returns to Persona. The list-detail scene must use `PopUntilContentChange`:
  the default `PopUntilScaffoldValueChange` waits for the pane layout to change, but after closing details the placeholder remains and the layout is unchanged, so it empties the entire back stack and one back exits the App (fixed 2026-11-01).
- The divider between panes is draggable (`PaneDivider`, `app/…/ui/PaneDividers.kt`): the left pane tracks the finger, gives a light haptic when crossing a flat fold, and snaps within 28dp on release; minimum widths are 280dp left and 320dp right; before any drag the left is about 38% (320–420dp, leaving 400dp on the right if possible); each tab remembers a width ratio; dragging all the way left hides the Agents list while something is open beside it. A separating fold fixes the split with no handle. Details in `docs/android.md` ("Large-screen two-pane layout").
- The Agents space page is detail (`SpaceRoute`): on wide screens it opens to the list's right rather than covering both panes.
- In landscape, the shell leaves space for left/right safe areas (camera cutout, side navigation bar); the background still fills the window.
- Reading width: `pagePadding` includes `LocalReadingInset`; content in wide panes is centered and capped at `ReadingWidth` (760dp), for chats, long articles and lists alike;
  bottom overlays such as input fields and key bars share that limit. Images and tables lay out within the content width.
- Foldable book posture (vertical half-fold): the hinge separates panes, and content and buttons do not sit over it (the list-detail scene handles it through `HingePolicy`).
- Tabletop posture (horizontal half-fold): pages with bottom overlays (chat, terminal) place content above the hinge and the input field / key bar below (`PageFrame`).
- Keyboard, mouse, trackpad: session, message and settings-row long-press menus also respond to right-click; hover has feedback; lists support the wheel; the software keyboard does not open by itself on launch.
- Glass blurs a larger area on large screens (each pane's top bar and scroll edges); performance has not been measured.

## 4. Screenshots and verification

- Check screenshots of every page in all three styles, dark and light; debug launch arguments `debug.style` and `debug.theme` select them directly.
- Readability floor (`StyleContrastTest` in `core:designsystem`, WCAG contrast, with translucent Glass surfaces composited over the page background first): body text (ink, ink2) on pages, cards, panels, light and dark surfaces ≥ 4.5; primary-button text, text on the highlighter, badge numbers ≥ 4.5; accent color used as a link ≥ 3; text in your own bubbles ≥ 4.5. No exceptions to these. Status text, tertiary text (ink3), text on state color blocks and Board's block glyphs also have thresholds, but 18 places currently fall slightly short (mainly green "done" text, grey offline text and ink3 in light mode, and white text on destructive buttons in dark mode), recorded in the test's `known`; this list may only shrink. Remove the corresponding entries when changing the palette.
- When checking Glass, look for: legible text under overlays, content obscured by the dock or input field, refraction breaking text behind it into fragments, legible text showing through gaps between overlays, and grey bands in the status-bar and gesture-bar areas of full-page overlays.

## 5. Performance

Measured on a test phone; see `docs/android.md` ("Interface style performance").
