// A subagent the agent launched, in the chat where it launched it (cindy's AgentTaskCard): what it is and how it is
// going; open, what it was asked, what it reported, its latest steps and the subagents it launched in turn. Its whole
// conversation opens read-only in the side panel, the chat staying where it is.
import { memo, useEffect, useState } from "react";
import { api, sessionPath } from "../../lib/api";
import { openPanel } from "../../lib/panel";
import { agentSummary, nestedSubagents, subagentFor, subagentTitle } from "../../lib/tasks";
import { segments } from "../../lib/turns";
import type { Item, ItemsPage, Subagent } from "../../lib/types";
import { tsMs, workDuration, type AgentStatus, type ToolStep } from "../../lib/work-groups";
import { Collapse, cx, Spinner, StatusDot, useNow } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { ActionRow } from "./ActionRow";
import { useChatContext } from "./chat-context";
import { useExpanded } from "./expanded";
import { t } from "../../i18n";
import { ScrollFold } from "./fold-scroll";

const STATUS: Record<AgentStatus, string> = {
  running: t("chat-subagentcard.status_running"),
  completed: t("chat-subagentcard.status_completed"),
  failed: t("chat-subagentcard.status_failed"),
  stopped: t("chat-subagentcard.status_stopped"),
};
/** How many of its latest steps the open card shows. */
const RECENT_STEPS = 3;

const ms = (ts?: string) => tsMs(ts) ?? undefined;

/** Opens a subagent's conversation in the side panel (SidePanel reads an "agent" panel on a `<parent>@<agent>` id as one). */
function openSubagent(mid: string, a: Subagent) {
  openPanel({ kind: "agent", mid, sid: a.session_id });
}

export const SubagentCard = memo(function SubagentCard({ step, status, mid, scope = "" }: { step: ToolStep; status: AgentStatus; mid: string; scope?: string }) {
  const { subagents } = useChatContext();
  const { use, result } = step;
  const inp = (use.tool_input ?? {}) as Record<string, unknown>;
  const meta = subagentFor(subagents, use);
  const [open, setOpen] = useExpanded(`${scope}task:${use.id}`);
  const running = status === "running";
  const now = useNow(1000, running);
  const title = (typeof inp.description === "string" && inp.description.trim()) || (meta ? subagentTitle(meta) : "") || t("chat-subagentcard.untitled_task");
  const prompt = typeof inp.prompt === "string" ? inp.prompt.trim() : "";
  const summary = agentSummary(result, meta);
  const type = (typeof inp.subagent_type === "string" && inp.subagent_type) || meta?.type || "";
  const start = ms(meta?.started_at) ?? ms(use.ts);
  const end = running ? now : (ms(result?.ts) ?? ms(meta?.updated_at));
  const took = start !== undefined && end !== undefined && end >= start ? workDuration(end - start) : "";
  const nested = meta ? nestedSubagents(subagents, meta.id) : [];
  return (
    <div className="flex w-full" data-subagent-card={status} data-tool-use={use.tool_use_id}>
      <div className="w-full min-w-0 rounded-xl border border-line bg-surface px-3 py-2">
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          aria-expanded={open}
          aria-label={open ? t("chat-subagentcard.collapse_details") : t("chat-subagentcard.expand_details")}
          className="flex w-full min-w-0 items-start gap-2 rounded-lg text-left [@media(pointer:coarse)]:min-h-10"
        >
          <span className="mt-0.5 inline-flex size-5 shrink-0 items-center justify-center rounded-full bg-field text-ink-2">
            <Icon name="agent" size={14} />
          </span>
          <span className="min-w-0 flex-1">
            <span className="flex min-w-0 items-center gap-2">
              <span className="shrink-0 text-[11px] whitespace-nowrap text-ink-2">{t("chat-subagentcard.subagent")}</span>
              <span className="min-w-0 flex-1 truncate text-[length:var(--chat-secondary)] leading-[1.428571] font-medium text-ink">{title}</span>
              <span className={cx("inline-flex shrink-0 items-center gap-1.5 text-[12px] leading-[1.333333] whitespace-nowrap", status === "failed" ? "text-red" : "text-ink-2")}>
                {running ? <Spinner size={11} /> : <Icon name={status === "failed" ? "info" : status === "stopped" ? "stop" : "check"} size={12} />}
                {STATUS[status]}
              </span>
            </span>
            <span className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[12px] leading-[1.333333] text-ink-3">
              <span>Claude Code</span>
              {type && <span className="inline-flex items-center rounded-full bg-field px-1.5 py-0.5">{meta?.fork ? t("chat-subagentcard.fork") : type}</span>}
              {took && <span className="tabular-nums">{running ? t("chat-subagentcard.running_for", { duration: took }) : took}</span>}
              {meta?.background && <span>{t("chat-subagentcard.background")}</span>}
            </span>
          </span>
          <Icon name="chevronRight" size={14} stroke={2.2} className="mt-1 shrink-0 text-ink-3 transition-transform duration-150 motion-reduce:transition-none" style={{ transform: open ? "rotate(90deg)" : "none" }} />
        </button>
        <Collapse open={open} lazy>
          <div className="mt-2 flex flex-col gap-1.5 border-l-2 border-tool-rail pl-3 text-[13px] leading-5 text-ink-2">
            {prompt && <ScrollFold className="max-h-[6lh] whitespace-pre-wrap" data-subagent-prompt>{prompt}</ScrollFold>}
            {summary && <p className="whitespace-pre-wrap text-ink" data-subagent-summary>{summary}</p>}
            {meta && open && <RecentSteps mid={mid} agent={meta} />}
            {nested.length > 0 && (
              <div className="flex flex-col" data-subagent-nested>
                {nested.map(({ agent: a, depth }) => (
                  <button
                    key={a.id}
                    type="button"
                    onClick={() => openSubagent(mid, a)}
                    className="flex min-w-0 items-center gap-2 rounded-[8px] py-1 pr-2 text-left transition-colors hover:bg-tool-hover"
                    style={{ paddingLeft: 8 + depth * 16 }}
                  >
                    {depth > 0 && <span aria-hidden className="h-2 w-2.5 shrink-0 rounded-bl-[4px] border-b border-l border-line-strong" />}
                    <StatusDot state={a.running ? "working" : "unread"} size={6} />
                    <span className="min-w-0 flex-1 truncate text-[13px] text-ink">{subagentTitle(a)}</span>
                    <span className="shrink-0 text-[12px] whitespace-nowrap text-ink-3">{a.running ? t("chat-subagentcard.status_running") : t("chat-subagentcard.ended")}</span>
                  </button>
                ))}
              </div>
            )}
            {meta && (
              <button
                type="button"
                onClick={() => openSubagent(mid, meta)}
                className="mt-0.5 flex w-fit items-center gap-1 rounded-[6px] py-0.5 text-[12px] font-medium whitespace-nowrap text-accent-ink hover:underline"
                data-subagent-open
              >
                {t("chat-subagentcard.view_full")}
                <Icon name="chevronRight" size={12} stroke={2.2} />
              </button>
            )}
          </div>
        </Collapse>
      </div>
    </div>
  );
});

/** The subagent's latest few tool calls, read from its own transcript when the card opens. */
function RecentSteps({ mid, agent }: { mid: string; agent: Subagent }) {
  const [steps, setSteps] = useState<ToolStep[] | null>(null);
  useEffect(() => {
    let dead = false;
    api<ItemsPage>(`${sessionPath(mid, agent.session_id)}/items?turns=1&lazy_images=1`)
      .then((p) => !dead && setSteps(lastTools(p.items ?? [], RECENT_STEPS)))
      .catch(() => !dead && setSteps([]));
    return () => {
      dead = true;
    };
    // the latest steps again when it says something new
  }, [mid, agent.session_id, agent.last_text, agent.running]);
  if (!steps) return <Spinner size={11} />;
  if (!steps.length) return null;
  return (
    <div className="-ml-2 flex flex-col" data-subagent-recent>
      {steps.map((s) => (
        <ActionRow key={s.id} step={s} running={!s.result && agent.running} mid={mid} sid={agent.session_id} />
      ))}
    </div>
  );
}

function lastTools(items: Item[], n: number): ToolStep[] {
  return segments(items)
    .flatMap((s) => s.steps)
    .filter((s): s is ToolStep => s.kind === "tool")
    .slice(-n);
}
