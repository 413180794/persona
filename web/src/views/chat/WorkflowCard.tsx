// A background workflow run (#2935, docs/agent-states.md「后台 workflow、Monitor」), in the chat's background dialog:
// its name, status and time, where it is among its phases, how many of its agents are done, and — open — each agent
// with its last words; an agent's whole conversation opens read-only in the side panel, as a subagent's does.
import { useState } from "react";
import { t } from "../../i18n";
import { openPanel } from "../../lib/panel";
import type { Workflow, WorkflowAgent } from "../../lib/types";
import { workflowAgents, workflowCounts, workflowElapsed, workflowName, workflowStatus, type WorkflowStatus } from "../../lib/workflows";
import { cx, StatusDot, useNow } from "../../ui/atoms";
import { Icon } from "../../ui/icons";

const STATUS_TONE: Record<WorkflowStatus, string> = {
  running: "bg-orange-tint text-orange",
  completed: "bg-green-tint text-green",
  failed: "bg-red-tint text-red",
  killed: "bg-red-tint text-red",
  stalled: "bg-orange-tint text-orange",
};

export function WorkflowCard({ run, mid, onOpen }: { run: Workflow; mid: string; onOpen?: () => void }) {
  const now = useNow(30_000, run.running);
  const [open, setOpen] = useState(false);
  const status = workflowStatus(run);
  const agents = workflowAgents(run);
  const running = run.agents_running ?? 0;
  return (
    <div data-workflow-card={status} className="flex flex-col overflow-hidden rounded-card bg-surface shadow-hairline">
      <div className="flex flex-col gap-2 px-3.5 py-3">
        <div className="flex items-center gap-2">
          <Icon name="layers" size={14} stroke={2.2} className="shrink-0 text-orange" />
          <span className="min-w-0 flex-1 truncate text-[13px] font-semibold text-ink" title={run.summary}>
            {workflowName(run)}
          </span>
          <span className={cx("flex h-5 shrink-0 items-center rounded-full px-2 text-[11px] font-medium whitespace-nowrap", STATUS_TONE[status])}>
            {t(`lib-workflows.status_${status}`)}
          </span>
          <span className="shrink-0 text-[12px] whitespace-nowrap text-ink-3 tabular-nums">{workflowElapsed(run, now)}</span>
        </div>
        {(run.phases?.length ?? 0) > 0 && <Phases run={run} />}
        <span className="text-[12px] text-ink-2 tabular-nums">
          {t("lib-workflows.agents_progress", { done: run.agents_done ?? 0, started: run.agents_started ?? 0 })}
          {run.running && running > 0 && <span className="text-ink-3"> · {t("lib-workflows.agents_running", { count: running })}</span>}
        </span>
        {run.error && status !== "completed" && <span className="truncate text-[12px] text-red">{run.error}</span>}
      </div>
      {agents.length > 0 && (
        <>
          <button
            type="button"
            aria-expanded={open}
            onClick={() => setOpen((v) => !v)}
            className="flex h-9 items-center gap-1 border-t border-line px-3.5 text-[12px] font-medium whitespace-nowrap text-accent-ink transition-colors hover:bg-hover"
          >
            <Icon name="chevron" size={12} stroke={2.2} className={cx("transition-transform", !open && "-rotate-90")} />
            {open ? t("lib-workflows.hide_agents") : t("lib-workflows.show_agents", { count: agents.length })}
            <span className="flex-1" />
            <span className="text-ink-3 tabular-nums">{workflowCounts(run)}</span>
          </button>
          {open && (
            <ul className="flex max-h-[320px] flex-col overflow-y-auto border-t border-line">
              {agents.map((a, i) => (
                <AgentRow
                  key={a.id}
                  a={a}
                  first={i === 0}
                  onOpen={() => {
                    onOpen?.();
                    openPanel({ kind: "agent", mid, sid: a.session_id });
                  }}
                />
              ))}
            </ul>
          )}
        </>
      )}
    </div>
  );
}

/** The script's phases in a row: those before the current one done, the current one marked. */
function Phases({ run }: { run: Workflow }) {
  const phases = run.phases ?? [];
  const at = run.phase_index ? run.phase_index - 1 : phases.indexOf(run.phase ?? "");
  return (
    <ol className="flex min-w-0 items-center gap-1 overflow-hidden">
      {phases.map((p, i) => {
        const state = at < 0 ? "next" : i < at || (!run.running && i === at && workflowStatus(run) === "completed") ? "done" : i === at ? "current" : "next";
        return (
          <li key={`${i}:${p}`} className="flex min-w-0 items-center gap-1" aria-label={t("lib-workflows.phase_aria", { state: t(`lib-workflows.phase_${state}`), phase: p })}>
            {i > 0 && <span aria-hidden="true" className={cx("h-px w-3 shrink-0", state === "next" ? "bg-line-strong" : "bg-orange")} />}
            <span
              className={cx(
                "flex h-5 min-w-0 items-center gap-1 rounded-full px-2 text-[11px] whitespace-nowrap",
                state === "current" ? (run.running ? "bg-orange-tint font-semibold text-orange" : "bg-hover font-semibold text-ink-2") : state === "done" ? "text-ink-2" : "text-ink-3",
              )}
            >
              {state === "done" && <Icon name="check" size={10} stroke={2.4} className="shrink-0" />}
              <span className="truncate">{p}</span>
            </span>
          </li>
        );
      })}
    </ol>
  );
}

function AgentRow({ a, first, onOpen }: { a: WorkflowAgent; first: boolean; onOpen: () => void }) {
  const label = a.label || t("lib-workflows.agent");
  return (
    <li className={cx(!first && "border-t border-line")}>
      <button type="button" onClick={onOpen} aria-label={t("lib-workflows.open_agent", { label })} className="flex w-full items-start gap-2 px-3.5 py-2 text-left transition-colors hover:bg-hover">
        <span className="mt-[5px] flex size-3 shrink-0 items-center justify-center">
          <StatusDot state={a.running ? "working" : "idle"} size={6} />
        </span>
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="flex min-w-0 items-center gap-1.5">
            <span className="truncate text-[12.5px] font-medium text-ink">{label}</span>
            {a.phase && <span className="shrink-0 text-[11px] text-ink-3">{a.phase}</span>}
          </span>
          {a.last_text && <span className="truncate text-[12px] text-ink-2">{a.last_text}</span>}
        </span>
        <Icon name="chevronRight" size={12} stroke={2.2} className="mt-[3px] shrink-0 text-ink-3" />
      </button>
    </li>
  );
}
