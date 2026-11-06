// The task board (iOS TaskBoardSheet): one conversation's tasks and the agents its dock shows, by where they are —
// 等你 / 待验收 / 没开起来 / 在做 / 做完了 / 停着 / 已完成, what the user must act on first (#4896). Pure, so the dock's
// 全部 N and the board count the same things.
import { awaitsReview, type DockAgent, type Todo, type TodoStatus } from "./todos";
import { byTime } from "./times";

export type BoardSectionKey = "waiting" | "review" | "failed" | "working" | "unread" | "stopped" | "done";

export interface BoardRow {
  todo: Todo;
  status: TodoStatus;
}

export interface BoardSection {
  key: BoardSectionKey;
  rows: BoardRow[];
  agents: DockAgent[];
}

/** Which section a task is in: by its status, but a supervised one awaiting the user's review in 待验收, not with the
 *  stopped or finished-unseen ones (#4896). The web keeps 断了 (interrupted) apart; it waits for the user like 等你. */
export function boardSectionOf({ todo, status }: BoardRow): BoardSectionKey {
  if (awaitsReview(todo, status)) return "review";
  switch (status) {
    case "needs_you":
    case "interrupted":
      return "waiting";
    case "working":
    case "not_started":
      return "working";
    case "unread":
      return "unread";
    case "follow_up":
      return "stopped";
    case "done":
      return "done";
    case "failed":
      return "failed";
  }
}

export const BOARD_ORDER: BoardSectionKey[] = ["waiting", "review", "failed", "working", "unread", "stopped", "done"];

/** The board's sections that have something, in order; tasks newest first, agents waiting ones under 等你, the rest
 *  under 在做 (as the dock orders them). */
export function boardSections(rows: BoardRow[], agents: DockAgent[]): BoardSection[] {
  const by = new Map<BoardSectionKey, BoardSection>(BOARD_ORDER.map((key) => [key, { key, rows: [], agents: [] }]));
  const newest = [...rows].sort((a, b) => byTime(b.todo.created_at, a.todo.created_at));
  for (const r of newest) by.get(boardSectionOf(r))!.rows.push(r);
  for (const a of agents) by.get(a.state === "needs_you" ? "waiting" : "working")!.agents.push(a);
  return BOARD_ORDER.map((k) => by.get(k)!).filter((s) => s.rows.length > 0 || s.agents.length > 0);
}
