# Web color inventory and acceptance (#6183)

Baseline: `b004188f0e58d249d204bd5765e0d37e2e0433b2` (2026-11-07). Scope: `web/` and its design documentation only. No Apple changes, deployment or release.

## Changed and retained

| Category | Surfaces / entry points | Decision and reason |
| --- | --- | --- |
| Routine approvals | `views/chat/Interactions.tsx`: permissions, plans, questions, terminal dialogs, MCP forms, folded cards, next-waiting row | Neutral card and footer; pending labels and dots use semantic amber. Allow / Approve stay primary, alternatives secondary / text. Field-validation errors remain red. |
| Needs-you summaries | `views/Home.tsx`, `views/Sidebar.tsx`, `views/ShellSwitch.tsx`, `ui/atoms.tsx`, `ui/StatusMark.tsx` | Red exclamation disks and filled counts become neutral small labels / amber marks. Header, sidebar, automatic groups, folded tallies, phone shell and StatePill share the same attention meaning. Working and completed pills also use neutral fills. |
| Task and review states | `lib/todos.ts`, `lib/supervision.ts`, `views/Todos.tsx`, `views/assistant/Tasks.tsx`, `views/assistant/Supervision.tsx`, `views/assistant/parts.tsx` | Needs-you, pending review, questions and task-dock marks use amber / brand emphasis on neutral fills. Failed task starts, interrupted runs, failed review criteria and destructive remove actions retain red text / icons. |
| Chat notices | `views/chat/ChatView.tsx`, `views/chat/AlwaysPermissions.tsx`, `views/chat/FailureNote.tsx`, `views/chat/Lines.tsx`, `views/chat/Activity.tsx`, `views/chat/Steps.tsx`, `views/chat/WorkflowCard.tsx`, `views/assistant/Readiness.tsx`, `views/assistant/AssistantChat.tsx` | Attention banner becomes neutral with a thin amber leading border. Background activity, retry / startup failures, readiness, recaps and workflow notices inherit neutral legacy tint tokens. Failure icons / text remain red; restart-needed permission notices become amber. |
| Inbox and messages | `views/inbox/InboxList.tsx`, `views/inbox/InboxParts.tsx`, `views/messages/Messages.tsx`, `views/inbox/ConnectorThread.tsx`, `views/inbox/LarkSummaryCard.tsx` | Waiting-agent tiles, priority tags and mentions become amber details. Closed threads are neutral; change requests use amber. Repository labels keep only their small data-color dot. Own connector messages become neutral; quote and attachment child foregrounds / focus use readable semantic tokens too. Gift-card decoration uses brand color instead of error red. |
| Message bubbles | `styles.css`: `--user-bubble`, `--neutral-bubble` | Neutral fills replace repeated blue blocks; alignment and existing sender/action semantics remain unchanged. |
| Extension installation consent | `views/settings/ExtensionMarket.tsx`, `admin/pages/ExtensionMarket.tsx`, `admin/pages/Extensions.tsx` | Install / upgrade consent, including added permissions, uses one primary action and amber notice details, not a red approval action. Destructive uninstall, cascade-disable and unsafe network changes remain red actions. |
| Health / ordinary labels | `views/health/Health.tsx`, `views/health/HealthReports.tsx`, `views/Settings.tsx` | Health artwork / report glyphs use neutral or brand detail; upload is secondary. Hidden launcher tags are neutral, not error badges. Report and memory deletion remain destructive. |
| Near-full context | `views/chat/ContextMeter.tsx`, `views/Sidebar.tsx` | Amber caution, including at 90% used: nearing automatic compaction is not a failed operation. Numbers, thresholds and explanations are unchanged. |
| Shared colored status fills | `styles.css`, `ui/extension-theme.ts` | Legacy red / orange / green / AI tints are neutral inset fills; accent tint is the neutral selected surface. The same applies to selected extension themes, covering banners, admin notices, settings, onboarding, machine status and loading/error displays without per-page literal recoloring. |
| Danger controls | `ui/atoms.tsx`, `views/chat/Composer.tsx`, `views/inbox/LarkExtras.tsx` | Keep delete / stop / revoke red. Solid danger fill is separate from readable dark-mode red text; white foreground reaches AA. Solid brand actions use readable accent against the surface foreground. |
| Error status glyphs | `views/assistant/TaskEditor.tsx`, Home completion glyphs | Keep small semantic error/success icons, remove colored disks; this avoids white-on-light-status contrast failures in dark mode. |
| Code / data colors | File comparisons, code review, terminal output, image markup, source logos, charts | Retain content meaning rather than recoloring data as an approval. Added/deleted code uses dedicated diff tokens, not neutralized status tints. Search and selection remain distinct neutral highlights / outlines. |

## Verification

Local results: **505 web test files / 4678 tests passed**; production build and lint passed; documentation checks **11 passed**; staged privacy scan and diff whitespace check clean. The production-preview browser check printed `PASS visual-noise browser verification` for both appearances, including actual Allow and Approve clicks. The build retains its existing large-chunk warnings; they are not test failures.

- `cd web && npm test`: full suite, including approval, task, sidebar and theme regressions.
- `cd web && npm run build`: TypeScript and production bundle; prebuild runs the privacy scan.
- `python3 scripts/test/test_doc_paths.py`: documentation links and index.
- `python3 scripts/privacy_check.py`: staged source and collaboration-text scan before push / merge.
- `python3 web/e2e/visual_noise.py <local preview URL> --phase before|after --shots <output directory>`: actual browser app against synthetic HTTP/WS fixtures, 1280 × 1100, fixed time, English, light / dark. Opens Overview, permission and plan cards, attention banner and an Inbox quote with file metadata; captures both phases; clicks Allow and Approve and verifies their unchanged request bodies. Checks no error-color class appears in routine-work fixtures and attention fill is neutral.
- `ui/theme-contrast.test.ts`: every semantic text color reaches 4.5:1 on page / canvas / surface / inset / hover / hover-2 / field; danger and primary action foregrounds reach 4.5:1. Shapes and words remain, so color is not the only signal.

The PR contains light/dark comparison sheets plus full-resolution before/after images. All content is synthetic (`Demo computer`, `/demo/project`, generic demo change and test command). No live hub, account, person, message or private address is used.

CI is disabled for this repository; local verification is the merge evidence. Production verification is deliberately not run: the task prohibits deployment and release.

## Complete baseline source index

The scan covered red semantic uses and large-fill candidates (`bg-red/orange/green/accent/ai`, including tints), excluding tests: **161 files, 680 matching source lines**. A hit is not automatically a large fill: this index includes small dots, icons, thin bars and error text. Baseline line numbers locate every hit; the category table above states what changes and what remains. Runtime theme colors are also covered by `ui/extension-theme.ts`.

| Source file | Baseline lines | Classification |
| --- | --- | --- |
| `web/src/admin/Shell.tsx` | 309, 310 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/admin/pages/Assistant.tsx` | 62, 159 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/admin/pages/Auth.tsx` | 114, 165 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/admin/pages/Devices.tsx` | 131 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/admin/pages/ExtensionMarket.tsx` | 100, 122, 217, 313, 379, 413, 445 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/admin/pages/ExtensionRunning.tsx` | 27, 87, 107, 281 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/admin/pages/Extensions.tsx` | 207, 225, 273, 424, 431, 434, 447, 487, 537, 583, 598 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/admin/pages/Machines.tsx` | 122, 194, 233, 288, 446, 459, 602, 726 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/admin/pages/Network.tsx` | 124, 157, 159, 248, 300 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/admin/pages/Overview.tsx` | 94 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/admin/pages/Peers.tsx` | 152, 173, 191, 279, 285, 315, 321, 451, 470 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/admin/pages/UsageSources.tsx` | 118, 253, 259 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/admin/parts.tsx` | 15, 16, 17, 18, 22, 121, 137, 144, 145, 146, 147, 150, 247 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/lib/attention-signals.ts` | 38, 55 | Semantic mapping / protocol or terminal data |
| `web/src/lib/connection.ts` | 41 | Semantic mapping / protocol or terminal data |
| `web/src/lib/extension-ui.ts` | 849 | Semantic mapping / protocol or terminal data |
| `web/src/lib/format.ts` | 76 | Semantic mapping / protocol or terminal data |
| `web/src/lib/terminal.ts` | 240, 249, 257 | Semantic mapping / protocol or terminal data |
| `web/src/screen/ScreenView.tsx` | 278, 309 | Neutral UI fills; small semantic/content marks retained |
| `web/src/styles.css` | 30, 31, 82, 83, 128, 129, 182 | Neutral UI fills; small semantic/content marks retained |
| `web/src/ui/AssistantCard.tsx` | 324, 329 | Neutral UI fills; small semantic/content marks retained |
| `web/src/ui/ChannelMark.tsx` | 19 | Neutral UI fills; small semantic/content marks retained |
| `web/src/ui/Mermaid.tsx` | 78 | Neutral UI fills; small semantic/content marks retained |
| `web/src/ui/ModelPicker.tsx` | 138 | Neutral UI fills; small semantic/content marks retained |
| `web/src/ui/RecommendedTag.tsx` | 5 | Neutral UI fills; small semantic/content marks retained |
| `web/src/ui/TriageRuleField.tsx` | 29 | Neutral UI fills; small semantic/content marks retained |
| `web/src/ui/atoms.tsx` | 79, 80, 81, 165, 168, 179, 183, 228, 633, 672, 700, 701, 1047 | Neutral UI fills; small semantic/content marks retained |
| `web/src/ui/list-metrics.ts` | 9 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/Home.tsx` | 180, 212, 260, 367, 419, 425 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/Hubs.tsx` | 105 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/LinkTodoDialog.tsx` | 182, 239 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/Login.tsx` | 98 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/Machines.tsx` | 96, 173, 174, 187, 215, 217, 222, 332 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/NewAgent.tsx` | 318, 424, 514 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/NewWorktree.tsx` | 107, 122, 154 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/Palette.tsx` | 323, 324, 432 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/Settings.tsx` | 295, 376, 385, 493, 636, 663, 681, 692, 873, 886 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/ShellSwitch.tsx` | 57 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/Sidebar.tsx` | 1098, 1137, 1241, 1247, 1416, 1422, 1796, 1852, 1942, 2056, 2084, 2212, 2235, 2318, 2351, 2431 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/SpacePage.tsx` | 29, 30, 31, 129, 134 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/StartingAgent.tsx` | 45 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/TodoStageBar.tsx` | 19, 20, 33 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/Todos.tsx` | 431, 532, 571, 585, 641, 762, 763, 764, 885, 967, 983, 1121, 1697 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/UsageBars.tsx` | 11 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/WorkspaceBoard.tsx` | 107, 108, 112, 117, 175 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/agents/AgentsView.tsx` | 89, 233, 238, 244, 269 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/agents/HerdrNotices.tsx` | 68 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/agents/PagedSessionList.tsx` | 190, 199 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/assistant/AssistantChat.tsx` | 267, 336 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/assistant/History.tsx` | 217, 279 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/assistant/Persona.tsx` | 418 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/assistant/Readiness.tsx` | 121, 128, 155, 174, 176 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/assistant/Routines.tsx` | 245, 334, 340 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/assistant/Supervision.tsx` | 14, 15, 16, 17, 37 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/assistant/TaskEditor.tsx` | 169, 171, 251, 273, 274, 292, 372, 374, 416, 436, 442, 482, 512, 542, 558 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/assistant/TaskPanel.tsx` | 68 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/assistant/Tasks.tsx` | 159, 167, 269, 340, 350, 425, 490, 677, 678, 723, 739, 740, 801, 844, 846, 847, 850, 855, 938, 957, 1003, 1075, 1090, 1120 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/assistant/parts.tsx` | 29, 60, 73, 91, 92, 94, 95, 121, 122, 123, 124 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/ActionRow.tsx` | 194, 214, 242 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/Activity.tsx` | 41, 47, 107, 118, 187, 188, 189 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/AlwaysPermissions.tsx` | 57, 58 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/ChatSearch.tsx` | 152 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/ChatView.tsx` | 955, 1276, 1290, 1522, 1554, 1560 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/CommentCard.tsx` | 27, 110, 194 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/CommentMarks.tsx` | 204 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/Composer.tsx` | 804 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/ContextMeter.tsx` | 14, 15 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/FailureNote.tsx` | 22, 24 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/FullText.tsx` | 48 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/Interactions.tsx` | 43, 506, 671, 811, 838 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/Lines.tsx` | 78, 117, 209, 210, 262, 264, 270 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/Messages.tsx` | 101 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/ModelEffortPopover.tsx` | 210 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/OutboxBubbles.tsx` | 17 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/QueueStrip.tsx` | 92 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/Quote.tsx` | 33 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/SessionDialogs.tsx` | 94, 361 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/StepDetail.tsx` | 91 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/Steps.tsx` | 116, 135, 136, 138, 215 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/SubagentCard.tsx` | 89, 99 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/Ultracode.tsx` | 69 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/chat/WorkflowCard.tsx` | 52, 53, 54, 55, 56, 60, 61, 62, 155, 207, 313, 320, 369, 373, 401 | Attention details; neutral status fills; actual errors retained |
| `web/src/views/extensions/CodeViews.tsx` | 29, 39, 197, 201, 217, 291, 436 | Neutral UI fills; content/diff colors retained |
| `web/src/views/extensions/DiffReview.tsx` | 248, 273, 429, 459, 467, 474, 496, 699, 1010, 1022, 1044, 1077, 1132, 1142 | Neutral UI fills; content/diff colors retained |
| `web/src/views/extensions/ViewBody.tsx` | 31, 32, 268, 369, 506, 537, 548, 582 | Neutral UI fills; content/diff colors retained |
| `web/src/views/health/Health.tsx` | 113, 121, 130, 192 | Neutral/brand decoration; deletion remains destructive |
| `web/src/views/health/HealthReports.tsx` | 45 | Neutral/brand decoration; deletion remains destructive |
| `web/src/views/inbox/ComposeDialog.tsx` | 142 | Neutral messages/tags; attention and error details separated |
| `web/src/views/inbox/ConnectorThread.tsx` | 139, 299, 305, 383, 384, 385, 493, 535, 546 | Neutral messages/tags; attention and error details separated |
| `web/src/views/inbox/InboxBodies.tsx` | 79, 210, 268, 293 | Neutral messages/tags; attention and error details separated |
| `web/src/views/inbox/InboxDialogs.tsx` | 94, 153, 265, 277, 284 | Neutral messages/tags; attention and error details separated |
| `web/src/views/inbox/InboxEvent.tsx` | 64, 146, 265 | Neutral messages/tags; attention and error details separated |
| `web/src/views/inbox/InboxList.tsx` | 160, 193, 398, 449, 462, 468 | Neutral messages/tags; attention and error details separated |
| `web/src/views/inbox/InboxParts.tsx` | 45, 91, 133, 156 | Neutral messages/tags; attention and error details separated |
| `web/src/views/inbox/LarkBraveClimb.tsx` | 56 | Neutral messages/tags; attention and error details separated |
| `web/src/views/inbox/LarkComposer.tsx` | 220 | Neutral messages/tags; attention and error details separated |
| `web/src/views/inbox/LarkExtras.tsx` | 29, 90, 144, 145, 146, 147, 148, 149, 150, 151, 306, 344, 364, 440, 449 | Neutral messages/tags; attention and error details separated |
| `web/src/views/inbox/LarkSummaryCard.tsx` | 156, 157, 158, 159 | Neutral messages/tags; attention and error details separated |
| `web/src/views/inbox/LarkThread.tsx` | 512, 541, 543, 582, 1038, 1042, 1072, 1081, 1137 | Neutral messages/tags; attention and error details separated |
| `web/src/views/inbox/LarkTopicCard.tsx` | 122, 316 | Neutral messages/tags; attention and error details separated |
| `web/src/views/inbox/PolicyDialog.tsx` | 130 | Neutral messages/tags; attention and error details separated |
| `web/src/views/inbox/TriageBanner.tsx` | 16 | Neutral messages/tags; attention and error details separated |
| `web/src/views/knowledge/KBDetail.tsx` | 252 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/knowledge/KBDialogs.tsx` | 141, 219, 225, 265 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/knowledge/KBList.tsx` | 187, 400, 418 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/knowledge/KBParts.tsx` | 49, 50, 51 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/link/ContextChips.tsx` | 23, 34 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/link/LinkPanel.tsx` | 272, 277, 386 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/machines/AddComputer.tsx` | 204, 238, 244, 260, 265, 290, 389, 412, 455 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/machines/BotmuxSwitch.tsx` | 58, 61 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/machines/BrowserLogins.tsx` | 110 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/machines/BrowserSection.tsx` | 207, 222 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/machines/ComputerSection.tsx` | 209 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/machines/EngineHostsSection.tsx` | 94, 105, 127 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/machines/EngineSection.tsx` | 103, 112, 123 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/machines/MachineDeploy.tsx` | 35 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/machines/MachineDetail.tsx` | 201, 202, 236, 244, 248, 332, 367, 532, 542, 566 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/machines/NodeHealth.tsx` | 30, 31, 32, 36, 122, 132 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/machines/UsageSection.tsx` | 264 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/messages/Messages.tsx` | 229, 262, 298, 301 | Neutral messages/tags; attention and error details separated |
| `web/src/views/session/GitPanel.tsx` | 212, 249, 285, 453 | Neutral UI fills; small semantic/content marks retained |
| `web/src/views/session/files/CompareView.tsx` | 95, 110, 114, 166, 211, 221, 235, 238 | Neutral UI fills; content/diff colors retained |
| `web/src/views/session/files/DiffLines.tsx` | 19, 20, 109 | Neutral UI fills; content/diff colors retained |
| `web/src/views/session/files/FileOps.tsx` | 334, 347, 389, 521, 545, 607 | Neutral UI fills; content/diff colors retained |
| `web/src/views/session/files/FileView.tsx` | 414, 416, 539, 760 | Neutral UI fills; content/diff colors retained |
| `web/src/views/session/files/HtmlPreview.tsx` | 52 | Neutral UI fills; content/diff colors retained |
| `web/src/views/session/files/NotebookView.tsx` | 55, 125, 139 | Neutral UI fills; content/diff colors retained |
| `web/src/views/session/files/SheetView.tsx` | 296, 301, 324, 366 | Neutral UI fills; content/diff colors retained |
| `web/src/views/session/files/parts.tsx` | 38, 39, 41 | Neutral UI fills; content/diff colors retained |
| `web/src/views/session/terminal/DrawnRows.tsx` | 44 | Neutral UI fills; content/diff colors retained |
| `web/src/views/session/terminal/FindBar.tsx` | 80, 95 | Neutral UI fills; content/diff colors retained |
| `web/src/views/session/terminal/FullTerminal.tsx` | 44, 45, 47, 49, 207 | Neutral UI fills; content/diff colors retained |
| `web/src/views/session/terminal/KeyBar.tsx` | 42, 45 | Neutral UI fills; content/diff colors retained |
| `web/src/views/session/terminal/LinkStatus.tsx` | 15 | Neutral UI fills; content/diff colors retained |
| `web/src/views/settings/AgentSettings.tsx` | 176 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/AssistantSettings.tsx` | 121, 342 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/Channels.tsx` | 23, 28, 95, 117, 199, 221, 240, 441 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/ClaudeDirs.tsx` | 76 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/ConnectDevice.tsx` | 79 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/Connectors.tsx` | 101, 105, 114, 194, 196, 283, 343, 396, 398, 400, 431, 456, 465, 483 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/ExtensionAuth.tsx` | 18, 19, 217 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/ExtensionMarket.tsx` | 42, 137, 164, 205, 327, 348, 354, 522, 538, 543, 569, 579, 635, 648, 749, 771, 802, 909, 944, 990 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/ExtensionOperation.tsx` | 31, 47, 60, 77, 92, 129, 137 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/ExtensionView.tsx` | 437, 447, 534 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/Extensions.tsx` | 97, 99, 100, 101, 197, 229, 553, 554, 606, 696, 713, 843, 876, 1007, 1112, 1162 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/FeishuBot.tsx` | 19, 63, 152, 168, 211, 220 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/HubConnector.tsx` | 29, 42, 64, 163, 252, 316, 321, 456, 462, 587, 704, 778 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/HubSettings.tsx` | 100, 115, 174, 192, 237, 243 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/LauncherQuota.tsx` | 48, 143, 147 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/ManagersSettings.tsx` | 18, 66, 72, 101, 257, 282, 340 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/ModelConfiguration.tsx` | 52, 219, 241 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/NotificationSettings.tsx` | 142 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/ProviderFields.tsx` | 22, 102 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/ProviderImport.tsx` | 98 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/ProviderWizard.tsx` | 317, 383, 407, 432, 561, 597, 609, 693, 710, 784, 802 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/SentSettings.tsx` | 123, 169 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/SettingsParts.tsx` | 118 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/ShortcutSettings.tsx` | 146, 183 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/TranslationSettings.tsx` | 131, 152 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/settings/TriageSettings.tsx` | 88 | Neutral notices/rows; errors and destructive controls retained |
| `web/src/views/terminal/TerminalPage.tsx` | 300 | Neutral UI fills; small semantic/content marks retained |
