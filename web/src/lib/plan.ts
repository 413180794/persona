// The agent's own task list (docs/integrations/agent-goal.md "Plan"): Claude Code's TodoWrite, or TaskCreate / TaskUpdate, as the
// node counts it (Session.plan). Task cards and agent rows show it as "3/5 · Reproducing the crash".
import { t } from "../i18n";
import type { Session } from "./types";
import type { Supervision } from "./supervision";

/** The plan in a short line, "" when the session has none. */
export function planLine(s: Pick<Session, "plan"> | undefined): string {
  const p = s?.plan;
  if (!p || !p.total) return "";
  const current = p.current?.trim();
  return current ? t("agent-goal.plan_line", { done: p.done, total: p.total, current }) : t("agent-goal.plan_count", { done: p.done, total: p.total });
}

/** The plan for a screen reader and a tooltip: "Plan, 3 of 5 done: Reproducing the crash". */
export function planLabel(s: Pick<Session, "plan"> | undefined): string {
  const p = s?.plan;
  if (!p || !p.total) return "";
  const current = p.current?.trim();
  return current ? t("agent-goal.plan_label_current", { done: p.done, total: p.total, current }) : t("agent-goal.plan_label", { done: p.done, total: p.total });
}

/** Whether a task card shows its agent's plan: it has one, and no report to the supervisor came after it (#5271; a
 *  report says more than the count). */
export function planShown(s: Pick<Session, "plan"> | undefined, sup: Supervision | undefined): boolean {
  const p = s?.plan;
  if (!p || !p.total) return false;
  const reported = Date.parse(sup?.progress?.text ? (sup.progress.at ?? "") : "");
  const planned = Date.parse(p.at ?? "");
  return !(Number.isFinite(reported) && (!Number.isFinite(planned) || reported > planned));
}

/** Whether an agent row shows the plan: while the agent works, or while items are left (a finished plan on an idle
 *  row says nothing its state does not). */
export function planInRow(s: Pick<Session, "plan">, working: boolean): boolean {
  const p = s.plan;
  return !!p && p.total > 0 && (working || p.done < p.total);
}
