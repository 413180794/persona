// An exchange's work as lib/work-groups.ts groups it (cindy's WorkGroupBlock): 「正在工作…」 with a clock and its
// newest steps peeking out while it runs, 「已工作 Xs」 once done; open, every step on a 2 px rail. A finished
// exchange's outer group is a timeline: the agent's words between inner groups that stay folded.
import { memo, useEffect, useMemo, useState } from "react";
import type { Item, SessionLive } from "../../lib/types";
import { groupOpens, groupTitle, isAgentStep, isPlanStep, isRowStep, planTodos, previewSteps, workDuration, type AgentStatus, type ToolStep, type WorkGroupNode, type WorkNode } from "../../lib/work-groups";
import type { Step } from "../../lib/turns";
import { Collapse, cx, Spinner, useNow } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { ActionRow } from "./ActionRow";
import { inOpenedFold, openFoldRange, useExpanded } from "./expanded";
import { PlanCard } from "./PlanCard";
import { Answer } from "./SegmentView";
import { NoticeRow, Thumb } from "./Steps";
import { SubagentCard } from "./SubagentCard";
import { ThinkingCard, ThinkingLine } from "./ThinkingCard";

export interface WorkProps {
  mid: string;
  sid: string;
  /** Keys the reader's open / closed state (expanded.ts) to this chat. */
  scope: string;
  expandFold: (f: Item) => Promise<void>;
  agentStatus: (s: ToolStep) => AgentStatus;
  thinking: Map<string, number>;
  /** The screen's status line while this exchange is live (contract §3): a thinking card at the live group's end. */
  status?: SessionLive | null;
  /** A merged exchange: opening its work splits it into its messages instead. */
  onOpen?: () => void;
  /** Inside an open group (a finished timeline's inner groups stay folded). */
  nested?: boolean;
}

/** Every fold under a group, nested ones too: opening it fetches their steps. */
function foldsOf(g: WorkGroupNode): Item[] {
  return g.children.flatMap((c) => (c.kind === "group" ? foldsOf(c) : c.step.kind === "fold" ? [c.step.item] : []));
}

export const WorkGroup = memo(function WorkGroup({ g, p }: { g: WorkGroupNode; p: WorkProps }) {
  // an outer group whose folded steps the reader just opened: open under whatever key it has once they are in
  const [remembered, setOpen] = useExpanded(`${p.scope}${g.id}`, !p.nested && inOpenedFold(p.scope, g.seq));
  const [loading, setLoading] = useState(false);
  const opens = groupOpens(g);
  const open = remembered && opens;
  const preview = g.live && !open ? previewSteps(g) : [];
  const inside = useMemo(() => ({ ...p, onOpen: undefined, nested: true }), [p]);
  const toggle = () => {
    if (p.onOpen) return p.onOpen();
    const next = !open;
    setOpen(next);
    if (next) for (const f of foldsOf(g)) if (f.fold) openFoldRange(p.scope, f.seq, f.fold.through_seq);
  };
  // an open group's folded steps are fetched: opened just now, or remembered open when the chat comes back (#1996: it
  // stayed at 正在取 N 条 then); once in, the folds are gone and this has nothing left to do. The outer group fetches
  // its nested groups' folds too.
  const folds = useMemo(() => (open && !p.nested ? foldsOf(g) : []), [open, p.nested, g]);
  const foldKey = folds.map((f) => f.id).join(" ");
  const expandFold = p.expandFold;
  // a fetch that failed (the hub restarting, the network): the group says so, and 重试 runs it again
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!foldKey) return;
    let live = true;
    setLoading(true);
    setFailed(false);
    folds
      // each fold that comes in runs this again with the rest: this run stops there
      .reduce((done, f) => done.then(() => (live ? expandFold(f) : undefined)), Promise.resolve())
      .catch(() => live && setFailed(true))
      .finally(() => live && setLoading(false));
    return () => {
      live = false;
    };
    // the folds by their ids: a new `g` with the same folds is not a new fetch
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [foldKey, expandFold, attempt]);
  const thinkingNow = g.live && p.status?.thinking ? <ThinkingCard live={p.status} /> : null;
  return (
    <div className="flex w-full min-w-0 flex-col" data-work-group={g.live ? "live" : "done"} data-group-id={g.id}>
      <button
        type="button"
        onClick={opens ? toggle : undefined}
        disabled={!opens}
        aria-expanded={opens ? open || preview.length > 0 : undefined}
        // on a touch screen 40 tall, the margin taking back what it adds, so nothing around it moves
        className={cx("flex w-full min-w-0 items-center gap-1.5 py-[2px] text-left select-none [@media(pointer:coarse)]:-my-[7.5px] [@media(pointer:coarse)]:min-h-10", opens ? "cursor-pointer transition-opacity hover:opacity-80" : "cursor-default")}
      >
        <span className="inline-flex h-[1lh] shrink-0 items-center text-tool-ink">{g.live ? <Spinner size={14} /> : <Icon name="layers" size={14} />}</span>
        <span className="min-w-0 translate-y-[1px] truncate text-[length:var(--chat-secondary)] text-tool-ink">{groupTitle(g)}</span>
        <span className="flex-1" />
        {loading && <Spinner size={11} />}
        {g.live && g.startedAt !== undefined && <Elapsed since={g.startedAt} />}
        {opens && <Icon name="chevronRight" size={14} stroke={2.2} className="shrink-0 text-tool-ink transition-transform duration-150 motion-reduce:transition-none" style={{ transform: open ? "rotate(90deg)" : "none" }} />}
      </button>
      {open && failed && !loading && (
        <div className="mt-1 flex items-center gap-2 pl-[22px] text-[12px] text-ink-3" role="status">
          没能取回这段过程
          <button type="button" onClick={() => setAttempt((n) => n + 1)} className="touch-hit rounded-control px-1.5 py-0.5 font-medium whitespace-nowrap text-accent-ink hover:bg-hover">重试</button>
        </div>
      )}
      {(preview.length > 0 || (thinkingNow && !open)) && (
        <div className="mt-1 flex min-w-0 flex-col border-l-2 border-tool-rail py-[2px] pl-3" data-live-preview>
          {preview.map((s) => (s.kind === "tool" ? <ActionRow key={s.id} step={s} running={!s.result} mid={p.mid} sid={p.sid} /> : <ThinkingLine key={s.id} item={s.item} />))}
          {thinkingNow}
        </div>
      )}
      <Collapse open={open} lazy>
        <div className="mt-1 flex min-w-0 flex-col gap-2 border-l-2 border-tool-rail py-[2px] pl-3" data-work-detail>
          <WorkNodes nodes={g.children} p={inside} live={g.live} />
          {thinkingNow}
        </div>
      </Collapse>
    </div>
  );
});

/** Nodes in order; tool calls next to each other sit together without a gap, like cindy's tool segments. */
export function WorkNodes({ nodes, p, live, fresh }: { nodes: WorkNode[]; p: WorkProps; live?: boolean; fresh?: ReadonlySet<string> }) {
  const runs: (WorkNode | ToolStep[])[] = [];
  for (const n of nodes) {
    const last = runs[runs.length - 1];
    if (n.kind === "step" && isRowStep(n.step)) {
      if (Array.isArray(last)) last.push(n.step);
      else runs.push([n.step]);
    } else runs.push(n);
  }
  return (
    <>
      {runs.map((run) =>
        Array.isArray(run) ? (
          <div key={run[0].id} className="flex min-w-0 flex-col">
            {run.map((s) => (
              <ActionRow key={s.id} step={s} running={live && !s.result} mid={p.mid} sid={p.sid} />
            ))}
          </div>
        ) : run.kind === "group" ? (
          <WorkGroup key={run.id} g={run} p={p} />
        ) : (
          <StepView key={run.step.id} step={run.step} p={p} live={live} fresh={fresh} />
        ),
      )}
    </>
  );
}

/** A step on its own: in the open, or inside an open group. */
export function StepView({ step, p, live, fresh }: { step: Step; p: WorkProps; live?: boolean; fresh?: ReadonlySet<string> }) {
  switch (step.kind) {
    case "tool":
      if (isAgentStep(step)) return <SubagentCard step={step} status={p.agentStatus(step)} mid={p.mid} scope={p.scope} />;
      if (isPlanStep(step)) return <PlanCard id={step.id} todos={planTodos(step)} live={live} />;
      return <ActionRow step={step} running={live && !step.result} mid={p.mid} sid={p.sid} />;
    case "thinking":
      return <ThinkingCard item={step.item} durationMs={p.thinking.get(step.id)} scope={p.scope} />;
    case "text":
      return <Answer item={step.item} mid={p.mid} sid={p.sid} fade={fresh?.has(step.id)} />;
    case "notice":
      return <NoticeRow item={step.item} mid={p.mid} sid={p.sid} />;
    case "image":
      return (
        <div className="py-1">
          <Thumb item={step.item} mid={p.mid} sid={p.sid} />
        </div>
      );
    case "fold":
      return (
        <div className="flex h-7 items-center gap-2 px-2 text-[13px] text-tool-ink">
          <Spinner size={11} /> 正在取 {step.item.fold?.items ?? ""} 条
        </div>
      );
  }
}

/** A live group's clock, every 500 ms; its own leaf, so the ticking redraws nothing else. */
function Elapsed({ since }: { since: number }) {
  const now = useNow(500);
  return <span className="shrink-0 font-mono text-[12px] text-tool-ink tabular-nums">{workDuration(Math.max(0, now - since))}</span>;
}
