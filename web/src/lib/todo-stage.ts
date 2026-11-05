// The stage a todo's work has reached (#2776): the six steps it goes through, in order. The hub sends one of these in
// a todo's `stage` (server/internal/assistant/todos.go); a hub that sends nothing, or a value this build doesn't know
// (an older or a newer hub), draws no bar at all.
export const TODO_STAGES = ["plan", "develop", "review", "merge", "deploy", "verify"] as const;

export type TodoStage = (typeof TODO_STAGES)[number];

export type TodoStageState = "done" | "current" | "ahead";

/** Where a stage sits among the six: -1 when the hub sent nothing or something we don't know. */
export function todoStageIndex(stage: string | null | undefined): number {
  if (!stage) return -1;
  return (TODO_STAGES as readonly string[]).indexOf(stage);
}

/** The steps before the current one are done, the ones after it are still ahead. */
export function todoStageState(index: number, current: number): TodoStageState {
  return index < current ? "done" : index === current ? "current" : "ahead";
}
