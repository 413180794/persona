// The assistant's scheduled tasks (docs/assistant.md「定时任务」, docs/protocol.md `/assistant/tasks`): the wire types,
// how a task is said in words (the iOS app's AssistantScheduleText), what the editor checks, and the body it saves.
// Pure functions of their arguments (`now` passed in), so they are tested without a DOM.

export type Repeat = "once" | "daily" | "interval";

export interface Task {
  id: string;
  title: string;
  prompt: string;
  repeat: Repeat;
  /** once: when; interval: the next run. */
  at?: string;
  /** daily: "HH:MM" in the user's time zone. */
  time?: string;
  /** daily: 1 = Monday … 7 = Sunday; empty = every day. */
  weekdays?: number[] | null;
  every_minutes?: number;
  enabled: boolean;
  machine_id?: string;
  /** A conversation runs go into (接着原来的对话); empty = the task's own one. */
  session_id?: string;
  launcher?: string;
  permission_mode?: string;
  effort?: string;
  created_by?: string;
  created_at?: string;
  last_run?: string;
  last_session_id?: string;
  last_status?: string;
  last_error?: string;
  next_run?: string;
  run_machine_id?: string;
  run_session_id?: string;
  run_launcher?: string;
}

export interface Run {
  id: string;
  kind: string;
  ref_id: string;
  title: string;
  due: string;
  at: string;
  status: string;
  error?: string;
  manual?: boolean;
  machine_id?: string;
  session_id?: string;
}

export const DAY_NAMES = ["一", "二", "三", "四", "五", "六", "日"];
export const WORKDAYS = [1, 2, 3, 4, 5];
export const WEEKEND = [6, 7];
export const HHMM = /^([01]\d|2[0-3]):[0-5]\d$/;
export const MIN_EVERY = 5;
export const MAX_EVERY = 7 * 24 * 60;
/** Quick picks for 每隔. */
export const INTERVALS = [15, 30, 60, 120, 360, 1440];

/** A time the hub sent, or null: empty, unparsable, or Go's zero time ("0001-01-01T00:00:00Z"). */
export function parseTime(iso?: string | null): Date | null {
  if (!iso) return null;
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return null;
  const d = new Date(t);
  return d.getUTCFullYear() < 2000 ? null : d;
}

/** Monday = 1 … Sunday = 7. */
function isoDay(d: Date): number {
  return ((d.getDay() + 6) % 7) + 1;
}

function startOfDay(d: Date): number {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
}

/** Calendar days from `now` to `d` (negative: before). */
function daysBetween(now: Date, d: Date): number {
  return Math.round((startOfDay(d) - startOfDay(now)) / 86_400_000);
}

/** 1…7, unique, sorted; all seven (or none) is []: every day. */
export function normalizeDays(days?: number[] | null): number[] {
  const s = [...new Set((days ?? []).filter((d) => Number.isInteger(d) && d >= 1 && d <= 7))].sort((a, b) => a - b);
  return s.length === 7 ? [] : s;
}

/** "每天" / "工作日" / "周末" / "每周一、三、五". */
export function repeatsLabel(days?: number[] | null): string {
  const d = normalizeDays(days);
  if (d.length === 0) return "每天";
  if (d.join() === WORKDAYS.join()) return "工作日";
  if (d.join() === WEEKEND.join()) return "周末";
  return "每周" + d.map((x) => DAY_NAMES[x - 1]).join("、");
}

/** "09:00" → "上午 9:00", "21:30" → "晚上 9:30", "00:15" → "凌晨 12:15". */
export function clockLabel(hhmm?: string): string {
  if (!hhmm || !HHMM.test(hhmm)) return hhmm ?? "";
  const [h, m] = hhmm.split(":").map(Number);
  const part = h < 5 ? "凌晨" : h < 9 ? "早上" : h < 12 ? "上午" : h < 13 ? "中午" : h < 18 ? "下午" : "晚上";
  const h12 = h % 12 === 0 ? 12 : h % 12;
  return `${part} ${h12}:${String(m).padStart(2, "0")}`;
}

/** "每 30 分钟" / "每 2 小时" / "每 1 小时 30 分钟" / "每天" / "每 2 天". */
export function everyLabel(minutes: number): string {
  if (minutes < 60) return `每 ${minutes} 分钟`;
  if (minutes % 1440 === 0) return minutes === 1440 ? "每天" : `每 ${minutes / 1440} 天`;
  const h = Math.floor(minutes / 60), m = minutes % 60;
  return m ? `每 ${h} 小时 ${m} 分钟` : `每 ${h} 小时`;
}

/** "17:05", always 24-hour. */
export function hm(d: Date): string {
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

/** The day part of a moment: "今天" / "明天" / "昨天" / "周三" (within a week) / "11月2日". */
export function dayLabel(d: Date, now: Date): string {
  const n = daysBetween(now, d);
  if (n === 0) return "今天";
  if (n === 1) return "明天";
  if (n === -1) return "昨天";
  if (Math.abs(n) < 7) return `周${DAY_NAMES[isoDay(d) - 1]}`;
  return `${d.getMonth() + 1}月${d.getDate()}日`;
}

/** "今天 15:30" / "明天 09:00" / "周三 09:00" / "11月2日 09:00". */
export function momentLabel(d: Date, now: Date): string {
  return `${dayLabel(d, now)} ${hm(d)}`;
}

/** A future moment, roughly: "马上" / "25 分钟后" / "2 小时后" / "1 小时 30 分钟后" / "3 天后"; "已过时间" in the past. */
export function untilLabel(d: Date, now: Date): string {
  const s = (d.getTime() - now.getTime()) / 1000;
  if (s < 60) return s < -60 ? "已过时间" : "马上";
  if (s < 3600) return `${Math.floor(s / 60)} 分钟后`;
  if (s < 86400) {
    const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60);
    return h < 3 && m >= 10 ? `${h} 小时 ${m} 分钟后` : `${h} 小时后`;
  }
  return `${Math.max(daysBetween(now, d), 1)} 天后`;
}

/** Its title, or the first line of the prompt (a follow-up Jarvis scheduled without one). */
export function taskTitle(t: Pick<Task, "title" | "prompt">): string {
  if (t.title?.trim()) return t.title.trim();
  const first = (t.prompt ?? "").split("\n").find((l) => l.trim()) ?? "";
  return first.trim() || "定时跟进";
}

/** The rule in words: "每个工作日 上午 9:00" / "每 30 分钟" / "一次 · 新对话". */
export function ruleText(t: Task): string {
  if (t.repeat === "interval") return everyLabel(t.every_minutes ?? 0);
  if (t.repeat === "once") return t.session_id ? "一次 · 接着原来的对话" : "一次 · 新对话";
  const d = normalizeDays(t.weekdays);
  const days = d.length === 0 ? "每天" : d.join() === WORKDAYS.join() ? "每个工作日" : d.join() === WEEKEND.join() ? "每个周末" : repeatsLabel(d);
  return `${days} ${clockLabel(t.time)}`;
}

/** When it runs next, or null when it is switched off or the hub has no time for it. */
export function nextRun(t: Task): Date | null {
  return t.enabled ? parseTime(t.next_run) ?? parseTime(t.at) : null;
}

/** "下次 今天 09:00 · 2 小时后" / "已关闭" / "". */
export function nextText(t: Task, now: Date): string {
  if (!t.enabled) return "已关闭";
  const n = nextRun(t);
  return n ? `下次 ${momentLabel(n, now)} · ${untilLabel(n, now)}` : "";
}

/** The last run in a few words, null when it never ran: "上次 今天 09:03 · 已运行". */
export function lastRunText(t: Task, now: Date): { text: string; failed: boolean } | null {
  const at = parseTime(t.last_run);
  if (!at) return null;
  const word = t.last_status === "failed" ? "失败" : t.last_status === "skipped" ? "已跳过" : "已运行";
  return { text: `上次 ${momentLabel(at, now)} · ${word}`, failed: t.last_status === "failed" };
}

// ── what a run starts claude with ───────────────────────

export interface ModeOption {
  id: string;
  title: string;
  detail: string;
}

/** Besides 默认 (""): the hub's permission modes (proto.PermissionModes). */
export const PERMISSION_MODES: ModeOption[] = [
  { id: "acceptEdits", title: "自动改文件", detail: "改文件不问，跑命令还会问" },
  { id: "plan", title: "只做计划", detail: "只读代码、写计划，批准后才动手" },
  { id: "auto", title: "自动", detail: "自己判断风险，安全的直接做" },
  { id: "bypassPermissions", title: "全部放行", detail: "什么都不问，有风险" },
];

export const EFFORTS: ModeOption[] = [
  { id: "low", title: "低", detail: "最快，简单的事够用" },
  { id: "medium", title: "中", detail: "快一些" },
  { id: "high", title: "高", detail: "大多数任务的默认" },
  { id: "xhigh", title: "很高", detail: "更仔细，也更慢、更贵" },
  { id: "max", title: "最高", detail: "最慢最仔细" },
];

export const BYPASS = "bypassPermissions";

/** "自动改文件" for a known mode, the raw value otherwise; "" for the default. */
export function modeLabel(id?: string): string {
  if (!id || id === "default") return "";
  return PERMISSION_MODES.find((m) => m.id === id)?.title ?? id;
}

export function effortLabel(id?: string): string {
  if (!id) return "";
  return EFFORTS.find((e) => e.id === id)?.title ?? id;
}

/** What it runs with when that isn't the default, in a few words: "claude-fast · 自动 · 思考低"; `danger` for 全部放行. */
export function runsWith(t: Pick<Task, "launcher" | "permission_mode" | "effort">): { text: string; danger: boolean } | null {
  const parts = [t.launcher, modeLabel(t.permission_mode), t.effort ? `思考${effortLabel(t.effort)}` : ""].filter(Boolean);
  return parts.length ? { text: parts.join(" · "), danger: t.permission_mode === BYPASS } : null;
}

// ── the editor ──────────────────────────────────────────

/** What the editor checks before saving (the hub checks the same and says why in Chinese): null = fine. */
export function validateTask(d: Task, now: Date): string | null {
  if (d.repeat !== "once" && !d.title.trim()) return "要有标题";
  if (!d.prompt.trim()) return "提示词不能为空";
  switch (d.repeat) {
    case "daily":
      if (!HHMM.test(d.time ?? "")) return "时间要写成 HH:MM，比如 09:00";
      return null;
    case "interval": {
      const m = d.every_minutes ?? 0;
      if (!Number.isInteger(m) || m < MIN_EVERY || m > MAX_EVERY) return "间隔要在 5 分钟到 7 天之间";
      return null;
    }
    case "once": {
      const at = parseTime(d.at);
      if (!at) return "要选一个运行时间";
      if (d.enabled && at.getTime() < now.getTime() - 60_000) return "这个时间已经过了，选一个以后的时间";
      return null;
    }
    default:
      return "不认识的重复方式";
  }
}

/** Whether two versions differ in anything the user edits (the hub's own fields and, outside its rule, the fields
 *  another rule uses don't count). */
export function sameTask(a: Task, b: Task): boolean {
  if (a.title.trim() !== b.title.trim() || a.prompt.trim() !== b.prompt.trim() || a.repeat !== b.repeat || a.enabled !== b.enabled) return false;
  if ((a.session_id ?? "") !== (b.session_id ?? "") || (a.launcher ?? "") !== (b.launcher ?? "")) return false;
  if ((a.permission_mode ?? "") !== (b.permission_mode ?? "") || (a.effort ?? "") !== (b.effort ?? "")) return false;
  switch (a.repeat) {
    case "once":
      return parseTime(a.at)?.getTime() === parseTime(b.at)?.getTime();
    case "daily":
      return a.time === b.time && normalizeDays(a.weekdays).join() === normalizeDays(b.weekdays).join();
    case "interval":
      return a.every_minutes === b.every_minutes;
  }
}

/** The JSON for POST (no `saved`) or PUT. The hub replaces every field it takes except the model ones, which it keeps
 *  when they are left out: they go only when changed here, so a model just set in 模型配置 isn't put back (docs/model-config.md).
 *  `session_id` "" makes runs use the task's own conversation; `at` is left out when there is none (Go can't read ""). */
export function taskBody(d: Task, saved?: Task | null): Record<string, unknown> {
  const body: Record<string, unknown> = {
    title: d.title.trim(),
    prompt: d.prompt.trim(),
    repeat: d.repeat,
    enabled: d.enabled,
    time: d.repeat === "daily" ? d.time ?? "" : "",
    weekdays: d.repeat === "daily" ? normalizeDays(d.weekdays) : [],
    every_minutes: d.repeat === "interval" ? d.every_minutes ?? 0 : 0,
    session_id: d.session_id ?? "",
    permission_mode: d.permission_mode ?? "",
  };
  const at = d.repeat === "daily" ? null : parseTime(d.at);
  if (at) body.at = at.toISOString();
  if (!saved || (d.launcher ?? "") !== (saved.launcher ?? "")) body.launcher = d.launcher ?? "";
  if (!saved || (d.effort ?? "") !== (saved.effort ?? "")) body.effort = d.effort ?? "";
  return body;
}

/** The next whole hour after `now` (a new one-off's time). */
export function nextHour(now: Date): Date {
  const d = new Date(now.getTime() + 3_600_000);
  d.setMinutes(0, 0, 0);
  return d;
}

/** A Date as an <input type="datetime-local"> value, in local time: "2026-11-02T15:30". */
export function toLocalInput(d: Date | null): string {
  if (!d) return "";
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
}

/** An <input type="datetime-local"> value back to a Date (local time); null when empty or malformed. */
export function fromLocalInput(s: string): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(s);
  if (!m) return null;
  const d = new Date(+m[1], +m[2] - 1, +m[3], +m[4], +m[5]);
  return Number.isNaN(d.getTime()) ? null : d;
}

/** Switching the rule keeps the editor valid: 一次 gets the next whole hour when it has no future time, 每隔 30 minutes
 *  when it has none, 每天 09:00 when it has no time. */
export function withRepeat(d: Task, r: Repeat, now: Date): Task {
  const t: Task = { ...d, repeat: r };
  if (r === "once" && (parseTime(t.at)?.getTime() ?? 0) < now.getTime()) t.at = nextHour(now).toISOString();
  if (r === "interval" && (t.every_minutes ?? 0) < MIN_EVERY) t.every_minutes = 30;
  if (r === "daily" && !HHMM.test(t.time ?? "")) t.time = "09:00";
  return t;
}

// ── new tasks ───────────────────────────────────────────

export interface Template {
  key: string;
  title: string;
  prompt: string;
  repeat: Repeat;
  time?: string;
  weekdays?: number[];
  every_minutes?: number;
}

/** The ＋ menu's starting points (the iOS app's AssistantRoutineTemplate). */
export const TEMPLATES: Template[] = [
  {
    key: "meeting",
    title: "会前准备",
    prompt:
      "看看今天飞书日历和 iPhone 日历里的会。对每个需要准备的会，用 schedule_followup 在会前 10 分钟安排一次会前准备：相关文档、上次的会议纪要、还没定的事、我该说的要点。今天没有要准备的会就简单说一声。",
    repeat: "daily",
    time: "08:45",
    weekdays: WORKDAYS,
  },
  {
    key: "weekly",
    title: "周报",
    prompt:
      "帮我起草这周的周报：从飞书日历、会议纪要、文档和消息里整理这周做了什么、进展和结果、遇到的问题、下周计划；各台电脑上的 Agent 这周完成的主要改动也算进去。先给我草稿，问我要不要改，不要直接发出去。",
    repeat: "daily",
    time: "17:00",
    weekdays: [5],
  },
  {
    key: "patrol",
    title: "Agent 巡检",
    prompt: "看看各台电脑上的 Agent：哪些在等我、哪些卡住或报错了、哪些跑完了没人看。有要我处理的就列出来（一行一个，说清楚在哪台电脑、等什么）；都正常就回一句「都正常」。",
    repeat: "interval",
    every_minutes: 30,
  },
  {
    key: "wrapup",
    title: "下班前盘点",
    prompt: "下班前盘点：今天还有哪些消息没回、答应别人的事还没做、各台电脑上还在跑或在等我的 Agent，明天上午有哪些会要准备。按紧急程度列出来，最后问我哪些要设提醒。",
    repeat: "daily",
    time: "18:30",
    weekdays: WORKDAYS,
  },
];

/** A blank task: every workday at 09:00. */
export function blankTask(): Task {
  return { id: "", title: "", prompt: "", repeat: "daily", time: "09:00", weekdays: WORKDAYS, every_minutes: 30, enabled: true };
}

export function fromTemplate(t: Template): Task {
  return { ...blankTask(), title: t.title, prompt: t.prompt, repeat: t.repeat, time: t.time ?? "09:00", weekdays: t.weekdays ?? [], every_minutes: t.every_minutes ?? 30 };
}

// ── lists ───────────────────────────────────────────────

/** Every day / every N minutes in the hub's order; one-offs soonest first. */
export function splitTasks(tasks: Task[]): { repeating: Task[]; oneOff: Task[] } {
  const oneOff = tasks.filter((t) => t.repeat === "once");
  const time = (t: Task) => nextRun(t)?.getTime() ?? parseTime(t.at)?.getTime() ?? Number.MAX_SAFE_INTEGER;
  return { repeating: tasks.filter((t) => t.repeat !== "once"), oneOff: oneOff.sort((a, b) => time(a) - time(b)) };
}

export interface Upcoming {
  id: string;
  at: Date;
  title: string;
  detail: string;
  repeating: boolean;
}

/** What Jarvis will do on its own next, soonest first; what is switched off is not coming up. */
export function upcoming(tasks: Task[], limit = 3): Upcoming[] {
  const out: Upcoming[] = [];
  for (const t of tasks) {
    const at = nextRun(t);
    if (!at) continue;
    out.push({ id: t.id, at, title: taskTitle(t), repeating: t.repeat !== "once", detail: t.repeat === "once" ? (t.session_id ? "跟进 · 接着原来的对话" : "跟进 · 新对话") : ruleText(t) });
  }
  return out.sort((a, b) => a.at.getTime() - b.at.getTime()).slice(0, limit);
}

// ── runs ────────────────────────────────────────────────

export function runStatusWord(r: Pick<Run, "status">): string {
  return r.status === "ok" ? "已运行" : r.status === "skipped" ? "已跳过" : "失败";
}

/** "手动运行" / the error / "准时" / "晚了 21 秒" / "晚了 3 分钟" / "晚了 2 小时". */
export function runDetail(r: Run): string {
  if (r.manual) return "手动运行";
  if (r.status !== "ok") return r.error || runStatusWord(r);
  const at = parseTime(r.at), due = parseTime(r.due);
  if (!at || !due) return runStatusWord(r);
  const late = (at.getTime() - due.getTime()) / 1000;
  if (late < 5) return "准时";
  if (late < 120) return `晚了 ${Math.round(late)} 秒`;
  if (late < 7200) return `晚了 ${Math.floor(late / 60)} 分钟`;
  return `晚了 ${Math.floor(late / 3600)} 小时`;
}

/** Newest first, one group per day ("今天", "昨天", "周三", "11月2日"). */
export function runDays(runs: Run[], now: Date): { day: string; runs: Run[] }[] {
  const out: { day: string; runs: Run[] }[] = [];
  const sorted = runs
    .map((r) => ({ r, at: parseTime(r.at) }))
    .sort((a, b) => (b.at?.getTime() ?? 0) - (a.at?.getTime() ?? 0));
  for (const { r, at } of sorted) {
    const day = at ? dayLabel(at, now) : "更早";
    if (out.length && out[out.length - 1].day === day) out[out.length - 1].runs.push(r);
    else out.push({ day, runs: [r] });
  }
  return out;
}
