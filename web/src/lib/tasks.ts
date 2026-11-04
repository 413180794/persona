// Background work in a chat (iOS TaskIndex / TaskNoteText / NoticeRow.wakeSummary / ActivityChips): which call started
// a background task, what its notices say in words, and how a /loop and the session's background tasks read.

import type { BackgroundTask, Item, LoopInfo, Subagent, TaskNote } from "./types";

/** Background tasks and the calls that started them. A notice names its call by tool_use_id, or only by the task id
 *  the call's result announced ("running in background with ID: …", "Monitor started (task …"). */
export interface TaskIndex {
  /** A call's notices, oldest first, by the call's tool_use_id. */
  notes: Map<string, TaskNote[]>;
  /** The call a notice came from (notice item id → tool_use_id). */
  calls: Map<string, string>;
  /** Calls whose result says they went to the background (they may not have reported back yet). */
  started: Set<string>;
  /** What each of those calls was: its description, and when it started. */
  descriptions: Map<string, string>;
  startedAt: Map<string, string>;
}

const LAUNCHED = /(?:running in background with ID|agentId|Monitor started \(task|moved to the background \(ID):?\s*([A-Za-z0-9_-]+)/;

export function emptyTaskIndex(): TaskIndex {
  return { notes: new Map(), calls: new Map(), started: new Set(), descriptions: new Map(), startedAt: new Map() };
}

export function buildTaskIndex(items: Item[]): TaskIndex {
  const x = emptyTaskIndex();
  const byTask = new Map<string, string>();
  const uses = new Map<string, Item>();
  const describe = (call: string) => {
    const use = uses.get(call);
    if (!use) return;
    const d = (use.tool_input as Record<string, unknown> | undefined)?.description;
    if (typeof d === "string" && !x.descriptions.has(call)) x.descriptions.set(call, d);
    if (use.ts && !x.startedAt.has(call)) x.startedAt.set(call, use.ts);
  };
  for (const it of items) {
    if (it.kind === "tool_use") {
      if (it.tool_use_id) uses.set(it.tool_use_id, it);
    } else if (it.kind === "tool_result") {
      const text = it.text ?? "";
      if (!it.tool_use_id || !(text.includes("background") || text.includes("Monitor started") || text.includes("agentId"))) continue;
      const m = LAUNCHED.exec(text);
      if (!m) continue;
      byTask.set(m[1], it.tool_use_id);
      x.started.add(it.tool_use_id);
      describe(it.tool_use_id);
    } else if (it.kind === "notice" && it.task) {
      const n = it.task;
      const call = n.tool_use_id || (n.task_id ? byTask.get(n.task_id) : undefined);
      if (!call) continue;
      x.notes.set(call, [...(x.notes.get(call) ?? []), n]);
      x.calls.set(it.id, call);
      describe(call);
    }
  }
  return x;
}

export function taskFailed(n: TaskNote): boolean {
  return n.status === "failed" || (n.exit_code ?? 0) !== 0;
}

/** "后台命令" / "Monitor 事件" / "Monitor 结束" / "子 Agent". */
export function taskLabel(n: TaskNote): string {
  switch (n.kind) {
    case "shell":
      return "后台命令";
    case "monitor":
      return n.status === "event" ? "Monitor 事件" : "Monitor 结束";
    case "agent":
      return "子 Agent";
  }
  return "后台任务";
}

/** The call's description when we know the call, else what the notice says (a Monitor event: the event's first line). */
export function taskSubject(n: TaskNote, description?: string): string {
  if (n.kind === "monitor" && n.status === "event" && n.detail) {
    const line = firstLine(n.detail);
    if (line) return line;
  }
  return description || n.title || "";
}

/** How long it ran, short: "40 秒", "5 分", "2 小时 5 分". */
export function took(s: number): string {
  if (s < 60) return `${s} 秒`;
  if (s < 3600) return `${Math.floor(s / 60)} 分`;
  const m = Math.floor((s % 3600) / 60);
  return m === 0 ? `${Math.floor(s / 3600)} 小时` : `${Math.floor(s / 3600)} 小时 ${m} 分`;
}

export type Tone = "green" | "red" | "neutral" | "orange";

/** The outcome as a status pill (done green, failed red, stopped plain, waiting amber), with how long it ran. */
export function taskPill(n: TaskNote, startedAt?: string, at?: string): { text: string; tone: Tone } | null {
  let ran = "";
  const s = Date.parse(startedAt ?? ""), e = Date.parse(at ?? "");
  if (n.status !== "event" && Number.isFinite(s) && Number.isFinite(e) && e > s) ran = took(Math.floor((e - s) / 1000));
  const withRan = (w: string) => (ran ? `${w} · ${ran}` : w);
  switch (n.status) {
    case "completed":
    case "ended":
      return { text: withRan("已完成"), tone: "green" };
    case "failed":
      return { text: withRan(n.exit_code !== undefined ? `退出码 ${n.exit_code}` : "失败"), tone: "red" };
    case "killed":
    case "stopped":
      return { text: withRan("已停止"), tone: "neutral" };
    case "waiting":
      return { text: "等待中", tone: "orange" };
    case "event":
      return { text: "事件", tone: "neutral" };
  }
  if (n.exit_code !== undefined && n.exit_code !== 0) return { text: withRan(`退出码 ${n.exit_code}`), tone: "red" };
  return null;
}

/** The badge on the call that started background work: "后台 · 运行中" / "后台 · 已完成" / "后台 · 3 个事件". */
export function taskBadge(notes: TaskNote[]): { text: string; failed: boolean } {
  const events = notes.filter((n) => n.status === "event").length;
  const last = [...notes].reverse().find((n) => n.status !== "event");
  if (!last) return { text: events > 0 ? `后台 · ${events} 个事件` : "后台 · 运行中", failed: false };
  const state = last.status === "failed" ? "失败" : last.status === "killed" || last.status === "stopped" ? "已停止" : last.status === "waiting" ? "等待中" : last.status === "ended" ? "已结束" : "已完成";
  return { text: `后台 · ${state}${events > 0 ? ` · ${events} 个事件` : ""}`, failed: taskFailed(last) };
}

/** A wake notice in one line: the hub's text when it read the notification (`task`), else worked out from an older
 *  hub's summary: `Background command "Release" completed (exit code 0)` → "后台命令完成 · Release". */
export function wakeSummary(item: Item): { text: string; failed: boolean } {
  if (item.task) return { text: item.text || item.task.title || "", failed: taskFailed(item.task) };
  const s = item.text ?? "";
  const m = /^(.*?)\s*"(.+)"\s*(.*)$/.exec(s);
  if (!m) return { text: s, failed: false };
  const kind = m[1] === "Background command" ? "后台命令" : m[1] === "Agent" ? "子 Agent " : m[1] === "Monitor" ? "监控" : "后台任务";
  const rest = m[3].toLowerCase();
  const code = /exit code (\d+)/.exec(rest);
  const failed = rest.includes("fail") || rest.includes("error") || (code ? Number(code[1]) !== 0 : false);
  const state = failed ? "失败" : rest.includes("kill") || rest.includes("stop") ? "已停止" : rest.includes("complet") ? "完成" : "有更新";
  return { text: `${kind}${state} · ${m[2]}`, failed };
}

function firstLine(s: string): string {
  return s.split("\n").map((l) => l.trim()).find(Boolean) ?? "";
}

// ── the session's /loop and background tasks ──

/** The task, without the "/loop" command word. */
export function loopTask(loop: LoopInfo): string {
  const p = (loop.prompt ?? "").trim();
  return p.startsWith("/loop") ? p.slice(5).trim() : p;
}

/** "每 30 分钟" / "按计划 33 17 * * *". */
export function loopCadence(loop: LoopInfo): string {
  if (loop.kind === "cron" && loop.cron) return `按计划 ${loop.cron}`;
  const s = loop.interval_seconds ?? 0;
  if (s <= 0) return "";
  if (s < 120) return `每 ${s} 秒`;
  if (s < 7200) return `每 ${Math.floor(s / 60)} 分钟`;
  return `每 ${Math.floor(s / 3600)} 小时`;
}

function hhmm(ms: number): string {
  const d = new Date(ms);
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

/** The loop beside its arrows, short: the next time, or that it is running. */
export function loopShortLabel(loop: LoopInfo, now: number): string {
  const next = Date.parse(loop.next_at ?? "");
  if (Number.isFinite(next) && next > now) return hhmm(next);
  return loop.next_at ? "执行中" : "循环";
}

/** "下次 11:57" / "循环执行中" / "每 30 分钟". */
export function loopLabel(loop: LoopInfo, now: number): string {
  const next = Date.parse(loop.next_at ?? "");
  if (Number.isFinite(next) && next > now) return `下次 ${hhmm(next)}`;
  if (loop.next_at) return "循环执行中";
  return loopCadence(loop) || "循环中";
}

/** "下次唤醒 11:57（还有 4 分钟）" / "正在执行这一轮". */
export function loopNext(loop: LoopInfo, now: number): string {
  const next = Date.parse(loop.next_at ?? "");
  if (!Number.isFinite(next)) return "";
  if (next <= now) return "正在执行这一轮";
  const m = Math.max(1, Math.ceil((next - now) / 60_000));
  const until = m < 60 ? `还有 ${m} 分钟` : `还有 ${Math.floor(m / 60)} 小时${m % 60 ? ` ${m % 60} 分` : ""}`;
  return `下次唤醒 ${hhmm(next)}（${until}）`;
}

/** What tells the agent to end its /loop: nothing outside Claude can cancel a scheduled wakeup or its cron job. */
export const STOP_LOOP_MESSAGE =
  "停止当前的 /loop：调用 ScheduleWakeup(stop: true) 结束循环（如果用 CronCreate 建过定时任务，用 CronDelete 删掉），不要再安排下一次唤醒，回复一句「循环已停止」即可。";

/** A background task in one line: the description Claude gave, else the command. */
export function backgroundTitle(t: BackgroundTask): string {
  if (t.description) return t.description;
  return t.command?.split("\n")[0] || (t.kind === "agent" ? "子代理" : "后台命令");
}

/** "40 秒" / "5 分 12 秒" / "1 时 5 分". */
export function elapsed(fromIso: string | undefined, now: number): string {
  const start = Date.parse(fromIso ?? "");
  if (!Number.isFinite(start)) return "";
  const s = Math.max(0, Math.floor((now - start) / 1000));
  if (s < 60) return `${s} 秒`;
  if (s < 3600) return `${Math.floor(s / 60)} 分 ${s % 60} 秒`;
  return `${Math.floor(s / 3600)} 时 ${Math.floor((s % 3600) / 60)} 分`;
}

export function subagentTitle(a: Subagent): string {
  return a.description || a.type || "子 Agent";
}

/** The subagents worth a chip: running ones, plus the one being read. */
export function visibleSubagents(all: Subagent[] | undefined, viewing?: string): Subagent[] {
  return (all ?? []).filter((a) => a.running || a.id === viewing);
}

export interface SubagentRow {
  agent: Subagent;
  /** 0: the session launched it (or its launcher is not listed); 1: a subagent did; … */
  depth: number;
}

/** Every subagent by who launched whom (`parent_id`), flattened for a list: each right under its launcher, one level
 *  deeper; one whose launcher is not listed (it ended before the node's one-day cutoff) at the top level. Within a
 *  level running ones first, then the newest first (as the node lists them). */
export function subagentTree(all: Subagent[] | undefined): SubagentRow[] {
  const list = all ?? [];
  const listed = new Set(list.map((a) => a.id));
  const newest = (a: Subagent) => Date.parse(a.started_at ?? "") || 0;
  const order = (a: Subagent, b: Subagent) => Number(Boolean(b.running)) - Number(Boolean(a.running)) || newest(b) - newest(a);
  const kids = new Map<string, Subagent[]>();
  for (const a of [...list].sort(order)) {
    const p = a.parent_id && a.parent_id !== a.id && listed.has(a.parent_id) ? a.parent_id : "";
    kids.set(p, [...(kids.get(p) ?? []), a]);
  }
  const out: SubagentRow[] = [];
  const placed = new Set<string>();
  const place = (a: Subagent, depth: number) => {
    if (placed.has(a.id)) return;
    placed.add(a.id);
    out.push({ agent: a, depth });
    for (const k of kids.get(a.id) ?? []) place(k, depth + 1);
  };
  for (const a of kids.get("") ?? []) place(a, 0);
  // launchers naming each other (only a runtime could write that) are reached from nowhere: list them at the top
  for (const a of [...list].sort(order)) place(a, 0);
  return out;
}
