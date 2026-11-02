import { api, enc } from "./api";
import { stateOf } from "./format";
import { splitAgentSessions, type AgentGroup, type AgentMutes } from "./agent-groups";
import type { AgentState, Machine, Session } from "./types";

export interface TodoAgent {
  machine_id: string;
  session_id: string;
  linked_at: string;
  released_at?: string | null;
  title?: string;
  cwd?: string;
  agent?: string;
  start_error?: string;
}

export interface Todo {
  id: string;
  title: string;
  notes?: string;
  done_at?: string | null;
  created_at: string;
  updated_at: string;
  agents?: TodoAgent[];
}

export type TodoStatus = "done" | "needs_you" | "unread" | "working" | "follow_up" | "not_started";

export const TODO_STATUS_RANK: Record<TodoStatus, number> = {
  needs_you: 0,
  unread: 1,
  working: 2,
  follow_up: 3,
  not_started: 4,
  done: 5,
};

export const TODO_STATUS_INFO: Record<TodoStatus, { label: string; tone: "red" | "green" | "amber" | "idle" | "off" }> = {
  done: { label: "已完成", tone: "off" },
  needs_you: { label: "等你", tone: "red" },
  unread: { label: "待查看", tone: "green" },
  working: { label: "进行中", tone: "amber" },
  follow_up: { label: "待跟进", tone: "idle" },
  not_started: { label: "未开始", tone: "off" },
};

export type SessionLookup = (mid: string, sid: string) => Session | undefined;

export function makeSessionLookup(sessions: Session[] | Record<string, Session[]>): SessionLookup {
  if (Array.isArray(sessions)) {
    const map = new Map<string, Session>();
    for (const s of sessions) map.set(`${s.machine_id}/${s.session_id}`, s);
    return (mid, sid) => map.get(`${mid}/${sid}`);
  }
  return (mid, sid) => (sessions[mid] ?? []).find((s) => s.session_id === sid);
}

/**
 * 待办状态判定规则（取第一个成立的）：
 * 1. 已完成：用户点了完成（done_at != null）
 * 2. 等你：有挂着的 Agent 需要你（提问、审批）
 * 3. 待查看：有挂着的 Agent 完成了你还没看
 * 4. 进行中：有挂着的 Agent 在工作或正在启动
 * 5. 待跟进：挂着的 Agent 都停着（已看、未运行、启动失败）
 * 6. 未开始：没有挂着的 Agent
 */
export function todoStatus(todo: Todo, lookup?: SessionLookup): TodoStatus {
  if (todo.done_at) return "done";
  const active = (todo.agents ?? []).filter((a) => !a.released_at);
  if (active.length === 0) return "not_started";

  let hasNeedsYou = false;
  let hasUnread = false;
  let hasWorking = false;

  for (const a of active) {
    if (a.start_error) {
      // 启动失败视为停着
      continue;
    }
    const s = lookup?.(a.machine_id, a.session_id);
    const st: AgentState = s ? stateOf(s) : "offline";
    if (st === "needs_you") {
      hasNeedsYou = true;
      break;
    }
    if (st === "unread") hasUnread = true;
    else if (st === "working" || s?.starting) hasWorking = true;
  }

  if (hasNeedsYou) return "needs_you";
  if (hasUnread) return "unread";
  if (hasWorking) return "working";
  return "follow_up";
}

/**
 * 待办列表排序：
 * - 未完成在上面，按状态优先级排（等你 > 待查看 > 进行中 > 待跟进 > 未开始）
 * - 同状态按创建时间排（新的在前）
 * - 已完成在底部
 */
export function sortTodos(todos: Todo[], lookup?: SessionLookup): Todo[] {
  return [...todos].sort((a, b) => {
    const aDone = Boolean(a.done_at);
    const bDone = Boolean(b.done_at);
    if (aDone !== bDone) return aDone ? 1 : -1;

    const aStat = todoStatus(a, lookup);
    const bStat = todoStatus(b, lookup);
    const rA = TODO_STATUS_RANK[aStat];
    const rB = TODO_STATUS_RANK[bStat];
    if (rA !== rB) return rA - rB;

    const tA = a.created_at ? Date.parse(a.created_at) || 0 : 0;
    const tB = b.created_at ? Date.parse(b.created_at) || 0 : 0;
    if (tA !== tB) return tB - tA;
    return a.id.localeCompare(b.id);
  });
}

/**
 * 查找待办中状态最要紧的一个 Agent 及其摘要信息
 */
export function mostUrgentAgent(
  todo: Todo,
  lookup?: SessionLookup,
): { agent: TodoAgent; session?: Session; status: AgentState | "failed" } | null {
  const active = (todo.agents ?? []).filter((a) => !a.released_at);
  if (active.length === 0) return null;

  const score = (st: AgentState | "failed") => {
    switch (st) {
      case "needs_you":
        return 4;
      case "unread":
        return 3;
      case "working":
        return 2;
      case "failed":
        return 1;
      default:
        return 0;
    }
  };

  let best: { agent: TodoAgent; session?: Session; status: AgentState | "failed" } | null = null;
  let bestScore = -1;

  for (const a of active) {
    const s = lookup?.(a.machine_id, a.session_id);
    const st: AgentState | "failed" = a.start_error ? "failed" : s ? stateOf(s) : "offline";
    const sc = score(st);
    if (sc > bestScore || !best) {
      bestScore = sc;
      best = { agent: a, session: s, status: st };
    }
  }

  return best;
}

function byDate(a: Session, b: Session): number {
  const parseTime = (s: Session) => {
    const raw = (stateOf(s) === "working" ? s.state_since || s.updated_at : s.updated_at) ?? "";
    return raw ? Date.parse(raw) || 0 : 0;
  };
  const diff = parseTime(b) - parseTime(a);
  if (diff !== 0) return diff;
  return a.session_id.localeCompare(b.session_id) || a.machine_id.localeCompare(b.machine_id);
}

function sessionRank(s: Session, mutes: AgentMutes): number {
  const state = stateOf(s);
  const waiting = ["idle", "unknown"].includes(state) && Boolean(s.loop || s.background?.length || s.subagents?.some((a) => a.running));
  const isMuted = mutes.sessions.includes(`${s.machine_id}/${s.session_id}`) || Boolean(s.space_name && mutes.spaces.includes(`${s.machine_id}/${s.space_name}`));
  if (state === "needs_you") return 0;
  if (isMuted && (state === "working" || state === "unread" || waiting)) return 4;
  if (state === "unread") return 1;
  if (state === "working") return 2;
  if (waiting) return 3;
  return 5;
}

/**
 * Agents 侧边栏「按待办」分组纯函数：
 * - 每个有挂着 Agent 的未完成待办一段，段头是待办标题和状态，点段头进待办
 * - 没挂待办的 Agent 放最后「没有待办」一段
 * - 置顶、屏蔽、「最近（未运行）」的规则和「按状态」一致
 */
export function buildTodoAgentGroups(
  all: Session[],
  machines: Machine[],
  todos: Todo[],
  mutes: AgentMutes = { spaces: [], sessions: [] },
): AgentGroup[] {
  const { live } = splitAgentSessions(all, machines);
  const lookup = makeSessionLookup(live);

  // 未完成待办排序
  const openTodos = sortTodos(todos.filter((t) => !t.done_at), lookup);

  // 映射各个待办与其 live sessions
  const liveByKey = new Map<string, Session>();
  for (const s of live) {
    liveByKey.set(`${s.machine_id}/${s.session_id}`, s);
  }

  const assignedSessionKeys = new Set<string>();
  const groups: AgentGroup[] = [];

  for (const todo of openTodos) {
    const todoSessions: Session[] = [];
    const activeAgents = (todo.agents ?? []).filter((a) => !a.released_at);

    // 首先从 todo.agents 收集未 released 的 live 会话
    for (const a of activeAgents) {
      const key = `${a.machine_id}/${a.session_id}`;
      const s = liveByKey.get(key);
      if (s && !assignedSessionKeys.has(key)) {
        todoSessions.push(s);
        assignedSessionKeys.add(key);
      }
    }

    // 其次补充 session.todo?.id 指向该待办的会话
    for (const s of live) {
      const key = `${s.machine_id}/${s.session_id}`;
      if (s.todo?.id === todo.id && !assignedSessionKeys.has(key)) {
        todoSessions.push(s);
        assignedSessionKeys.add(key);
      }
    }

    if (todoSessions.length > 0) {
      // 组内排序：按状态优先级（置顶/需要你排前面，屏蔽降级），同优先级按时间降序
      todoSessions.sort((a, b) => {
        const rA = sessionRank(a, mutes);
        const rB = sessionRank(b, mutes);
        if (rA !== rB) return rA - rB;
        return byDate(a, b);
      });

      const st = todoStatus(todo, lookup);
      groups.push({
        key: `todo:${todo.id}`,
        name: todo.title,
        sessions: todoSessions,
        todoId: todo.id,
        todoStatus: st,
      });
    }
  }

  // 没挂待办的放最后「没有待办」一段
  const unassigned = live.filter((s) => !assignedSessionKeys.has(`${s.machine_id}/${s.session_id}`));
  if (unassigned.length > 0) {
    unassigned.sort((a, b) => {
      const rA = sessionRank(a, mutes);
      const rB = sessionRank(b, mutes);
      if (rA !== rB) return rA - rB;
      return byDate(a, b);
    });
    groups.push({
      key: "todo:none",
      name: "没有待办",
      sessions: unassigned,
    });
  }

  return groups;
}

/**
 * 拼装交给 Agent 时的格式（与收件箱一致）
 * `<jarvis-context kind="context" title="待办 · <标题>" url="">说明</jarvis-context>`
 */
export function formatTodoContext(title: string, notes?: string): string {
  const t = title.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  const body = (notes ?? "").trim();
  return `<jarvis-context kind="context" title="待办 · ${t}" url="">${body}</jarvis-context>`;
}

// ── API helpers ──────────────────────────────────────────────

export async function fetchTodos(): Promise<Todo[]> {
  const r = await api<{ todos: Todo[] }>("/todos");
  return r.todos ?? [];
}

export async function createTodo(title: string, notes?: string): Promise<Todo> {
  const r = await api<{ todo: Todo }>("/todos", {
    method: "POST",
    json: { title: title.trim(), notes: notes?.trim() || undefined },
  });
  return r.todo;
}

export async function updateTodo(id: string, patch: { title?: string; notes?: string; done?: boolean }): Promise<Todo> {
  const r = await api<{ todo: Todo }>(`/todos/${enc(id)}`, {
    method: "PATCH",
    json: patch,
  });
  return r.todo;
}

export async function deleteTodo(id: string): Promise<void> {
  await api(`/todos/${enc(id)}`, { method: "DELETE" });
}

export async function linkAgentToTodo(todoId: string, mid: string, sid: string): Promise<void> {
  await api(`/todos/${enc(todoId)}/agents/${enc(mid)}/${enc(sid)}`, { method: "PUT" });
}

export async function unlinkAgentFromTodo(todoId: string, mid: string, sid: string): Promise<void> {
  await api(`/todos/${enc(todoId)}/agents/${enc(mid)}/${enc(sid)}`, { method: "DELETE" });
}
