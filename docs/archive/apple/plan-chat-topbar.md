# Plan: the iOS Agents chat page's top bar

Status: phase 1 done (2026-11-06, the user asked to build it as the prototype shows). Decisions: Q1 phase 1 only (the goal,
workflow and port rows are unchanged; only the subagent strip became the status line); Q2 the tab capsules keep their
state-colored fill (built first with a neutral fill and a dot as proposed, changed back at the user's request after the
screenshots); Q3 the loop shows only its next time; Q4 the header was replaced in both existing styles, no new style.
Where the build differs from the plan: ended subagents used to be reached from the strip's "All subagents" at its end; with the
strip gone on iPhone and iPad, ⋯ has "All Subagents (N)". BeautifulUI's tab chip sizes itself (`fixedSize`), so a tab name
is cut by length there (`SiblingTabs.capped`). The Mac and hosted headers (#3255) keep `AgentStrip`.

The rest is the plan as written. Scope: the open header of the Agents chat page on iPhone (the full part of `ChromeHeader`).
The folded bar (`compactChrome`), the assistant's `AssistantTopBar`, the web and Android are out of scope.

Interactive prototype: `docs/archive/apple/prototypes/chat-topbar.html`. Open it in a browser: today's header on the left, the plan
on the right, one set of sample data; `#preset=all&theme=dark` opens the "everything" scene in dark mode.

## 1. What was wrong

The user said the bar at the top of the chat looked messy. Their screenshot (a working Agent, two tabs in its space, a loop,
one background task, one subagent running), top to bottom:

```
(‹)      <a long task title, cut>…            (⋯)
      ● Working 5 min · <machine> · <project>
 ● tab-a   (● 31)   (⋯)   (⟳ 13:24)   (⚙ Background 1)
(Main Agent)   ● <the subagent's title, running off the screen>
```

The code: `devChrome` in `apple/Persona/Views/Chat/ChatView.swift`. Row one is `headerBack` / `titleButton` / the menu; row two
is `SiblingTabs` (`apple/Persona/Views/Chats/SpaceView.swift`) plus `ActivityChips(compact: true)`
(`apple/Persona/Views/Session/SessionPanes.swift`); below them, when they apply, `GoalBar`, `workflowBar`, `SessionPortChips`
and `AgentStrip`.

Layout
1. No cap on rows. Under the title row there are five conditional rows (tabs + activity, goal, workflow, ports, subagents):
   six rows when all apply. The screenshot's three already take about a fifth of the screen.
2. Row two's halves fight for width: `ActivityChips` is `.fixedSize()`, its two capsules take about 45% of the row, and the tabs
   are squeezed into the rest, which looks like a wrap gone wrong.
3. Edges do not line up: BeautifulUI's unselected chips have no fill, so their text sits further in than the "Main Agent" chip
   below; `AgentStrip` has its own horizontal and bottom padding.
4. The subagent's title is not truncated; the screen cuts it. The scrolling strips have no fade, so "more to scroll" and "did
   not fit" look the same.

Hierarchy
5. One row mixes three kinds of thing: siblings (other agents to switch to), this agent's activity (loop, background tasks; a
   tap shows details) and the tabs' "all". They look alike (capsules) and each has its own colors, so navigation and status
   cannot be told apart.
6. Two ⋯: the one after the tabs is "every agent in the space", the one top right is the page's menu.
7. "⟳ 13:24" reads as "refreshed at 13:24"; it is the loop's next run.
8. The subagent row has a "Main Agent" chip. It is only useful while reading a subagent, when the composer's place already has
   `SubagentFooter`'s "Back to Main Agent"; otherwise it only takes room and looks like the subagents beside it.
9. State color three times: the subtitle's dot, the current tab's, the subagent's.

Crowding and truncation
10. The title's width is capped at `Theme.Size.box260` (`titleLabel`'s `.scaledFrame(maxWidth:)`), about 40pt less than the
    room between the buttons.
11. Tab names have no maximum width: one long name pushes the current tab out of sight.
12. `ActivityChips`' compact padding is a literal `10` / `8`, its height made to match the menu beside it, not a token.

## 2. Principles

- Three rows at most: navigation, context, at most one status line. Anything more goes into a sheet a tap opens.
- One kind of thing per row: row one is "which conversation", row two is "siblings on the left, this agent's activity on the
  right", row three is "the one thing worth knowing now".
- Fixed places: the same kind of element is in the same place in every state.
- Color only for state: state color only on dots and icons, capsules on `Theme.Fill.chip`; the current one by ring and weight.
  (Q2 kept the tabs' state-colored fill.)
- Same as the folded bar: it already shows the loop and background work as icon + number (`hostedMarks` / `activityMark`).
- Cut, never wrap: every text is one line with a defined truncation and maximum width; the full text is in the sheet and the
  VoiceOver label.

## 3. Design

### Wireframes

Usual case (siblings, activity, one subagent running):

```
┌──────────────────────────────────────────────┐
│ (‹)   <a long title, all the room there is>… (⋯) │ ① navigation
│      ● Working 5 min · <machine> · <project>     │
│ ‹(●31)(● tab-a)(● tab-b)░  (▦ 4)│(⟳ 13:24 ⚙ 1) │ ② context
│ ◐ Subagent  <its title, one line, cut>…       › │ ③ status (optional)
└──────────────────────────────────────────────┘
  ░ = fading edge   (▦ 4) = every agent in the space   │ = fixed gap
```

No siblings, only activity: the activity pill stays at the trailing end.

```
│ (‹)          <title>                     (⋯) │
│              ● Working 5 min · <machine>       │
│                                (⟳ 13:24 ⚙ 1) │
```

Siblings, no activity, no subagent: two rows; row two is only the tabs, full width.

Reading a subagent:

```
│ ◐ Viewing Subagent · <title>… (read-only)      │ ③
  the way back stays SubagentFooter's "Back to Main Agent" at the bottom
```

### ① Navigation row

- Unchanged in structure: round back · title over subtitle (centered) · round ⋯.
- Title: one line, cut at its end. No `box260` cap: it uses the room between the buttons (they are the same width, so it stays
  centered); on the Mac a readable maximum as a new `Theme.Size` token. The full title is in Session info and VoiceOver.
- Subtitle: today's rules (`devSubtitle`: state · machine · place; short of room the machine goes first, then the place).
- No activity marks in this row: one more button there would take the title off center or 44pt from each side.

### ② Context row

Siblings on the left, this agent's activity on the right, a fixed gap (`Theme.Space.m`) between.

- Siblings (`SiblingTabs`):
  - Flat tabs and horizontal scrolling stay (the user's call on 2026-10-31); the current tab is scrolled into view.
  - Both ends fade (`Theme.Size.fadeEdge`) while there is more to scroll that way.
  - Each name is one line, cut at its end, at most a new token (about 120pt, scaled with text size).
  - Proposed: neutral fill, state by dot only (Q2; not taken).
  - The trailing ⋯ becomes the space icon and the tab count, apart from the page's menu; the red dot when another agent needs
    you stays.
- Activity (`ActivityChips`, compact):
  - One pill at the trailing end, `Theme.Size.wellS` tall like the tabs, `Theme.Fill.chip`, tinted icons (loop `Theme.loop`,
    background `Theme.busy`), primary text.
  - Only icons and numbers: the loop's next time "⟳ 13:24", background "⚙ 1", side by side in the one pill; the same icons as the
    folded bar's `activityMark`.
  - No "next": the icon and the VoiceOver label carry it, `ActivitySheet` has the words. Problem 7 can still be misread at
    first sight, hence Q3.
  - `Theme.Space` steps instead of the literal paddings.
- No row when there is nothing for it (as today).

### ③ Status line (at most one)

One thing, the first that holds:

1. Reading a subagent: "◐ Viewing Subagent · <title> (read-only)". The way back is not up here; it stays `SubagentFooter` at the
   composer, in reach of a thumb.
2. Subagents running: one, "◐ Subagent <title>… ›", a tap opens it; several, "◐ 3 subagents running · <the newest's title>… ›", a
   tap opens `SubagentTreeSheet`.
3. None running, some ended: no line; they are reached from the Agent call's "View subagent" and from ⋯.

No "Main Agent" chip: it only helped in case 1, where the footer already has the way back. The title is one line across the
page's margins (`Theme.Space.page` each side). A running subagent shows the work spinner, not a third orange dot.

How the goal, workflow and port rows fold into "at most one" is Q1; phase 1 leaves them alone.

### Truncation

| Element | Lines | Cut | Max width | Full text in |
| --- | --- | --- | --- | --- |
| Title | 1 | end | between the buttons; a Mac token | Session info, VoiceOver |
| Subtitle | 1 | machine, then place, then end | as the title | Session info |
| Tab name | 1 | end | new token, ~120pt × text scale | the "▦ N" list |
| Activity pill | 1 | none (icons and numbers, `fixedSize`) | its own | `ActivitySheet` |
| Subagent title | 1 | end | the row less icon and › | its conversation / the tree |

Every button label is `.lineLimit(1)`. At large text sizes the pill drops the loop's time first; the tabs still scroll.

### Height

At the default text size: the navigation row 44pt plus padding, the context row `wellS` 30pt, the status line about 28pt.
With all three it is about today's three rows; the difference is the cap, six rows before and three after (once Q1 is
decided). The header's height is measured (`ChatChrome.height`, `onGeometryChange`), so folding and the list's margins follow.

## 4. Where the code is

| File | Change |
| --- | --- |
| `apple/Persona/Views/Chat/ChatView.swift` | `devChrome`: row two is siblings (left, the remaining width) + activity (right, fixed), activity trailing also without siblings; the subagent strip becomes the status line; `titleLabel` without `box260`; `sideHeaderRows` (the iPad side column, iPhone Duo on its side) gets the same rows; `activityMark` shared by the open and folded header |
| `apple/Persona/Views/Chats/SpaceView.swift` | `SiblingTabs`: fading ends, the tab name's maximum width, `allButton` as space icon + count |
| `apple/Persona/Views/Session/SessionPanes.swift` | `ActivityChips`' compact form as one pill without literal padding; `HeaderStatusLine`; `AgentStrip` kept for the Mac and hosted headers; `SubagentFooter` unchanged |
| `apple/Shared/Design/Theme.swift` | new tokens: the fade's width, the tab name's maximum, the Mac title's maximum |
| `apple/Persona/DesignSystem/` | the fade as a modifier (`.edgeFade()`), for other scrolling strips too |
| `apple/PersonaUITests/TaskSheetHeaderTests.swift` and others | UI tests that find `chat.title`, `chat.sibling` keep working; the status line has an identifier and tests |
| `docs/apple/ios.md` | the top bar and "Other Agents in the space" paragraphs |

Notes:
- Both styles change: the default liquid glass draws `plainTab` and the rest as before, BeautifulUI uses `BUIFilterChip`; both
  follow the new rules and both are screenshotted. The user asked for this screen to change, so it is not "changing an existing
  screen's look" and needs no new `AppStyle`; if the old look should stay too, it becomes a new style (Q4).
- Hosted pages (a chat in a task's sheet, `host != nil`, #3255) are already one row with `hostedMarks`; unchanged.
- The Mac does not fold its header (`ChatChrome.folds == false`); it gets the same layout and its own screenshot.
- Verification: `TokenRatchetTests`' baseline should go down (the literal paddings are gone); screenshots from the mock hub with
  siblings, loop, background and subagents, light and dark × both styles × default and largest text, plus the iPad side column
  and the Mac.

## 5. Decisions

- Q1 Fold the goal, workflow and port rows into the one status line too (order: reading a subagent > a workflow that needs you
  > subagents running > a workflow running > goal > ports; the rest in a summary sheet)? Recommended: phase 1 first (subagent
  strip → status line, row two rearranged), then decide after the screenshots; those rows have their own designs
  (`docs/integrations/agent-goal.md`, `docs/operations/ports.md`). Decided: phase 1 only.
- Q2 Tab capsules from state-colored fill to one neutral fill with a dot? That would undo the user's 2026-10-31 rule.
  Recommended: change it. Decided (2026-11-06): keep the state-colored fill; the activity pill is neutral with tinted icons.
- Q3 The loop as "⟳ 13:24" or "⟳ Next 13:24"? Recommended: the time only (as the folded bar, narrower). Decided: time only.
- Q4 Replace today's header, or add a new style beside it? Recommended: replace, in both existing styles. Decided: replace.
