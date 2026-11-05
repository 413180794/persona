// Tasks dispatched from an assistant conversation (docs/unified-entry.md, iOS TasksStore / Shared/Tasks.swift): the plans,
// each conversation's feed (what the hub inserted into it), where those go among the chat's lines, the day lines and
// the quick results. A task is a todo whose `origin` is the conversation; its state is the todo's (lib/todos.ts).
import { useSyncExternalStore } from "react";
import { api, enc } from "./api";
import { formatDate, t } from "../i18n";
import { parseAPIError } from "./api-errors";
import { getState, onHubEvent, putTodo, refreshTodos } from "./store";
import { applySnapshot } from "./assistant-views";
import type { AssistantView, ViewAction } from "./extension-wire";
import { isResumed } from "./reconnect";
import { deleteTodo, type Todo } from "./todos";
import { itemPath } from "./inbox";
import { closePanel, getPanel } from "./panel";
import type { ChatLine } from "./turns";
import type { Item } from "./types";

export interface PlanTask {
  title: string;
  prompt: string;
  machine: string;
  cwd: string;
  launcher?: string;
  model?: string;
  permission?: string;
  readonly?: boolean;
  why?: string;
  todo_id?: string;
  error?: string;
}

export interface TaskPlan {
  id: string;
  conversation: string;
  tasks: PlanTask[];
  state: "open" | "started" | "dismissed";
  created_at?: string;
}

export type FeedKind = "plan" | "task" | "answered" | "receipt" | "routed" | "today" | "message" | "view" | "review";

export interface FeedItem {
  id: string;
  conversation: string;
  at: string;
  kind: FeedKind;
  ref?: string;
  body?: Record<string, unknown>;
}

export interface TodayBody {
  date: string;
  done?: { id: string; title: string }[];
  waiting?: { id: string; title: string }[];
  working?: number;
  meetings?: { start: string; end: string; title: string }[];
  at_me?: { name: string; unread?: number; preview?: string }[];
  unread?: number;
}

/** A string field of an item's body. */
export function field(it: FeedItem, key: string): string | undefined {
  const v = it.body?.[key];
  return typeof v === "string" ? v : undefined;
}

/** A receipt's outcome: an `interrupted` one (#1516), or a `done` one from a hub before it whose summary is the API
 *  error that cut the turn off, is not done. */
export function receiptState(it: FeedItem): "done" | "failed" | "interrupted" {
  const st = field(it, "state");
  if (st === "failed" || st === "interrupted") return st;
  return parseAPIError(field(it, "summary")) ? "interrupted" : "done";
}

/** One question of an answered card and what the user answered. */
export interface AnsweredItem {
  question: string;
  answer: string;
}

/** `answered`: every question of the card with the user's answer in full, newlines kept (#1231). A record from before
 *  it has only its line (question, answer), which stands in; empty when there is nothing to show. */
export function answeredItems(it: FeedItem): AnsweredItem[] {
  const str = (v: unknown) => (typeof v === "string" ? v : "");
  const raw = it.body?.items;
  if (Array.isArray(raw)) {
    const out = raw.flatMap((x): AnsweredItem[] => {
      if (!x || typeof x !== "object") return [];
      const o = x as Record<string, unknown>;
      const one = { question: str(o.question), answer: str(o.answer) };
      return one.question || one.answer ? [one] : [];
    });
    if (out.length) return out;
  }
  const line = { question: field(it, "question") ?? "", answer: field(it, "answer") ?? "" };
  return line.question || line.answer ? [line] : [];
}

// ── state ──

interface FeedState {
  feeds: Record<string, FeedItem[]>;
  plans: Record<string, TaskPlan>;
  /** Conversation → the task the next message goes to (「对「xx」说」). */
  routes: Record<string, string>;
  busy: Record<string, boolean>;
  /** Message cards put off with 稍后. */
  later: Record<string, boolean>;
}

let st: FeedState = { feeds: {}, plans: {}, routes: {}, busy: {}, later: {} };
const listeners = new Set<() => void>();
function set(patch: Partial<FeedState>) {
  st = { ...st, ...patch };
  listeners.forEach((l) => l());
}

export function useFeed<T>(select: (s: FeedState) => T): T {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => select(st),
    () => select(st), // static rendering (tests)
  );
}

export function getFeedState() {
  return st;
}

/** Puts items into a conversation's feed, oldest first, without repeats; one it has whose body changed (a message
 *  card sent) takes the new body. */
export function mergeInto(list: FeedItem[], items: FeedItem[]): FeedItem[] {
  const have = new Map(list.map((i) => [i.id, i]));
  const changed = new Map(items.filter((i) => have.has(i.id) && JSON.stringify(i.body) !== JSON.stringify(have.get(i.id)!.body)).map((i) => [i.id, i]));
  const fresh = items.filter((i) => !have.has(i.id));
  if (!fresh.length && !changed.size) return list;
  const kept = changed.size ? list.map((i) => changed.get(i.id) ?? i) : list;
  if (!fresh.length) return kept;
  return [...kept, ...fresh].sort((a, b) => (a.at === b.at ? a.id.localeCompare(b.id) : Date.parse(a.at) - Date.parse(b.at)));
}

/** A message card whose reply went out — from this card or from the inbox, on any device (the hub records it on the
 *  card): when, and what it said (a card sent before the hub recorded it: its draft). */
export function cardSent(it: FeedItem): { at: string; text: string } | undefined {
  const at = field(it, "sent_at");
  return at ? { at, text: field(it, "sent") || field(it, "draft") || "" } : undefined;
}

/** A blank a draft leaves to fill in (【预计几点前】, {{链接}}): it would go out as it is. */
export function draftPlaceholder(text: string): string | undefined {
  return /【[^】\n]*】|\{\{[^}\n]*\}\}/.exec(text)?.[0];
}

function received(items: FeedItem[], plans?: Record<string, TaskPlan>) {
  const feeds = { ...st.feeds };
  for (const it of items) feeds[it.conversation] = mergeInto(feeds[it.conversation] ?? [], [it]);
  set({ feeds, plans: plans ? { ...st.plans, ...plans } : st.plans });
  if (items.some((i) => i.kind === "task" || i.kind === "receipt")) void refreshTodos();
}

const loading = new Set<string>();
const todayAsked = new Map<string, string>();

/** The most items the hub answers at once (docs/protocol.md: oldest first, at most 500): a full page has more after it. */
const FEED_PAGE = 500;
/** Pages read in one load at most. */
const FEED_PAGES = 40;

/** A conversation's feed: what came since the newest item it has. */
export async function loadFeed(conversation: string) {
  if (loading.has(conversation)) return;
  loading.add(conversation);
  try {
    let last = st.feeds[conversation]?.at(-1)?.at;
    for (let n = 0; n < FEED_PAGES; n++) {
      const res = await api<{ items: FeedItem[]; plans?: Record<string, TaskPlan>; views?: Record<string, AssistantView>; view_actions?: Record<string, ViewAction>; view_blocks?: string[] }>(
        `/assistant/conversations/${enc(conversation)}/feed${last ? `?since=${encodeURIComponent(last)}` : ""}`,
      );
      if (!st.feeds[conversation]) set({ feeds: { ...st.feeds, [conversation]: [] } });
      const items = res.items ?? [];
      received(items, res.plans ?? undefined);
      // the cards' snapshot comes whole with every page: what an event missed comes back here
      applySnapshot(conversation, res);
      const newest = items.at(-1)?.at;
      // the next page starts after this one's newest; one that did not move on would come back the same
      if (items.length < FEED_PAGE || !newest || (last && Date.parse(newest) <= Date.parse(last))) break;
      last = newest;
    }
  } catch {
    // an older hub has no feed: the conversation shows as before
  } finally {
    loading.delete(conversation);
  }
}

/** The main conversation's 今天 card, once a day (the hub makes it on the first ask). */
export async function loadToday(conversation: string) {
  const day = new Date().toLocaleDateString("sv");
  if (todayAsked.get(conversation) === day) return;
  todayAsked.set(conversation, day);
  try {
    const res = await api<{ item: FeedItem }>(`/assistant/conversations/${enc(conversation)}/today`, { method: "POST" });
    if (res.item) received([res.item]);
  } catch {
    todayAsked.delete(conversation);
  }
}

onHubEvent("assistant_feed", (m) => {
  const it = m.item as FeedItem | undefined;
  if (it) received([it]);
});
// A connection that did not resume missed events: the conversations open here read their feed (and cards) again.
onHubEvent("hello", (m) => {
  if (isResumed(m)) return;
  for (const c of Object.keys(st.feeds)) void loadFeed(c);
});
onHubEvent("task_plan", (m) => {
  const p = m.plan as TaskPlan | undefined;
  if (p) set({ plans: { ...st.plans, [p.id]: p } });
});

// ── actions ──

/** The fields of a plan's task 改一下 lets the user change before it starts (iOS PlanEditSheet). */
export type PlanTaskEdit = Pick<PlanTask, "title" | "machine" | "cwd" | "model" | "permission" | "prompt">;

export const PLAN_PERMISSIONS = ["default", "acceptEdits", "plan", "bypassPermissions"] as const;

/** A plan's tasks as 改一下 starts them: every editable field filled in (an unset permission is 每步都问). */
export function planEditsOf(plan: TaskPlan): PlanTaskEdit[] {
  return plan.tasks.map((t) => ({ title: t.title, machine: t.machine, cwd: t.cwd, model: t.model ?? "", permission: t.permission || "default", prompt: t.prompt }));
}

/** Whether 改一下 changed anything (closing it then asks before throwing the changes away). */
export function planEditsChanged(plan: TaskPlan, edits: PlanTaskEdit[]): boolean {
  const before = planEditsOf(plan);
  return edits.some((e, i) => (Object.keys(e) as (keyof PlanTaskEdit)[]).some((k) => (e[k] ?? "") !== (before[i]?.[k] ?? "")));
}

/** What 开始 sends from 改一下: the edited tasks, leaving out a 每步都问 the plan never set (the agent's own default). */
export function planEditsToSend(plan: TaskPlan, edits: PlanTaskEdit[]): Partial<PlanTask>[] {
  return edits.map((e, i) => {
    const out: Partial<PlanTask> = { ...e };
    if (e.permission === "default" && !plan.tasks[i]?.permission) delete out.permission;
    return out;
  });
}

export async function startPlan(plan: TaskPlan, remember: boolean, edits?: Partial<PlanTask>[]) {
  if (st.busy[plan.id]) return;
  set({ busy: { ...st.busy, [plan.id]: true } });
  try {
    const res = await api<{ plan: TaskPlan }>(`/task-plans/${enc(plan.id)}/start`, { method: "POST", json: { tasks: edits, remember } });
    set({ plans: { ...st.plans, [res.plan.id]: res.plan } });
    void refreshTodos();
  } finally {
    set({ busy: { ...st.busy, [plan.id]: false } });
  }
}

export async function dismissPlan(plan: TaskPlan) {
  if (st.busy[plan.id]) return;
  set({ busy: { ...st.busy, [plan.id]: true } });
  try {
    const res = await api<{ plan: TaskPlan }>(`/task-plans/${enc(plan.id)}/dismiss`, { method: "POST" });
    set({ plans: { ...st.plans, [res.plan.id]: res.plan } });
  } finally {
    set({ busy: { ...st.busy, [plan.id]: false } });
  }
}

/** The busy key of a task's 重试 (busy is keyed by plan id otherwise). */
export const retryKey = (todoId: string) => `retry:${todoId}`;

/**
 * 重试 on a task whose agent did not start (#1396): the hub starts the same task again (its plan's machine, directory,
 * prompt, launcher, model and permission) in the same todo, which it reopens at once — the card shows it starting
 * where it was. Taps while one is on its way do nothing; the hub starts one agent however many arrive. On a task the
 * model service cut off (#1516) it is 继续: the hub tells its agent to go on, once.
 */
export async function retryTask(todo: Todo) {
  const key = retryKey(todo.id);
  if (st.busy[key]) return;
  set({ busy: { ...st.busy, [key]: true } });
  try {
    const res = await api<{ todo: Todo }>(`/todos/${enc(todo.id)}/retry`, { method: "POST" });
    if (res.todo) putTodo(res.todo);
    void refreshTodos();
  } finally {
    set({ busy: { ...st.busy, [key]: false } });
  }
}

/**
 * Why a task's agent did not start: the todo's own word (#1077), else — from a hub before it, which left the todo open
 * — its plan's, while it has no agent. Undefined while it may still start.
 */
export function startFailure(todo: Todo, plans: Record<string, TaskPlan>): string | undefined {
  if (todo.start_error) return todo.start_error;
  if ((todo.agents ?? []).length > 0 || !todo.plan_id) return undefined;
  return plans[todo.plan_id]?.tasks.find((t) => t.todo_id === todo.id)?.error || undefined;
}

/**
 * Removes a task that does not run: its todo goes (its agents and their chats stay on the Agents page), and with it
 * its card, pill and panel. One whose agents ran asks first; one whose agent never started goes at once. Answers
 * whether it went.
 */
export async function removeTask(todo: Todo, ask: (message: string) => boolean = (m) => confirm(m)): Promise<boolean> {
  if ((todo.agents ?? []).length > 0 && !ask(t("lib-feed.remove_confirm", { title: todo.title }))) return false;
  await deleteTodo(todo.id);
  const routes = { ...st.routes };
  for (const [c, id] of Object.entries(routes)) if (id === todo.id) delete routes[c];
  set({ routes });
  const panel = getPanel();
  if (panel?.kind === "task" && panel.todo === todo.id) closePanel();
  await refreshTodos();
  return true;
}

export function setRoute(conversation: string, todoId: string | null) {
  const routes = { ...st.routes };
  if (todoId) routes[conversation] = todoId;
  else delete routes[conversation];
  set({ routes });
}

/** Sends text straight to the task the conversation is routed to; false when none (send it to Persona as usual). */
export async function sendRouted(conversation: string, text: string): Promise<boolean> {
  const todoId = st.routes[conversation];
  if (!todoId) return false;
  // A failed send keeps the route for the retry, so a task gone elsewhere (its chip no longer drawn) must not hold it.
  const { todos, todosLoaded } = getState();
  if (todosLoaded && !todos.some((t) => t.id === todoId)) {
    setRoute(conversation, null);
    return false;
  }
  const res = await api<{ item: FeedItem }>(`/assistant/conversations/${enc(conversation)}/route`, { method: "POST", json: { todo_id: todoId, text } });
  if (st.routes[conversation] === todoId) setRoute(conversation, null);
  if (res.item) received([res.item]);
  return true;
}

/**
 * 发送 on a message card: the (edited) draft goes out through the inbox, which files the item as done and records it
 * on the card (the hub sends once per card, however many taps or devices). The card shows it sent once the hub says
 * so; taps while it is on its way do nothing. A draft with a blank to fill in is not sent.
 */
export async function sendMessage(card: FeedItem, text: string) {
  const itemId = card.ref;
  const latest = () => st.feeds[card.conversation]?.find((i) => i.id === card.id) ?? card;
  if (!itemId || st.busy[card.id] || cardSent(latest())) return;
  const blank = draftPlaceholder(text);
  if (blank) throw new Error(t("lib-feed.draft_blank", { blank }));
  set({ busy: { ...st.busy, [card.id]: true } });
  try {
    await api(`${itemPath(itemId)}/reply`, { method: "POST", json: { text, card: card.id } });
    // The hub's assistant_feed event brings the card as it recorded it; this draws it sent without waiting.
    const now = latest();
    if (!cardSent(now)) received([{ ...now, body: { ...now.body, sent_at: new Date().toISOString(), sent: text } }]);
  } finally {
    set({ busy: { ...st.busy, [card.id]: false } });
  }
}

export function later(itemId: string) {
  set({ later: { ...st.later, [itemId]: true } });
}

// ── the conversation's lines ──

export interface QuickResult {
  kind: "reminder" | "event";
  callId: string;
  title: string;
  when?: string;
  place?: string;
}

export type DisplayLine =
  | { kind: "chat"; id: string; line: ChatLine }
  | { kind: "feed"; id: string; item: FeedItem }
  | { kind: "quick"; id: string; result: QuickResult }
  | { kind: "day"; id: string; date: Date };

/** When the user sent the line's message (feed items go before the first message sent after them). */
function inputTime(line: ChatLine): number | undefined {
  if (line.kind !== "segment" || !line.seg.user?.ts) return undefined;
  return Date.parse(line.seg.user.ts);
}

/** When a line happened, for the day lines. */
function lineTime(l: DisplayLine): number | undefined {
  switch (l.kind) {
    case "feed":
      return Date.parse(l.item.at);
    case "chat": {
      const c = l.line;
      if (c.kind === "segment") return Date.parse(c.seg.user?.ts ?? c.seg.start ?? "") || undefined;
      if (c.kind === "notice" || c.kind === "apiError" || c.kind === "compact") return Date.parse(c.item.ts ?? "") || undefined;
      return undefined;
    }
    default:
      return undefined;
  }
}

/** A reminder or an event the assistant added on the phone, from its tool call. */
export function quickResult(use: Item, result?: Item): QuickResult | undefined {
  const short = (use.tool_name ?? "").split("__").at(-1);
  const kind = short === "phone_reminders_create" ? "reminder" : short === "phone_calendar_create_event" ? "event" : undefined;
  if (!kind || !result || result.is_error || !result.text) return undefined;
  try {
    const o = JSON.parse(result.text) as Record<string, unknown>;
    if (typeof o.id !== "string" || !o.id) return undefined;
    const when = kind === "reminder" ? o.due : o.start;
    const place = kind === "reminder" ? o.list : o.calendar;
    return { kind, callId: use.id, title: String(o.title ?? ""), when: typeof when === "string" ? when : undefined, place: typeof place === "string" ? place : undefined };
  } catch {
    return undefined;
  }
}

/** The chat's lines with the feed among them (each item at the end of the exchange it happened in), each exchange's
 *  quick results after it, and a day line over the first line of each day. */
export function displayLines(lines: ChatLine[], feed: FeedItem[]): DisplayLine[] {
  const out: DisplayLine[] = [];
  let next = 0;
  for (const line of lines) {
    const at = inputTime(line);
    if (at !== undefined) {
      while (next < feed.length && Date.parse(feed[next].at) < at) {
        out.push({ kind: "feed", id: "feed-" + feed[next].id, item: feed[next] });
        next++;
      }
    }
    out.push({ kind: "chat", id: line.id, line });
    if (line.kind === "segment") {
      for (const step of line.seg.steps) {
        if (step.kind !== "tool") continue;
        const q = quickResult(step.use, step.result);
        if (q) out.push({ kind: "quick", id: "quick-" + q.callId, result: q });
      }
    }
  }
  for (; next < feed.length; next++) out.push({ kind: "feed", id: "feed-" + feed[next].id, item: feed[next] });
  return withDays(out);
}

/** A day line over the first line of each later day. The lines keep their order, which is not always the order of their
 *  times (an exchange's feed items, a clock that was behind): a line from a day already passed gets no line of its own,
 *  so the days only go forward and each day line (`day-<date>`) is there once. */
export function withDays(lines: DisplayLine[], now = Date.now()): DisplayLine[] {
  const out: DisplayLine[] = [];
  let last = "";
  for (const l of lines) {
    const t = lineTime(l);
    // a day line only forward (a line from a clock that was behind is not a day back, nor the same day twice), and none
    // from a line dated after now (a clock ahead would take every day after it with it)
    if (t !== undefined && !Number.isNaN(t) && t <= now + 60_000) {
      const d = new Date(t);
      // YYYY-MM-DD: compares as a string
      const day = d.toLocaleDateString("sv");
      if (day > last) {
        out.push({ kind: "day", id: "day-" + day, date: new Date(d.getFullYear(), d.getMonth(), d.getDate()) });
        last = day;
      }
    }
    out.push(l);
  }
  return out;
}

/** 今天 · 11月3日 周二, 昨天 · …, or the date. */
export function dayLabel(date: Date, now = new Date()): string {
  const day = formatDate(date, { month: "long", day: "numeric", weekday: "short" });
  const same = (a: Date, b: Date) => a.toDateString() === b.toDateString();
  if (same(date, now)) return t("lib-feed.today", { day });
  const y = new Date(now);
  y.setDate(now.getDate() - 1);
  if (same(date, y)) return t("lib-feed.yesterday", { day });
  if (date.getFullYear() !== now.getFullYear()) return formatDate(date, { year: "numeric", month: "long", day: "numeric", weekday: "short" });
  return day;
}

export function isToday(iso: string, now = new Date()): boolean {
  return new Date(iso).toDateString() === now.toDateString();
}
