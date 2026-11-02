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
  node_mismatch?: boolean;
  deploying?: boolean;
  deploy_error?: string;
  system?: { name?: string; model?: string; cpu?: string; cores?: number; memory?: number };
}

export interface Subagent {
  id: string;
  session_id: string;
  type?: string;
  description?: string;
  running?: boolean;
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
  machine_id: string;
  session_id: string;
  title: string;
  cwd: string;
  status: string;
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
  pending_interactions?: number;
  queued?: string[];
  subagents?: Subagent[];
  loop?: { kind: string; next_at?: string; cron?: string; prompt?: string };
  background?: { id: string; kind: string; description?: string; command?: string }[];
  rev?: number;
  suggestion?: string;
}

export interface Item {
  id: string;
  seq: number;
  ts?: string;
  role: "user" | "assistant" | "system";
  kind: "text" | "thinking" | "tool_use" | "tool_result" | "notice" | "image" | "fold";
  text?: string;
  tool_use_id?: string;
  tool_name?: string;
  tool_input?: Record<string, unknown>;
  is_error?: boolean;
  full_size?: number;
  queued?: boolean;
  wake?: boolean;
  task?: { kind: string; status: string; title?: string; detail?: string; exit_code?: number };
  fold?: { items: number; tools: number; thoughts: number; until?: string; through_seq: number; through_id?: string };
  image?: { mime: string; data?: string; lazy?: boolean; width?: number; height?: number };
  from?: { name?: string };
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
  dialog?: { header?: string; question?: string; detail?: string; options?: DialogOption[]; rev?: string };
  /** Who asks when it is not Claude Code: "codex", "traex", or an agent runtime's agent ("dsh"; docs/runtimes.md). */
  agent?: string;
  /** What "always allow" allows here; another agent's card offers it only with this. */
  always_label?: string;
}

export interface Launcher {
  name: string;
  model?: string;
}

export interface AssistantInfo {
  machine_id: string;
  launcher?: string;
  cwd?: string;
  main?: string;
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
