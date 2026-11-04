import { api, ApiError, enc } from "./api";
import { parseAPIError, retryable } from "./api-errors";
import { stateOf } from "./format";
import { splitAgentSessions, type AgentGroup, type AgentMutes } from "./agent-groups";
import type { AgentState, Machine, Session } from "./types";
import type { Route } from "./router";

export type TodoAgentSource = "manual" | "space" | "spawned";

export interface TodoAgent {
  machine_id: string;
  session_id: string;
  linked_at: string;
  released_at?: string | null;
  title?: string;
  cwd?: string;
  agent?: string;
  start_error?: string;
  source?: TodoAgentSource;
  via?: string;
}

export interface TodoSpace {
  machine_id: string;
  space: string;
  linked_at: string;
}

export interface Todo {
  id: string;
  title: string;
  notes?: string;
  done_at?: string | null;
  created_at: string;
  updated_at: string;
  agents?: TodoAgent[];
  spaces?: TodoSpace[];
  /** The assistant conversation that opened it as a task, and the plan (docs/unified-entry.md); unset for a todo made by hand. */
  origin?: string;
  plan_id?: string;
  /** "auto" when the hub marked the task done because its work was judged finished, with why (docs/todos.md「自动完成」). */
  done_by?: string;
  done_reason?: string;
  /** When the user last reopened it after it was done. */
  reopened_at?: string | null;
  /** The task's agent did not start, and why: the hub ended it there (done_at is set too, #1077). */
  start_error?: string;
}

export type TodoStatus = "done" | "needs_you" | "interrupted" | "unread" | "working" | "follow_up" | "not_started" | "failed";

export const TODO_STATUS_RANK: Record<TodoStatus, number> = {
  needs_you: 0,
  interrupted: 1,
  unread: 2,
  working: 3,
  follow_up: 4,
  not_started: 5,
  done: 6,
  failed: 7,
};

export const TODO_STATUS_INFO: Record<TodoStatus, { label: string; tone: "red" | "green" | "amber" | "idle" | "off" }> = {
  done: { label: "已完成", tone: "off" },
  needs_you: { label: "等你", tone: "red" },
  interrupted: { label: "中断了", tone: "red" },
  unread: { label: "待查看", tone: "green" },
  working: { label: "进行中", tone: "amber" },
  follow_up: { label: "待跟进", tone: "idle" },
  not_started: { label: "未开始", tone: "off" },
  failed: { label: "没开起来", tone: "red" },
};

/** Nothing of the task runs (it ended, stopped, or never started): the user may remove it (#1142). */
export function isRemovable(status: TodoStatus): boolean {
  return status !== "working" && status !== "needs_you";
}

export type SessionLookup = (mid: string, sid: string) => Session | undefined;

/** The hub closed it because its task's work was judged finished; reopening undoes it (docs/todos.md「自动完成」). */
export function isAutoDone(todo: Todo): boolean {
  return Boolean(todo.done_at) && todo.done_by === "auto";
}

/** The status's label for this todo: 「自动完成」 for one the hub closed. */
export function todoStatusLabel(todo: Todo, status: TodoStatus): string {
  return status === "done" && isAutoDone(todo) ? "自动完成" : TODO_STATUS_INFO[status].label;
}

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
 * 0. 没开起来：从对话开的任务 Agent 没启动起来，hub 把它结束了（start_error，#1077）
 * 1. 已完成：用户点了完成（done_at != null）
 * 2. 等你：有挂着的 Agent 需要你（提问、审批）
 * 2.5 中断了：有挂着的 Agent 这一轮以模型 API 错误结束（turn_error，#1516），没做完，等你让它继续
 * 3. 待查看：有挂着的 Agent 完成了你还没看
 * 4. 进行中：有挂着的 Agent 在工作或正在启动
 * 5. 待跟进：挂着的 Agent 都停着（已看、未运行、启动失败）
 * 6. 未开始：没有挂着的 Agent
 */
export function todoStatus(todo: Todo, lookup?: SessionLookup): TodoStatus {
  if (todo.start_error) return "failed";
  if (todo.done_at) return "done";
  const active = (todo.agents ?? []).filter((a) => !a.released_at);
  if (active.length === 0) return "not_started";

  let hasNeedsYou = false;
  let hasInterrupted = false;
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
    if (interruptedAgent(s)) hasInterrupted = true;
    else if (st === "unread") hasUnread = true;
    else if (st === "working" || s?.starting) hasWorking = true;
  }

  if (hasNeedsYou) return "needs_you";
  if (hasInterrupted) return "interrupted";
  if (hasUnread) return "unread";
  if (hasWorking) return "working";
  return "follow_up";
}

/** The agent stopped on a model API error (the hub's turn_error): cut off, not finished (#1516). */
export function interruptedAgent(s?: Session): boolean {
  if (!s?.turn_error) return false;
  const st = stateOf(s);
  return st === "unread" || st === "idle";
}

/** Why the model service cut the agent off (#1516), in words, and whether 继续 can help (a login or a bad request is
 *  not fixed by going on); undefined when it was not cut off. */
export function interruption(s?: Session): { reason: string; canContinue: boolean } | undefined {
  if (!interruptedAgent(s)) return undefined;
  const info = parseAPIError(s?.turn_error);
  return { reason: info?.reason ?? "模型请求失败", canContinue: !info || retryable(info) };
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

  const score = (st: AgentState | "failed", s?: Session) => {
    if (interruptedAgent(s)) return 3.5; // after one waiting on the user, before one done
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
    const sc = score(st, s);
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

export async function linkSpaceToTodo(
  todoId: string,
  mid: string,
  space: string,
): Promise<{ ok: boolean; linked: number }> {
  return await api<{ ok: boolean; linked: number }>(`/todos/${enc(todoId)}/spaces/${enc(mid)}/${enc(space)}`, {
    method: "PUT",
  });
}

export async function unlinkSpaceFromTodo(todoId: string, mid: string, space: string): Promise<void> {
  await api(`/todos/${enc(todoId)}/spaces/${enc(mid)}/${enc(space)}`, { method: "DELETE" });
}

/**
 * 查某个项目挂在哪个待办上：
 * 返回包含该项目 (machineId, spaceName) 且未完成的 Todo。
 */
export function findTodoForSpace(todos: Todo[], machineId: string, spaceName: string): Todo | undefined {
  if (!machineId || !spaceName) return undefined;
  for (const t of todos) {
    if (t.done_at) continue;
    if (t.spaces?.some((sp) => sp.machine_id === machineId && sp.space === spaceName)) {
      return t;
    }
  }
  return undefined;
}

/**
 * Navigate to a todo detail view and trigger drawer close callback if provided.
 */
export function openTodoFromTag(
  todoId: string,
  nav: (to: Route) => void,
  onNavigate?: () => void,
) {
  nav({ name: "todos", id: todoId });
  onNavigate?.();
}

/**
 * Check if an error from the spaces endpoints is a legacy hub 404 (route does not exist).
 * In Persona hub, jsonRouteErrors produces code === "not_found" and error === "Not Found"
 * for unregistered routes. Handlers themselves return specific messages like "todo not found"
 * or "待办不存在". Only a route-level 404 (code === "not_found" and error === "Not Found", or non-JSON 404)
 * marks the hub as legacy; other 404s are ordinary business failures (#881 review).
 */
export function isLegacySpacesRouteError(e: unknown): boolean {
  if (!(e instanceof ApiError) || e.status !== 404) return false;
  if (!e.isJson) return true;
  return e.body?.code === "not_found" && e.body?.error === "Not Found";
}

/**
 * Check whether todos data supports the spaces field (legacy hub compatibility).
 * An empty list is treated as unknown/unsupported (returns false).
 * Only returns true if at least one todo explicitly contains a spaces array.
 */
export function isSpacesSupported(todos: Todo[]): boolean {
  if (todos.length === 0) return false;
  return todos.some((t) => "spaces" in t && Array.isArray(t.spaces));
}

/**
 * Resolve the current todo ID for LinkTodoDialog target.
 * For a space, looks up from the todos list.
 * For a session, uses session.todo?.id directly so the unlink control
 * does not disappear if /todos hasn't loaded or failed (#874 item 4).
 */
export function resolveLinkCurrentTodoId(
  space?: { machineId: string; space: string },
  session?: Session,
  todos: Todo[] = [],
): string | undefined {
  if (space) {
    return findTodoForSpace(todos, space.machineId, space.space)?.id;
  }
  return session?.todo?.id;
}

/**
 * Format agent source label:
 * - Spawned from project: "随项目 <space>" (resolved from via, or agent's own session lookup as fallback);
 *   "随项目" if neither is available;
 * - Spawned from parent agent: "由 <parent title> 派出"; "派生" if parent is not in session list;
 * - Manual: undefined (not labeled).
 */
export function formatAgentSource(
  agent: TodoAgent,
  _spaces?: TodoSpace[],
  lookup?: SessionLookup,
): string | undefined {
  if (!agent.source || agent.source === "manual") return undefined;

  if (agent.source === "space") {
    let spaceName = "";
    if (agent.via) {
      const slash = agent.via.indexOf("/");
      if (slash >= 0) {
        spaceName = agent.via.slice(slash + 1);
      } else {
        spaceName = agent.via;
      }
    }
    if (!spaceName && lookup) {
      const s = lookup(agent.machine_id, agent.session_id);
      if (s?.space_name) spaceName = s.space_name;
    }
    return spaceName ? `随项目 ${spaceName}` : "随项目";
  }

  if (agent.source === "spawned") {
    if (agent.via && lookup) {
      const slash = agent.via.indexOf("/");
      if (slash >= 0) {
        const mid = agent.via.slice(0, slash);
        const sid = agent.via.slice(slash + 1);
        const parent = lookup(mid, sid);
        if (parent?.title) {
          return `由 ${parent.title} 派出`;
        }
      }
    }
    return "派生";
  }

  return undefined;
}


/** An agent going on (等你 / 在做) that the main conversation's dock shows after its tasks (#1113, iOS DockAgent). */
export interface DockAgent {
  session: Session;
  state: "needs_you" | "working";
}

/**
 * What a conversation's dock shows after its tasks (docs/unified-entry.md「主对话的任务坞里的手动 Agent」): in the main
 * conversation, every agent going on that no task stands for — the ones opened by hand on the Agents page — waiting
 * for the user first, then the newest in its state; elsewhere nothing. Persona's own conversations are not agents. A
 * task's agents (a task not done, from any conversation) are its pill already; an agent the hub tags with a todo shows
 * only when that todo was made by hand, so a task the todo list has not caught up with stays out too.
 */
export function dockAgents(sessions: Session[], todos: Todo[], conversation: string, main: string | undefined): DockAgent[] {
  if (!main || conversation !== main) return [];
  const taskAgents = new Set<string>();
  const manual = new Set<string>();
  for (const t of todos) {
    if (!t.origin) manual.add(t.id);
    else if (!t.done_at) for (const a of t.agents ?? []) if (!a.released_at) taskAgents.add(`${a.machine_id}/${a.session_id}`);
  }
  const out: DockAgent[] = [];
  for (const s of sessions) {
    if (s.assistant) continue;
    const state = stateOf(s);
    if (state !== "needs_you" && state !== "working") continue;
    if (taskAgents.has(`${s.machine_id}/${s.session_id}`)) continue;
    if (s.todo && !manual.has(s.todo.id)) continue;
    out.push({ session: s, state });
  }
  return out.sort(
    (a, b) =>
      (a.state === "needs_you" ? 0 : 1) - (b.state === "needs_you" ? 0 : 1) ||
      (b.session.state_since ?? "").localeCompare(a.session.state_since ?? "") ||
      `${a.session.machine_id}/${a.session.session_id}`.localeCompare(`${b.session.machine_id}/${b.session.session_id}`),
  );
}

/** What a task's lead agent says or does now (iOS TaskNow). */
export type TaskNow = { kind: "words"; text: string } | { kind: "step"; text: string; since?: string };

/**
 * A working agent's step (its latest tool call, which the hub only sends when it is newer than its words) comes
 * before its words; once it stopped, its words do (a turn cut short on a tool call), and the step only stands in for
 * words it never said. Null when it has said and done nothing yet (docs/unified-entry.md「任务卡片的进度」, #1583).
 */
export function taskNow(lead: Session | undefined, working: boolean): TaskNow | null {
  const words = lead?.last_text?.trim() ?? "";
  const step = lead?.activity?.trim() ?? "";
  if (step && (working || !words)) return { kind: "step", text: step, since: working ? lead?.activity_at : undefined };
  if (words) return { kind: "words", text: words };
  return null;
}

/** What each of an agent's running subagents is for: shown under its own progress, never instead of it. */
export function runningSubagents(s: Session | undefined): string[] {
  return (s?.subagents ?? []).filter((a) => a.running).map((a) => a.description?.trim() || a.type || "子任务");
}
