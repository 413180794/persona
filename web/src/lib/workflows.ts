// Background workflows (#2935, docs/agent-states.md「后台 workflow、Monitor」): a Claude Code agent whose turn ended to
// wait for its Workflow run is still at work. What the rows, the chat's header and the task cards say about them.
// Newer hubs send Session.workflows; an older one only lists the run in background (kind "workflow").
import { t } from "../i18n";
import { took } from "./tasks";
import type { Session, Workflow, WorkflowAgent } from "./types";

export type WorkflowStatus = "running" | "completed" | "failed" | "killed" | "stalled";

export function workflowStatus(w: Workflow): WorkflowStatus {
  if (w.running) return "running";
  return (["failed", "killed", "stalled"] as const).find((s) => s === w.status) ?? "completed";
}

/** Its runs still running. */
export function runningWorkflows(s: Session | undefined): Workflow[] {
  return (s?.workflows ?? []).filter((w) => w.running);
}

/** Its background tasks other than workflow runs (those have their own cards), newest hub or not. */
export function otherBackground(s: Session | undefined) {
  const runs = s?.workflows;
  // An older hub lists a run only in background: there it is a task like the others.
  return (s?.background ?? []).filter((b) => b.kind !== "workflow" || !runs);
}

/** How many things run in its background: its background tasks, a running workflow counted once. */
export function backgroundCount(s: Session | undefined): number {
  return otherBackground(s).length + runningWorkflows(s).length;
}

/** Something still runs in its background (a command, subagent, Monitor or workflow): it is not done. */
export function backgroundRunning(s: Session | undefined): boolean {
  return backgroundCount(s) > 0 || Boolean(s?.subagents?.some((a) => a.running));
}

export function workflowName(w: Workflow): string {
  return w.name?.trim() || w.summary?.trim() || t("lib-workflows.untitled");
}

/** "Verify 2/2", "Verify", "" */
export function workflowPhase(w: Workflow): string {
  if (!w.phase) return "";
  const n = w.phases?.length ?? 0;
  return w.phase_index && n ? `${w.phase} ${w.phase_index}/${n}` : w.phase;
}

/** How long it has run (or ran): "14 分". */
export function workflowElapsed(w: Workflow, now: number): string {
  const start = Date.parse(w.started_at ?? "");
  if (!Number.isFinite(start)) return "";
  const end = w.running ? now : Date.parse(w.ended_at ?? "") || now;
  return took(Math.max(0, Math.floor((end - start) / 1000)));
}

/** "169/185" — the total is not known before it ends: done of those started so far. */
export function workflowCounts(w: Workflow): string {
  return `${w.agents_done ?? 0}/${w.agents_started ?? 0}`;
}

/** For a narrow row: what moves first, so truncation keeps it: 169/185 · parity-audit · Verify (+1). */
export function workflowShortLine(s: Session | undefined): string {
  const runs = runningWorkflows(s);
  if (!runs.length) return (s?.background ?? []).some((b) => b.kind === "workflow") ? t("lib-workflows.running_in_background") : "";
  const w = runs[0];
  const line = [workflowCounts(w), workflowName(w), w.phase ?? ""].filter(Boolean).join(" · ");
  return runs.length > 1 ? `${line} +${runs.length - 1}` : line;
}

/** One line for a row or a card: 后台 workflow「parity-audit」· Verify 2/2 · 169/185 · 14 分（等 2 个）. */
export function workflowLine(s: Session | undefined, now: number): string {
  const runs = runningWorkflows(s);
  if (!runs.length) {
    // an older hub: it only says a workflow runs
    return (s?.background ?? []).some((b) => b.kind === "workflow") ? t("lib-workflows.running_in_background") : "";
  }
  const w = runs[0];
  const parts = [t("lib-workflows.line_name", { name: workflowName(w) }), workflowPhase(w), workflowCounts(w), workflowElapsed(w, now)].filter(Boolean);
  const line = parts.join(" · ");
  return runs.length > 1 ? t("lib-workflows.line_more", { line, count: runs.length }) : line;
}

/** Its agents for the card: running first, then the latest started. */
export function workflowAgents(w: Workflow): WorkflowAgent[] {
  const list = [...(w.agents ?? [])].reverse();
  return [...list.filter((a) => a.running), ...list.filter((a) => !a.running)];
}

/** The workflow agent with this session id among s's runs, for the side panel's title. */
export function workflowAgentFor(s: Session | undefined, sid: string): { run: Workflow; agent: WorkflowAgent } | null {
  for (const run of s?.workflows ?? []) {
    const agent = run.agents?.find((a) => a.session_id === sid);
    if (agent) return { run, agent };
  }
  return null;
}
