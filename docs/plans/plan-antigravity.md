# Plan: Antigravity support (issue #281)

Status: step 0, research; **waiting on the maintainer** (see "Points for the maintainer to decide" at the end). Steps 1 and 2 are unclaimed; code changes wait until this is decided.
Once decided, this plan becomes an implementation order; when done, the design parts move into a new Antigravity doc (docs/plans/plan-antigravity.md) and `docs/assistant/assistant.md`, and this file is deleted.

## Conclusions first

- **Antigravity is two things**: the desktop app (Antigravity 2.0 / IDE, sessions in `~/.gemini/antigravity`) and the **Antigravity CLI (`agy`)**.
  Every hook point an external program can use is on the CLI, so this plan only covers the CLI; the desktop app has no public local interface, and only Google's own Remote Control web page can drive it remotely [D7].
- **`agy` has a TUI, so it is an Agent inside herdr, not a runtime**. herdr 0.9.5 already ships detection rules for `agy` (working / permission dialog / question dialog / trust-folder dialog) and an integration hook that only reports the session id [L3].
  But for the terminal-less cases ("start from the phone", "continue after going offline", the assistant), `agy acp` (the ACP server since 1.5.0) is almost a ready-made runtime backend: streaming, resume, interrupt, mid-turn messages, permission requests, question forms and images are all there [D4][C1].
  So the recommendation is **both paths together**, structured like pi (`docs/integrations/pi.md`): `agy` in a herdr pane belongs to herdr; terminal-less sessions belong to a companion-process runtime that runs `agy acp` behind it.
- **For headless use, pick `agy acp`, not `agy -p`**. `-p --output-format stream-json` can stay resident, stream and resume, but has no control messages for interrupting or mid-turn messages, only takes text input, and "soft-denies" every call that needs human approval instead of asking [D3],
  so phone approval could only be hacked in via hooks. ACP has all of these.
- **The biggest risk is the account terms**, not the technology: the official FAQ says that on personal plans "wrapping `agy` in a third-party application" is unsupported, that accessing Antigravity through third-party tools violates the terms and may get the account banned,
  and recommends Gemini Enterprise or AI Studio API keys for third-party integrations [D1][F1]. Persona is a third-party application. Whether to support personal accounts is for the maintainer to decide.

## How the research was done

- Locally: `agy` 1.4.10 (macOS arm64), **read-only**: looked at `--help` and each subcommand's help, the directory layout of `~/.gemini/antigravity-cli/`, the SQLite schema, the field names in `transcript.jsonl`
  (stats fields and tool names; no chat bodies read), and strings in the binary (RPC method names, environment variable names). Did not log in, did not start an agy session, did not run `agy models` (needs network and credentials), did not read token files.
- This local version has no `acp` subcommand (`agy acp --help` prints the main help); ACP was added in 1.5.0 [C1]. So the whole ACP column comes from the official docs and was **not tested**.
- External sources: the official docs on antigravity.google, the CHANGELOG and issues on GitHub, the Google AI developer forum. Source numbers are at the end.

## 1. Integration points

| Point | What it is | What it can be used for | Source |
| --- | --- | --- | --- |
| Interactive TUI (`agy`) | Full-screen terminal UI | Goes in a herdr pane; herdr recognizes its states and dialogs | [L1][L3] |
| Headless (`agy -p`) | One prompt at a time; multiple turns in one process with `--input-format stream-json` | Streaming text, tool call events; resume with `--conversation <id>` | [D3][L1] |
| ACP (`agy acp`, ≥ 1.5.0) | JSON-RPC over stdio, Agent Client Protocol v1, for editors like Zed / JetBrains / Xcode | Create / load / resume / list / close sessions, streaming, mid-turn messages, cancel, permission requests, question forms, images, model and thinking effort, three modes `default` / `accept-edits` / `plan` | [D4][D5][C1] |
| hooks | `SessionStart`, `PreToolUse`, `PostToolUse`, `PreInvocation`, `PostInvocation`, `Stop`; command-type, JSON on stdin, decision on stdout | `PreToolUse` can `allow` / `auto_approve` / `deny` (with `reason`) / `ask` / `force_ask`, and can `overwrite` arguments; `SessionStart` / `PreInvocation` can inject messages | [D6] |
| MCP | `~/.gemini/config/mcp_config.json` (global), `<workdir>/.agents/mcp_config.json` (workdir), and those in plugins | Mount Persona's tools (`ask_user` etc.); `timeoutSeconds: -1` disables the timeout | [D8] |
| Plugins | `agy plugin install`, bundling hooks, MCP, skills, rules, sub-Agents | Install Persona's hooks and MCP in one go; `agy plugin import claude` can import Claude Code plugins | [L1][C1] |
| Rules and skills | `AGENTS.md` / `GEMINI.md` / `.agents/rules/`; `.agents/skills/` | It recognizes the assistant directory's existing `AGENTS.md` and `.agents/skills/` | [D10][D11] |
| Remote Control | `agy --remote-control` / `agy remote-control start`: a reverse tunnel to Google's web console | Only for Google's own web page; no interface for third parties; not used | [D7] |
| Internal language server | The binary contains hundreds of gRPC methods of `LanguageServerService` (`StartCascade`, `HandleCascadeUserInteraction`, `StreamCascadeReactiveUpdates`…), needing `ANTIGRAVITY_LS_ADDRESS` and a CSRF token | Private, undocumented, changes with versions; the forum also says calling the backend directly is unsupported. **Not used** | [L2][F2] |
| Python SDK (`google-antigravity`) | A library for writing your own Agent; only accepts `GEMINI_API_KEY` or Vertex | Unrelated to the `agy` sessions on the user's computer, so not suited to the **Agents** tab; could be a fallback for the assistant, but would need its own tools and permissions rewritten | [D9] |

## 2. Where sessions are stored and what they look like (local 1.4.10)

All under `~/.gemini/antigravity-cli/` (herdr honors `ANTIGRAVITY_CLI_CONFIG_DIR` to change this directory [L3]):

| Path | Contents |
| --- | --- |
| `conversations/<session id>.db` | One SQLite (WAL) per session. The `steps` table has one row per step (`idx`, `step_type`, `status`, protobuf `step_payload` / `metadata` / `permissions`…); there are also `trajectory_meta`, `gen_metadata` and others. The payload is unpublished protobuf; **not read** [L2] |
| `brain/<session id>/.system_generated/logs/transcript.jsonl` | **The chat record, one JSON line per step**; this is the one to read (the hook's stdin `transcriptPath` points to it [D6]). Next to it, `transcript_full.jsonl` is the untruncated full text |
| `brain/<session id>/.system_generated/{steps,tasks,messages,terminals}/` | Full tool output (`steps/<index>/output.txt`), background task logs, sub-Agent messages |
| `.md`, `scratch/`, `.tempmediaStorage/` under `brain/<session id>/` | Artifacts the Agent produces (plans, summaries…), screenshots, images |
| `cache/conversation_metadata.json` | Each session's `summary` (title), `last_modified_time`, `is_internal` |
| `cache/last_conversations.json` | Directory → that directory's most recent session id (used by `--continue`) |
| `presence/<session id>.lock` | One lock file per session, apparently "who has it open"; **meaning not verified** |
| `history.jsonl`, `settings.json`, `log/`, `antigravity-oauth-token` | Input history, settings, logs, login credentials (0600, not read) |

Fields of each `transcript.jsonl` line (from 16 local sessions, about 2600 lines): `step_index`, `type`, `status` (`DONE` / `RUNNING` / `ERROR`), `source` (`USER_EXPLICIT` / `MODEL` / `SYSTEM`), `created_at`,
`content`, `truncated_fields`; `PLANNER_RESPONSE` also has `thinking`, `tool_calls[{name, args}]` and token counts; `GENERIC` has `media[{mime_type, uri}]`, `error`. Mapped to Persona items:

| transcript `type` | Persona item |
| --- | --- |
| `USER_INPUT` | `text` (user) |
| `PLANNER_RESPONSE` | `thinking` + `text` (assistant) + one `tool_use` per `tool_calls` entry |
| `GENERIC` | The `tool_result` of the previous tool call (**no call id**, so it can only be paired by order; needs testing to confirm); one with `media` produces an `image` |
| `SYSTEM_MESSAGE` / `CHECKPOINT` / `ERROR_MESSAGE` | `notice` or hidden (system injections produce no item, `docs/integrations/runtimes.md` ("Content")) |

Tool names must be mapped to the set the app draws: `run_command` (arguments `CommandLine`, `Cwd`) → `Bash`, `view_file` / `read_file` → `Read`, `write_to_file` / `write_file` → `Write`,
`replace_file_content` → `Edit`, `grep_search` → `Grep`, `find_by_name` → `Glob`, `search_web` → `WebSearch`, `read_url_content` → `WebFetch`, `ask_question` (argument `questions`) → question;
argument names are converted per tool (`appToolName` + its own table). The storage format is not publicly documented and the CHANGELOG shows it changed several times (compression, branching, WAL) [C1], so the reader needs corpus tests guarding it.

## 3. Where sessions, messages, tool calls, approvals and questions live, and how to answer them

| Task | `agy` in a herdr pane (TUI) | Session run by `agy acp` | Source |
| --- | --- | --- | --- |
| Session list, titles | herdr reports Agent `agy` and the session id (when `herdr integration install antigravity-cli` is installed); title read from `conversation_metadata.json` | ACP session list, automatic titles | [L3][D5] |
| State | herdr reads the screen: spinner + `esc to cancel` = working; permission / question / trust dialog = needs you | ACP turn start and end | [L3] |
| Chat content | Read `transcript.jsonl` | ACP session updates, or the same `transcript.jsonl` | [L2][D4] |
| Streaming | None (only whole steps written to disk) | ACP streaming | [D4] |
| Sending messages | Typed into the pane via herdr (same as Claude) | `session/prompt`; sending while running = inserted into the current turn | [D5] |
| Interrupt | herdr presses Esc | `session/cancel`: ends running tool calls, drops pending permission requests | [D5] |
| Tool permission | `PreToolUse` hook: the hook asks the phone first; once answered it returns `auto_approve` / `deny`+`reason`, and hand-back-to-computer returns `ask`; or answer the permission dialog by reading the screen | Permission requests go to the client (Allow / Deny); the runtime forwards them as `rt.ask` | [D6][D5] |
| Questions (`ask_question`) | Answer by reading the screen (herdr recognizes the question dialog); whether the hook's `overwrite` can answer on its behalf is **not verified** | ACP elicitation forms | [L3][D5] |
| Plan approval | `plan` mode lists steps before writing code; how to approve it in the TUI is **not yet clear** | Not documented whether it goes through permission requests | [D2] |
| New / continue | A new herdr tab runs `agy --conversation <id>` (the `resume_argv` path pi uses) | `session/new` / `session/resume` (history not replayed) | [D3][C1] |

The same session id works on both sides: a session created over ACP is the same session when the person comes back to the computer and runs `agy --conversation <id>` (same storage). But **what happens when the same session is open in two processes at once is not yet clear**:
`presence/*.lock` looks like a lock; needs testing whether it refuses a second process.

## 4. Is it a herdr Agent or a runtime

Per the spec, anything with a terminal is a herdr Agent. `agy`'s main form is a TUI and herdr already recognizes it, so **`agy` in a pane belongs to herdr**: terminal, keys, screen reading, closing and spaces are all herdr's, same as Claude and Codex.

What is missing is what Claude's hooks provide (session id, permissions on the phone) and node code to read the transcript. The issue wants "as little node-side code as possible", so the suggestion is to split it like pi:

```
 App ── hub ── node ─┬─ herdr host: agy in a pane (Agent "agy", herdr reads the screen for state)
                     └─ runtime.sock ◄── Antigravity companion process: runtime `antigravity`
                          · lists recent agy sessions from disk, provides content from transcript.jsonl
                          · for ones running in a herdr pane, the row carries pane_id and node merges them into one row (same as pi)
                          · not running: when sending from the phone, either resume_argv opens `agy --conversation` in herdr, or it continues in its own `agy acp`
                          · new from the phone, the assistant: run in `agy acp` (terminal-less sessions, source_kind: runtime)
                          · permissions: pane ones go through Persona's PreToolUse hook → companion process → rt.ask; acp ones go through ACP permission requests → rt.ask
```

Differences from pi: pi's extension runs inside the pi process and can send it messages directly; `agy` has no in-process extensions (plugins are only hooks / MCP / skills / rules),
so sending a message to `agy` in a pane can only be typed into the input field via herdr (herdr has `agent.prompt`), and that part needs node's help. As long as the companion process's rows carry the pane, whether node's existing "runtime row + herdr pane merge" can be used as is or needs changes must be confirmed by reading `node/pi.go` and `runtimes.go` before step 1 starts.

## 5. Headless capabilities compared with Codex's table

Compared against the table in `docs/assistant/assistant.md` ("Codex"). ✅ = documented as present; ❓ = not documented or not tested; ❌ = documented as absent.

| Capability | `agy -p --input-format stream-json` | `agy acp` | Source |
| --- | --- | --- | --- |
| Resident | ✅ multiple turns in one process, ends when stdin closes | ✅ editor-style long-lived connection; `--process-per-session` gives each session its own child process | [D3][D5] |
| Streaming output | ✅ `text_delta` in `step_update` | ✅ | [D3][D4] |
| Resume / after restart | ✅ `--conversation <id>` | ✅ `session/resume`, `session/load` | [D3][C1] |
| Stop | ❌ no control messages (`control_request` errors out and exits); only SIGINT to kill the process | ✅ `session/cancel` | [D3][D5] |
| Messages mid-reply | ❌ every input is a new turn | ✅ a prompt sent while running is inserted into the current turn | [D3][D5] |
| Persona MCP tools | ✅ the workdir's `.agents/mcp_config.json` or global config; unconfigured MCP tools need approval by default, add an `mcp(persona/*)` rule to allow them | ✅ same; whether the `mcpServers` passed with `session/new` is honored is not documented (it only says HTTP MCP configured by the editor needs `--process-per-session`) ❓ | [D8][D5] |
| `ask_user` waiting on the phone | ✅ MCP tool calls time out after 180 seconds by default; `timeoutSeconds: -1` disables it | Same | [D8] |
| Its own questions | ❌ decides on its own when headless, does not ask | ✅ elicitation forms | [C2][D5] |
| Command / file-edit approval on the phone | ❌ everything needing approval is soft-denied (a line on stderr + `denied_actions`); only a `PreToolUse` hook can ask the phone | ✅ permission requests go to the client | [D3][C2][D5] |
| Lark write operations through the gate | `PreToolUse` hook matches `run_command`, runs node's `larkCheck`, asks the phone about writes, and a deny returns `deny` + `reason` (the reason goes to the model) | Same (whether hooks also apply in ACP is **not verified**); or, like DSH, use the tools declared by the extension's command gate | [D6] |
| Permission modes | `--mode accept-edits / auto / plan`, or default; `--dangerously-skip-permissions` allows everything | Only `default` / `accept-edits` / `plan`; `auto` is only for personal accounts and needs a sandbox | [D2][C1] |
| Thinking effort | `--effort low…max` (local help goes up to `xhigh` / `max`) | Settable per session | [L1][D5] |
| Images | ❌ input only accepts `text` blocks; anything else errors out and exits | ✅ | [D3][D5] |
| Instruction files | ✅ the workdir's `AGENTS.md` / `GEMINI.md`; it also reads the user's own `~/.gemini/AGENTS.md` etc. | Same | [D10] |
| Extension skills (e.g. the Lark gateway's) | ✅ the workdir's `.agents/skills/`; no "extra skill directory" option | Same | [D11] |
| Memory (shared with Claude / Codex) | Write it into the instructions the way Codex does; agy has its own memory / knowledge (`GetCascadeMemories` in the binary, a `knowledge/` directory), and how to turn it off is **not yet clear** | Same | [L2] |
| Background tasks | Since 1.5.0, without `--print-timeout` it waits for background tasks to finish; has a `schedule` tool | ❓ | [C1][L2] |
| Warm-up | Start the process ahead of time | Start `agy acp` and `session/new` ahead of time | — |

hooks time out after 30 seconds by default, and `timeout` can be set per handler; the docs do not say the maximum or whether a timeout counts as allow or deny [D6]: test this first before waiting on phone answers.
hooks can only live in the workdir (`.agents/hooks.json`), globally (`~/.gemini/config/hooks.json`) or in plugins, and `--settings` does not carry hooks [D6]:
the assistant can keep them in the assistant directory only; for `agy` in a pane to get permissions on the phone, they must be installed globally or packaged as a plugin, which changes the user's own Antigravity config.

## 6. Accounts and models

- **Sign-in methods** [D1][D5]: personal Google account (browser OAuth; over SSH it prints a link and you paste the code back; `agy acp --login` works the same way); Gemini Enterprise / Google Cloud (SSO, needs `aiplatform.googleapis.com` enabled);
  ADC (`gcloud auth application-default login` + `AGY_ADC_AUTH=true`); `GEMINI_API_KEY` (CLI only, and `"modelProvider": "gemini"` must also be set in `settings.json`; the environment variable alone does nothing).
- **Where credentials are stored**: the docs say the system keychain (macOS Keychain, Linux Secret Service / dbus) [D1]; local 1.4.10 also has a 0600 `antigravity-oauth-token` file, which may differ between versions.
  If the hub machine is Linux without a desktop session, the keychain may be unavailable, so an API key or ADC is more reliable (not tested).
- **What running on the hub machine needs**: install `agy` (≥ 1.5.0 for ACP) and sign in once (headless without sign-in fails with `authentication required`; ACP returns `-32000`) [D3][D5];
  `auto` mode on personal accounts needs the terminal sandbox (macOS `sandbox-exec`, Linux unprivileged user namespaces, often disabled in containers) [D2].
- **Models** (docs as of 2026-11) [D12]: Gemini 4 Argon, Gemini 3.8 / 3.7 / 3.6 Flash, Gemini 3.1 Pro, Claude Sonnet / Opus 5.5, Claude Sonnet / Opus 4.6, GPT-OSS 120B;
  the free tier has no Argon or Claude 5.5; Claude 4.6 and GPT-OSS retire on 11-18; Gemini and Claude / GPT quotas are counted separately (weekly + every 5 hours). API keys only get Gemini models; Claude models are available under ADC [C1].
  So Persona's model configuration for this kind of launcher can only list what `agy models` returns, with no gateway address to fill in; `GOOGLE_GEMINI_BASE_URL` can point to a Gemini-compatible endpoint [D1].
- **Launcher detection**: `docs/integrations/launchers.md` currently only recognizes aliases whose last command is `claude` / `codex`; it needs to recognize `agy` too. Users' aliases often carry `--dangerously-skip-permissions` and proxy environment variables, so the assistant has to combine the launcher's own arguments with Persona's, as it does for Codex.
- **Terms** (see "Conclusions first"): the FAQ says `agy -p` is "intended for direct scripting and CI workflows", that wrapping `agy` in a third-party coding agent or application is unsupported on personal plans,
  that accessing Antigravity with third-party software violates the terms of service and may get the account suspended or terminated, and recommends Gemini Enterprise or AI Studio API keys for third-party integrations [D1].
  On the forum on 10-16, someone replying in a company voice said the FAQ takes precedence over earlier forum replies, and that wrapping it in a third-party application on personal plans is "No" [F1]; that account has no official badge.
  ACP itself exists for third-party editors like Zed and JetBrains, but there is no official statement on whether an ACP client counts as a "third-party application" [F1][G2]. GitHub issues asking similar questions have no official answer yet [G2].

## 7. Suggested approach (for discussion; split into steps 1 and 2 once decided)

1. **runtime `antigravity`** (in integrations/antigravity, built on runtime-sdk): a companion process that reads `transcript.jsonl` for the list and content. Terminal-less sessions (new from the phone, continued from the phone, the assistant) run in an `agy acp` it manages;
   permissions, questions, streaming, interrupt and mid-turn messages are forwarded from ACP to the SDK. Minimum version 1.5.0; below that it reads only, never runs, and says so in the list.
2. **`agy` in a pane**: herdr already recognizes it; node recognizes Agent `agy` and merges it with the companion process's row (like pi); messages go via herdr; permissions reach the phone through a Persona Antigravity plugin (a `PreToolUse` hook that asks the companion process).
3. **Assistant**: the companion process declares `caps.assistant` and uses the existing `assistant_runtime.go` (the DSH path); Lark goes through the tools declared by the extension's command gate, and gate commands inside `run_command` are intercepted by the hook.
   That way node gets no Antigravity-specific code. The issue originally said "a separate file, like Codex"; the trade-off between the two is in the next section.
4. Do the parts that do not touch the account terms first: storage reader and corpus tests, mapping, herdr merging; end-to-end tests that really run `agy` use an API key or an Enterprise account.

## Points for the maintainer to decide

1. **Whether to support personal Google accounts** (most important). Options: a) support only `GEMINI_API_KEY` / Gemini Enterprise / ADC; sessions from personal OAuth accounts are read-only, not driven; b) support everything, stating the ban risk in the docs and on the launcher; c) ask Google for a clear answer first.
   Suggest a: only reading the local chat records is not "wrapping it in a third-party application"; driving it is. Under an API key there are only Gemini models; document that difference.
2. **Shape**: a) herdr Agent + companion-process runtime (section 7 above, suggested); b) runtime only, with `agy` in panes shown only as "Not running" chat records; c) herdr Agent only, with Go code in node to read the transcript.
3. **Assistant backend**: a) the companion process declares `caps.assistant` and uses `assistant_runtime.go` (no Go code added; Lark through the tools declared by the extension's command gate; suggested); b) as the issue says, write `assistant_antigravity.go` and have node start `agy acp` directly (symmetric with Codex, one more set of Go code; the Lark gate has to be wired into both ACP permissions and the hook).
4. **Minimum version**: require ≥ 1.5.0 (ACP). Below it, list sessions read-only, or not support it at all.
5. **Whether pane permissions on the phone may change the user's global config**: a) ship an Antigravity plugin that users install themselves with `agy plugin install` (suggested, removable); b) node writes `~/.gemini/config/hooks.json` automatically; c) don't, and only answer permission dialogs by reading the screen.
6. **The same session open on both sides**: if agy does not refuse a second process, messages sent from the phone to ACP and the TUI on the computer would write to the same storage at once. Either always defer to the herdr pane and have ACP never touch a session open in a TUI (suggested), or some other rule.
7. **The Antigravity desktop app is out of scope for this issue**: its sessions live in another directory with no public interface. If agreed, say "Antigravity CLI" in the issue title and acceptance criteria.
8. **agy's own memory**: when the assistant uses Persona's shared memory, whether to turn off agy's own memory / knowledge (how to turn it off still needs looking into).

## To test before step 1 starts

- `agy acp` (1.5.0) method names and message shapes: permission requests, elicitation, `mcpServers` in `session/new`, how pending permission requests are wrapped up on cancel, how plan mode is approved.
- hooks: how large `timeout` can be and what a timeout counts as; whether `ask_question` can be answered via `overwrite`; whether hooks fire inside `agy acp`.
- `presence/*.lock`: behavior when a second process opens the same session.
- `transcript.jsonl`: pairing of `GENERIC` with tool calls, sub-Agent records, whether compaction and branching rewrite it (decides when `history` changes).
- Sign-in on a Linux hub: whether OAuth can persist when the keychain is unavailable.

## Sources

Local (`agy` 1.4.10, herdr 0.9.5, inspected read-only on 2026-11-05):

- [L1] `agy --help`, `agy mcp add --help`, `agy plugin`, `agy remote-control start --help`.
- [L2] Directory layout of `~/.gemini/antigravity-cli/`, schema of `conversations/*.db`, field and tool-name stats of `transcript.jsonl`; gRPC method names and environment variable names in the `agy` binary.
- [L3] herdr 0.9.5's built-in `agy` detection rules (`id = "agy"`, aliases `antigravity` / `antigravity-cli`) and the hook installed by `herdr integration install antigravity-cli` (only reports `conversationId` and `transcriptPath`); `herdr integration status`.

Official docs (antigravity.google):

- [D1] FAQ: https://antigravity.google/docs/faq/ ; sign-in: https://antigravity.google/docs/install/
- [D2] Execution modes: https://antigravity.google/docs/cli/modes/
- [D3] Headless mode: https://antigravity.google/docs/cli/headless/
- [D4] ACP (CLI): https://antigravity.google/docs/cli/acp/
- [D5] ACP (other editors: capabilities, arguments, sign-in methods): https://antigravity.google/docs/ide/extensions/acp
- [D6] Hooks: https://antigravity.google/docs/hooks/
- [D7] Remote Control: https://antigravity.google/docs/remote-control?tab=cli
- [D8] MCP: https://antigravity.google/docs/mcp/
- [D9] SDK: https://antigravity.google/docs/sdk/overview/
- [D10] Rules: https://antigravity.google/docs/rules/
- [D11] Skills: https://antigravity.google/docs/skills/
- [D12] Models: https://antigravity.google/docs/models/ ; CLI reference: https://antigravity.google/docs/cli/reference/

CHANGELOG, issues, forum:

- [C1] https://github.com/google-antigravity/antigravity-cli/blob/main/CHANGELOG.md (latest 1.5.0: `agy acp`, ACP modes, `session/resume`, `--login`; 1.4.11: billing choice under ADC; 1.3.6: Claude 5.5 under ADC)
- [C2] Same, earlier entries (1.1.3 headless soft-deny, 1.1.12 headless no longer stops on questions, 1.1.27 `denied_actions`, 1.2.0 headless auto-passes plan approval)
- [G1] `agy -p` drops output without a TTY (fixed in 1.1.1; stdin is closed when the prompt is passed as an argument): https://github.com/google-antigravity/antigravity-cli/issues/76
- [G2] Terms question on whether an official agent may call `agy -p` via shell / hook (filed 2026-11-02, unanswered): https://github.com/google-antigravity/antigravity-cli/issues/1392
- [F1] Forum: calling the official `agy` from a local subprocess on a personal plan: https://discuss.ai.google.dev/t/clarification-on-consumer-plan-policy-for-invoking-official-agy-via-local-subprocess/189905
- [F2] Forum: external orchestration of headless mode (a 09-15 reply said it was supported, overturned by the 10-16 reply above): https://discuss.ai.google.dev/t/is-external-orchestration-of-antigravity-cli-headless-mode-supported-with-account-based-usage/183051
