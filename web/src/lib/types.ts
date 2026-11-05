// The hub's wire types (docs/protocol.md). Only the fields the web client reads.

export type AgentState = "needs_you" | "working" | "unread" | "idle" | "offline" | "unknown";

/** An engine as the machine's node found it (docs/engines.md): `note` says why one that is there cannot be used. */
export interface DetectedEngine {
  id: string;
  name: string;
  available: boolean;
  version?: string;
  note?: string;
}

export interface Machine {
  id: string;
  name: string;
  online: boolean;
  error?: string;
  hostname?: string;
  os?: string;
  claude_version?: string;
  herdr_version?: string;
  node_version?: string;
  node_mismatch?: boolean;
  deploying?: boolean;
  can_deploy?: boolean;
  deploy_reason?: string;
  deploy_phase?: "waiting_idle" | "transferring" | "restarting" | "confirming";
  deploy_error?: string;
  system?: { name?: string; model?: string; cpu?: string; cores?: number; memory?: number };
  /** The terminal engine its sessions come from (herdr when absent); `engines`: what its node found, empty while offline. */
  engine?: string;
  engines?: DetectedEngine[];
  kind?: string;
  arch?: string;
  home?: string;
  last_seen?: string;
  /** Interconnected hubs (docs/plan-hub-mesh.md 2.6): the hub that owns this computer, and the way there. */
  hub_id?: string;
  hub_name?: string;
  machine_uid?: string;
  route?: { via: string; hops: number; rtt_ms?: number };
  /** Operating it through Cua Driver (the hub's extension cua, docs/computer-use.md): only while the extension is on and it is a Mac. */
  computer?: {
    enabled: boolean; installed?: boolean; daemon?: boolean; ready?: boolean; accessibility?: string; screen_recording?: string;
    desktop?: string; screen_sharing?: boolean; note?: string;
  };
  /** ego lite (the hub's extension ego, docs/plan-ego-browser.md): only while the extension is on and it is a Mac. */
  browser?: { enabled: boolean; installed?: boolean; set_up?: boolean; ready?: boolean; note?: string };
}

/** The hubs of this hub's interconnected group, from the signed-in hello (docs/plan-hub-mesh.md 2.6). */
export interface MeshHub {
  hub_id: string;
  name: string;
  online?: boolean;
  self?: boolean;
  endpoints?: string[];
}

export interface Subagent {
  id: string;
  session_id: string;
  type?: string;
  description?: string;
  running?: boolean;
  tool_use_id?: string;
  /** The subagent that launched it; absent when the session did. One not in the list: shown at the top level. */
  parent_id?: string;
  /** A fork of its launcher's conversation. */
  fork?: boolean;
  background?: boolean;
  started_at?: string;
  updated_at?: string;
  last_text?: string;
}

export interface SessionCapabilities {
  read?: boolean;
  send?: boolean;
  interrupt?: boolean;
  approve?: boolean;
  terminal?: boolean;
  attach?: boolean;
}

/** A session's work state (docs/agent-work.md): working, blocked on a named precondition, or done. */
export interface WorkState {
  phase: "working" | "blocked" | "done";
  /** What a blocked one waits on: user (a decision), agent, external (a deploy, CI, a time), transient (a model error). */
  on?: "user" | "agent" | "external" | "transient";
  /** One line for the lists: what it waits for, or what it concluded. */
  line?: string;
  wait?: string;
  then?: string;
  /** When the hub acts next on its own: 继续 (transient) or a wake (external, agent). */
  wake_at?: string;
  attempts?: number;
  max?: number;
  agent?: { machine_id: string; session_id: string; label?: string };
  link?: { url: string; expires_at?: string };
  artifacts?: { kind: string; ref: string }[];
  interaction_id?: string;
}

/** 设置 › 会话分诊 (GET / PUT /agents/triage): all off until the user turns them on. */
export interface TriageSwitches {
  enabled: boolean;
  auto_continue: boolean;
  wake: boolean;
  decide: boolean;
}

export interface Session {
  /** Node-verified physical working directory; the original cwd stays available for existing session flows. */
  workspace_cwd?: string;
  machine_id: string;
  session_id: string;
  title: string;
  cwd: string;
  status: string;
  starting?: boolean;
  start_error?: string;
  state?: AgentState;
  state_since?: string;
  agent?: string;
  agent_name?: string;
  source_kind?: string;
  source_thread_id?: string;
  source_instance_id?: string;
  parent_thread_id?: string;
  read_only_reason?: string;
  capabilities?: SessionCapabilities;
  attached?: boolean;
  /** What to call an agent the app does not know (a runtime's own name, "DSH"; docs/runtimes.md). */
  agent_label?: string;
  launcher?: string;
  model?: string;
  permission_mode?: string;
  effort?: string;
  assistant?: boolean;
  started_by_jarvis?: boolean;
  space_id?: string;
  space_name?: string;
  space_index?: number;
  slot_id?: string;
  slot_name?: string;
  slot_index?: number;
  /** The host's pane, tab and workspace ids (herdr: bare "w9:p1"; other hosts "<kind>:<id>"). Workspace ids change
   *  across a herdr restart: never key anything stored by them (pins and folds go by 机器/空间名). */
  pane_id?: string;
  tab_id?: string;
  workspace_id?: string;
  /** Which host the space belongs to ("herdr", "orca", a runtime id…): space ids are unique only within one. */
  space_kind?: string;
  updated_at?: string;
  seen_at?: string;
  last_text?: string;
  /** Its latest step when that is a tool call no text came after (「在跑测试」「在读 tasks.go」), and when it started. */
  activity?: string;
  activity_at?: string;
  /** Its latest turn ended in a model API error (the hub sets it while it is stopped, #1516): its task is interrupted. */
  turn_error?: string;
  /** Where it stands in its work (docs/agent-work.md): set by the hub once it triaged a stopped turn. */
  work?: WorkState;
  pending_interactions?: number;
  queued?: string[];
  subagents?: Subagent[];
  loop?: LoopInfo;
  background?: BackgroundTask[];
  rev?: number;
  suggestion?: string;
  /** An assistant conversation moved out of the list (docs/assistant.md「管理自己的对话」). */
  archived?: boolean;
  /** The channels messages came into it from, newest first (docs/channels.md「来源标记」). */
  channels?: string[];
  todo?: { id: string; title: string };
  /** Works (or worked) for a quiet task (#1731): not in the global badge; the main conversation tells about it. */
  quiet?: boolean;
  /** Why it waits for the user, in its host's words (a botmux attention's reason); only while it needs you (docs/engines.md「botmux」). */
  attention_reason?: string;
  /** Where it lives outside Persona (a botmux session's Feishu topic): 在飞书打开. */
  external_url?: string;
  /** The last compaction the user asked for in an assistant conversation (POST …/compact, #1727). */
  compaction?: Compaction;
}

/** A compaction the user asked for (proto.Compaction): running, done or failed (`error` says why). */
export interface Compaction {
  state: "running" | "done" | "failed";
  error?: string;
  started_at: string;
  finished_at?: string;
}

/** Where the context was compacted (proto.Compact): "manual" (/compact) or "auto", the size before it. `notes`: the
 *  notices of the compaction folded into it here (lib/compaction.ts), never from the hub. */
export interface CompactInfo {
  trigger?: string;
  pre_tokens?: number;
  notes?: string[];
}

/** A /loop: Claude re-runs a prompt on its own, scheduling each wakeup ("wakeup") or by cron ("cron"). */
export interface LoopInfo {
  kind: string;
  prompt?: string;
  reason?: string;
  interval_seconds?: number;
  next_at?: string;
  cron?: string;
  ticks?: number;
}

/** A command or subagent running in the background until it reports back. */
export interface BackgroundTask {
  id: string;
  kind: string;
  description?: string;
  command?: string;
  output_file?: string;
  started_at?: string;
}

/** What a background task's notice says (proto.TaskNote). */
export interface TaskNote {
  kind: string;
  status: string;
  title?: string;
  task_id?: string;
  tool_use_id?: string;
  exit_code?: number;
  output_file?: string;
  detail?: string;
}

/** A picture folded into a run of work (proto.Fold.images): the turn-image strip shows it without opening the fold. */
export interface FoldImage {
  /** The image item: its bytes from /sessions/{mid}/{sid}/items/{item_id}/image. Missing: only a path (file/thumb). */
  item_id?: string;
  tool_use_id: string;
  tool: string;
  source: "read" | "screenshot" | "browser" | "mcp" | "tool";
  path?: string;
  machine?: string;
  mime?: string;
  size?: number;
  width?: number;
  height?: number;
}

export interface Item {
  id: string;
  seq: number;
  ts?: string;
  role: "user" | "assistant" | "system";
  kind: "text" | "thinking" | "tool_use" | "tool_result" | "notice" | "image" | "fold" | "recap";
  text?: string;
  tool_use_id?: string;
  tool_name?: string;
  tool_input?: Record<string, unknown>;
  is_error?: boolean;
  full_size?: number;
  queued?: boolean;
  wake?: boolean;
  task?: TaskNote;
  fold?: { items: number; tools: number; thoughts: number; until?: string; through_seq: number; through_id?: string; images?: FoldImage[] };
  image?: { mime: string; data?: string; lazy?: boolean; size?: number; width?: number; height?: number };
  /** Sent by another Claude session (cross-session messaging), not typed by you: its name. */
  from?: string;
  /** The client_msg_id of the message this came from, as the hub tied it (every item of it: the text, its images, a
   *  queued copy, or the notice it became); older hubs never send it (lib/echo.ts). */
  client_msg_id?: string;
  /** This notice is where the context was compacted (Claude Code's compact_boundary). */
  compact?: CompactInfo;
}

export interface ItemsPage {
  items: Item[];
  has_more?: boolean;
  more_after?: boolean;
  more?: boolean;
  history?: string;
  to_seq?: number;
  from_seq?: number;
}

export interface Question {
  question: string;
  header?: string;
  multi_select?: boolean;
  options: { label: string; description?: string }[];
}

export interface DialogOption {
  key: string;
  label: string;
  detail?: string;
  kind?: string;
  /** Ticked at the computer (a multi-select box), or an answered question's pick. */
  checked?: boolean;
  /** What is typed into the free-text field (kind "type_something"). */
  text?: string;
}

/** A terminal dialog as the hub reads it. A multi-select question (multi) is sent whole by its submit row ("Submit",
 *  or "Next" when another question follows); steps are the questions of its tab bar, review the tab that submits them. */
export interface ScreenDialog {
  header?: string;
  question?: string;
  detail?: string;
  options?: DialogOption[];
  rev?: string;
  multi?: boolean;
  submit?: string;
  steps?: { label: string; done: boolean }[];
  review?: boolean;
  incomplete?: boolean;
}

export interface Interaction {
  id: string;
  machine_id: string;
  session_id: string;
  session_title?: string;
  cwd?: string;
  kind: "question" | "plan" | "permission" | "screen";
  created_at?: string;
  tool_name?: string;
  questions?: Question[];
  plan?: string;
  tool_input?: Record<string, unknown>;
  summary?: string;
  screen?: string;
  secret?: boolean;
  dialog?: ScreenDialog;
  /** Who asks when it is not Claude Code: "codex", "traex", or an agent runtime's agent ("dsh"; docs/runtimes.md). */
  agent?: string;
  /** What "always allow" allows here; another agent's card offers it only with this. */
  always_label?: string;
}

export interface Launcher {
  name: string;
  model?: string;
  /** What it starts when not Claude Code ("codex"); agent_type: what the hub's machine config says it runs. */
  agent?: string;
  agent_type?: string;
}

export interface AssistantInfo {
  machine_id: string;
  launcher?: string;
  cwd?: string;
  main?: string;
  timezone?: string;
  notes?: string;
  notes_effective?: string;
  conversations: Session[];
}

export interface MetricPoint {
  t: number;
  cpu?: number;
  load?: number;
  mem?: number;
  gap?: boolean;
}

export interface MachineMetrics {
  machine_id: string;
  online: boolean;
  system?: Machine["system"];
  latest?: { t: number; cpu?: number; load?: number; mem_used?: number; mem_total?: number; net_rx?: number; net_tx?: number; disk_used?: number; disk_total?: number };
  points: MetricPoint[];
}

/** What a working Claude's status line says right now (hub `live` push, feature `session_live`; docs/protocol.md
 *  「实时状态」): read off its pane about once a second while someone watches. Absent fields: the line did not say. */
export interface SessionLive {
  /** The spinner's verb without the ellipsis ("Frosting"). */
  verb?: string;
  /** Seconds this turn has run, as the line counts them. */
  elapsed_s?: number;
  /** Output tokens so far this turn (the line's "↓ 22.8k tokens"). */
  tokens_down?: number;
  /** It is thinking now ("thinking with … effort"). */
  thinking?: boolean;
  /** It just thought for this many seconds ("thought for 3s"). */
  thought_s?: number;
  /** When the node read it (RFC 3339). */
  at?: string;
}

/** The hub's `live` push: `live` null or absent means the turn is over (or it is no longer read). */
export interface LiveEvent {
  type: "live";
  machine_id: string;
  session_id: string;
  live?: SessionLive | null;
}
