import { hasRunningTaskWork } from "./background-work";
import { todoStatus, type SessionLookup, type Todo, type TodoStatus } from "./todos";

/** The board and detail describe current work even when another linked session has an unread turn. */
export function todoWorkStatus(todo: Todo, lookup?: SessionLookup): TodoStatus {
  const status = todoStatus(todo, lookup);
  return status === "unread" && hasRunningTaskWork(todo, lookup) ? "working" : status;
}
