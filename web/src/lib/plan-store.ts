// Each open chat's newest plan (its latest TodoWrite), for the side panel's Plan (#5602, after Claude Desktop: the
// chat keeps one line, the whole list lives in the panel). The chat publishes it as it changes; the panel follows.
import { useSyncExternalStore } from "react";
import type { Todo } from "./work-groups";

export interface ChatPlan {
  todos: Todo[];
  /** The agent is at work on it. */
  live: boolean;
}

const plans = new Map<string, ChatPlan>();
const listeners = new Set<() => void>();
const key = (mid: string, sid: string) => `${mid}/${sid}`;

export function publishPlan(mid: string, sid: string, plan: ChatPlan | undefined) {
  const k = key(mid, sid);
  const had = plans.get(k);
  if (!plan) {
    if (!had) return;
    plans.delete(k);
  } else {
    if (had && had.live === plan.live && had.todos === plan.todos) return;
    plans.set(k, plan);
  }
  listeners.forEach((l) => l());
}

export function usePlan(mid: string, sid: string): ChatPlan | undefined {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => plans.get(key(mid, sid)),
    () => undefined,
  );
}
