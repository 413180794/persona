// Auto flow (#2648): the board's optional second view, derived from live agent and to-do state.
import { stateOf } from "./format";
import { decisionDismissed, reviewState } from "./supervision";
import { todoWorkStatus } from "./todo-work-status";
import { sameDirectory } from "./workspace-board";
import { acceptedAtRest, askTake, openAsk, type SessionLookup, type Todo } from "./todos";
import type { BoardCard } from "./workspace-board";
import type { Session } from "./types";

export type AutoLane = "needs_you" | "working" | "unread" | "idle";
export const AUTO_LANES: readonly AutoLane[] = ["needs_you", "working", "unread", "idle"];

function sessionLane(s: Session): AutoLane {
  const state = stateOf(s);
  return state === "needs_you" || state === "working" || state === "unread" ? state : "idle";
}

/** A workspace follows its most urgent session. */
export function autoLaneOf(sessions: readonly Session[]): AutoLane {
  const lanes = new Set(sessions.map(sessionLane));
  return AUTO_LANES.find((lane) => lanes.has(lane)) ?? "idle";
}

/** Latest agent activity, for ordering; cards without any go last. */
export function autoActivity(sessions: readonly Session[]): number {
  let latest = 0;
  for (const s of sessions) {
    const at = Date.parse(s.updated_at ?? s.state_since ?? "");
    if (Number.isFinite(at) && at > latest) latest = at;
  }
  return latest;
}

/** The most recently active session in the state that placed the workspace in its lane. */
export function autoFocusSession(sessions: readonly Session[]): Session | undefined {
  const lane = autoLaneOf(sessions);
  const pool = sessions.filter((s) => sessionLane(s) === lane);
  return [...pool].sort((a, b) => autoActivity([b]) - autoActivity([a]))[0];
}

/** One lane's cards: most recent agent activity first, then pinned, then title so the order is stable. */
export function autoSort(cards: readonly BoardCard[], sessionsOf: (card: BoardCard) => readonly Session[]): BoardCard[] {
  return [...cards].sort((a, b) => {
    const activity = autoActivity(sessionsOf(b)) - autoActivity(sessionsOf(a));
    if (activity) return activity;
    if (a.pinned !== b.pinned) return a.pinned ? -1 : 1;
    return a.title.localeCompare(b.title) || a.id.localeCompare(b.id);
  });
}

/** The lanes and their cards for the auto view, after the board's own search/machine/archived filters. */
export function autoLanes(
  cards: readonly BoardCard[],
  sessionsOf: (card: BoardCard) => readonly Session[],
): { lane: AutoLane; cards: BoardCard[] }[] {
  return AUTO_LANES.map((lane) => ({
    lane,
    cards: autoSort(cards.filter((c) => autoLaneOf(sessionsOf(c)) === lane), sessionsOf),
  }));
}

// ---- All work in one panel (#2648): not only bound workspaces. Unbound agent sessions and to-dos enter the same
// lanes, so the auto view is "everything in flight", not only what the user bound by hand.

export type WorkLane = AutoLane | "review" | "planned" | "done";
export const WORK_LANES: readonly WorkLane[] = ["needs_you", "working", "review", "unread", "planned", "idle", "done"];

export interface WorkItem {
  kind: "workspace" | "session" | "todo";
  id: string;
  lane: WorkLane;
  title: string;
  activity: number;
  pinned: boolean;
  card?: BoardCard;
  session?: Session;
  todo?: Todo;
}

export function workspaceItem(card: BoardCard, sessions: readonly Session[]): WorkItem {
  return { kind: "workspace", id: `workspace:${card.id}`, lane: autoLaneOf(sessions), title: card.title, activity: autoActivity(sessions), pinned: card.pinned, card };
}

export function sessionItem(s: Session): WorkItem {
  return { kind: "session", id: `session:${s.machine_id}/${s.session_id}`, lane: sessionLane(s), title: s.title || s.session_id, activity: autoActivity([s]), pinned: false, session: s };
}

function todoLane(todo: Todo, lookup?: SessionLookup): WorkLane {
  const status = todoWorkStatus(todo, lookup);
  const ask = openAsk(todo);
  const action = ask && askTake(ask);
  // An auto-closed task can still carry a live verification request.
  if (action === "accept") return "review";
  if (status === "needs_you" || status === "interrupted" || status === "usage_limit" || status === "failed") return "needs_you";
  if (!todo.done_at && !decisionDismissed(todo) && (ask?.outcome === "decide" || reviewState(todo) === "needs_user")) return "needs_you";
  if (status === "working") return "working";
  if (action === "ack") return "unread";
  if (status === "done" || acceptedAtRest(todo, status)) return "done";
  if (status === "unread") return "unread";
  return "planned";
}

export function todoItem(t: Todo, lookup?: SessionLookup): WorkItem {
  const lane = todoLane(t, lookup);
  return { kind: "todo", id: `todo:${t.id}`, lane, title: t.title, activity: Date.parse(t.updated_at ?? "") || 0, pinned: false, todo: t };
}

/** Sessions no workspace card claims (a different machine or directory), so they appear on the board too. The main
 *  assistant conversation is not an agent task and stays out. */
export function unboundSessions(sessions: readonly Session[], cards: readonly BoardCard[]): Session[] {
  return sessions.filter((s) => !s.assistant && !cards.some((c) => c.machine_id === s.machine_id && sameDirectory(s.workspace_cwd || s.cwd, c.cwd)));
}

/** Every work item on the board, in its lane: most recent activity first, then pinned, then title. */
export function workLanes(items: readonly WorkItem[]): { lane: WorkLane; items: WorkItem[] }[] {
  return WORK_LANES.map((lane) => ({
    lane,
    items: items
      .filter((i) => i.lane === lane)
      .sort((a, b) => (b.activity - a.activity) || (a.pinned === b.pinned ? 0 : a.pinned ? -1 : 1) || a.title.localeCompare(b.title) || a.id.localeCompare(b.id)),
  }));
}
