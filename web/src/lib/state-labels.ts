// The one set of names for an agent's and a task's state on the web (#3415; docs/i18n.md「Agent 状态」,
// docs/agent-states.md): every page takes its state words from here, so a state is never 需要你 on one page and 等你
// on the next. The words are in lib-state-labels.json; state-labels.test.ts keeps other namespaces from defining them.
import { t } from "../i18n";
import type { AgentState } from "./types";

/** An agent's state (AgentState), 待唤醒, and the states only a task or a summary has: 已完成, 中断了, 待跟进, 未开始, 没开起来. */
export type StateName = AgentState | "waiting" | "done" | "interrupted" | "follow_up" | "not_started" | "failed";

export function stateName(s: StateName): string {
  return t(`lib-state-labels.${s}`);
}

/** The states a list counts in its headers and summaries: 「3 个需要你」. */
export type CountedState = "needs_you" | "working" | "unread" | "done";

export function stateCount(s: CountedState, count: number): string {
  return t(`lib-state-labels.count_${s}`, { count });
}

/** 「全部标为已看」, with how many when given: what marks every 完成未看 agent seen. */
export function markAllSeenLabel(count?: number): string {
  return count === undefined ? t("lib-state-labels.mark_all_seen") : t("lib-state-labels.mark_all_seen_count", { count });
}

export const STATE_LABEL: Record<AgentState, string> = {
  needs_you: stateName("needs_you"),
  working: stateName("working"),
  unread: stateName("unread"),
  idle: stateName("idle"),
  offline: stateName("offline"),
  unknown: stateName("unknown"),
};
