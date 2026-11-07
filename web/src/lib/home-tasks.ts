// The overview's "Tasks in progress" (#6450, the web side of the Lark bot's /tasks, #5432): every open task, manual ones
// too, in the Tasks page's order (sortTodos: what the user must act on first, then newest). Pure, so the section and its
// count agree.
import { sortTodos, todoStatus, type SessionLookup, type Todo, type TodoStatus } from "./todos";

/** How many tasks the overview lists; the rest are a "See all" away. */
export const HOME_TASKS_MAX = 6;

export interface HomeTask {
  todo: Todo;
  status: TodoStatus;
  /** Agents linked and not released, as the Tasks page counts them. */
  agents: number;
}

/** lookup is the page's sessions with the cards answered here applied (answeredLookup), as the Tasks page reads them. */
export function homeTasks(todos: Todo[], lookup: SessionLookup, max = HOME_TASKS_MAX): { rows: HomeTask[]; total: number } {
  const open = sortTodos(todos.filter((todo) => !todo.done_at), lookup);
  const rows = open.slice(0, max).map((todo) => ({ todo, status: todoStatus(todo, lookup), agents: (todo.agents ?? []).filter((a) => !a.released_at).length }));
  return { rows, total: open.length };
}
