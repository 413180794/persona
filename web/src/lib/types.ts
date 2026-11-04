// The hub's wire types (docs/protocol.md). Only the fields the web client reads.

export type AgentState = "needs_you" | "working" | "unread" | "idle" | "offline" | "unknown";

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
  engine?: string;
  kind?: string;
  arch?: string;
  home?: string;
  last_seen?: string;
  /** Interconnected hubs (docs/plan-hub-mesh.md 2.6): the hub that owns this computer, and the way there. */
  hub_id?: string;
  hub_name?: string;
  machine_uid?: string;
  route?: { via: string; hops: number; rtt_ms?: number };
  /** Operating it through Cua Driver (the hub's plugin cua, docs/computer-use.md): only while the plugin is on and it is a Mac. */
  computer?: {
    enabled: boolean; installed?: boolean; daemon?: boolean; ready?: boolean; accessibility?: string; screen_recording?: string;
    desktop?: string; screen_sharing?: boolean; note?: string;
  };
  /** ego lite (the hub's plugin ego, docs/plan-ego-browser.md): only while the plugin is on and it is a Mac. */
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
  updated_at?: string;
  seen_at?: string;
  last_text?: string;
  /** Its latest step when that is a tool call no text came after (「在跑测试」「在读 tasks.go」), and when it started. */
  activity?: string;
  activity_at?: string;
  /** Its latest turn ended in a model API error (the hub sets it while it is stopped, #1516): its task is interrupted. */
  turn_error?: string;
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
