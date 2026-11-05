// What keeps an agent going, beside the chat's title (iOS ActivityChips / ActivitySheet): a /loop (its next wakeup, a
// way to stop it) and the background tasks (their output). Subagents are cards in the chat where they were launched
// (SubagentCard.tsx).
import { useState } from "react";
import { openPanel } from "../../lib/panel";
import { t } from "../../i18n";
import { goalHeadline, goalStatusLabel, goalTone, goalUsage } from "../../lib/goal";
import { backgroundTitle, elapsed, loopCadence, loopLabel, loopNext, loopShortLabel, loopTask } from "../../lib/tasks";
import type { BackgroundTask, GoalInfo, LoopInfo, Session } from "../../lib/types";
import { Button, copyText, cx, Dialog, useNow } from "../../ui/atoms";
import { Icon } from "../../ui/icons";

/** "⟳ 11:57" and "后台 2" in the chat's header (on a phone one icon: ⟳, or ⚙ 2); a click opens them. */
export function ActivityChips({ session, onStopLoop }: { session: Session; onStopLoop: () => void }) {
  const [open, setOpen] = useState(false);
  const loop = session.loop;
  const bg = session.background ?? [];
  const now = useNow(30_000, Boolean(loop));
  if (!loop && !bg.length) return null;
  const label = loop && bg.length
    ? t("chat-activity.label_loop_and_background", { loop: loopLabel(loop, now), count: bg.length })
    : loop
      ? t("chat-activity.label_loop", { loop: loopLabel(loop, now) })
      : t("chat-activity.label_background", { count: bg.length });
  return (
    <>
      <button type="button" onClick={() => setOpen(true)} aria-label={t("chat-activity.open_label", { label })} title={label} className="touch-hit flex h-7 shrink-0 items-center gap-1 rounded-[8px] px-1 transition-colors hover:bg-hover">
        {loop && (
          <span className="flex h-5.5 items-center gap-1 rounded-full bg-accent-tint px-2 text-[11.5px] font-medium whitespace-nowrap text-accent-ink">
            <Icon name="loop" size={12} stroke={2.2} />
            <span className="hidden sm:inline">{loopShortLabel(loop, now)}</span>
          </span>
        )}
        {bg.length > 0 && (
          <span className={cx("flex h-5.5 items-center gap-1 rounded-full bg-orange-tint px-2 text-[11.5px] font-medium whitespace-nowrap text-orange", loop && "max-sm:hidden")}>
            <Icon name="gear" size={12} stroke={2.2} />
            <span className="hidden sm:inline">{t("chat-activity.background_chip")}</span> {bg.length}
          </span>
        )}
      </button>
      <Dialog open={open} onClose={() => setOpen(false)} title={loop && !bg.length ? t("chat-activity.title_loop") : !loop ? t("chat-activity.title_background") : t("chat-activity.title_both")} width={520}>
        <div className="flex flex-col gap-3">
          {loop && (
            <LoopCard
              loop={loop}
              onStop={() => {
                setOpen(false);
                onStopLoop();
              }}
            />
          )}
          {bg.length > 0 && (
            <BackgroundList
              tasks={bg}
              onOutput={(task) => {
                setOpen(false);
                openPanel({ kind: "file", mid: session.machine_id, path: task.output_file!, cwd: session.cwd });
              }}
            />
          )}
        </div>
      </Dialog>
    </>
  );
}

/** The /loop driving this agent: what it repeats, how often, when it wakes next — and a way to stop it. */
function LoopCard({ loop, onStop }: { loop: LoopInfo; onStop: () => void }) {
  const now = useNow(30_000);
  const [confirming, setConfirming] = useState(false);
  const cadence = loopCadence(loop);
  const next = loopNext(loop, now);
  return (
    <div className="flex flex-col gap-2 rounded-card bg-accent-tint/50 px-3.5 py-3 shadow-hairline">
      <div className="flex items-center gap-2.5">
        <Icon name="loop" size={15} stroke={2.2} className="shrink-0 text-accent" />
        <div className="flex min-w-0 flex-1 flex-col">
          <span className="truncate text-[13px] font-semibold text-ink">{cadence ? t("chat-activity.looping_every", { cadence }) : t("chat-activity.looping")}</span>
          {next && <span className="truncate text-[12px] text-ink-2">{next}</span>}
        </div>
        {!confirming && (
          <button
            type="button"
            onClick={() => setConfirming(true)}
            className="inline-flex h-8 shrink-0 items-center rounded-control bg-surface px-3 text-[12.5px] font-medium whitespace-nowrap text-red shadow-btn transition-colors hover:bg-hover"
          >
            {t("chat-activity.stop_loop")}
          </button>
        )}
      </div>
      {loopTask(loop) && <p className="text-[12.5px] leading-relaxed whitespace-pre-wrap text-ink-2">{loopTask(loop)}</p>}
      {loop.reason && (
        <span className="flex items-start gap-1.5 text-[12px] text-ink-3">
          <Icon name="chat" size={12} className="mt-[2px] shrink-0" />
          {loop.reason}
        </span>
      )}
      {confirming && (
        <div className="flex flex-col gap-2 rounded-[8px] bg-surface px-3 py-2.5 shadow-hairline">
          <p className="text-[12.5px] leading-relaxed text-ink-2">{t("chat-activity.stop_loop_explain")}</p>
          <div className="flex justify-end gap-2">
            <Button tone="quiet" onClick={() => setConfirming(false)}>
              {t("common.cancel")}
            </Button>
            <Button tone="danger" onClick={onStop}>
              {t("chat-activity.stop_loop")}
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}

/** Every background task, how long it has run, and its output (in the side panel). */
function BackgroundList({ tasks, onOutput }: { tasks: BackgroundTask[]; onOutput: (t: BackgroundTask) => void }) {
  const now = useNow(1000);
  return (
    <div className="flex flex-col overflow-hidden rounded-card bg-surface shadow-hairline">
      <div className="flex items-center gap-1.5 border-b border-line bg-inset px-3.5 py-2 text-[12px] font-medium text-ink-2">
        <Icon name="gear" size={12} className="text-orange" />
        {t("chat-activity.background_count", { count: tasks.length })}
      </div>
      {tasks.map((task, i) => {
        const cmd = task.command && task.command !== backgroundTitle(task) ? task.command : "";
        return (
          <div key={task.id} className={cx("flex items-start gap-2.5 px-3.5 py-2.5", i > 0 && "border-t border-line")}>
            <span className="mt-[3px] flex size-4 shrink-0 items-center justify-center text-orange">
              <Icon name={task.kind === "agent" ? "agent" : "run"} size={13} />
            </span>
            <div className="flex min-w-0 flex-1 flex-col gap-0.5">
              <span className="truncate text-[13px] font-medium text-ink">{backgroundTitle(task)}</span>
              {cmd && (
                <button type="button" onClick={() => copyText(cmd)} title={t("chat-activity.copy_command")} className="line-clamp-2 text-left font-mono text-[11.5px] text-ink-3 hover:text-ink-2">
                  {cmd}
                </button>
              )}
            </div>
            <span className="shrink-0 text-[12px] whitespace-nowrap text-ink-3 tabular-nums">{elapsed(task.started_at, now)}</span>
            {task.output_file && (
              <button type="button" onClick={() => onOutput(task)} className="flex shrink-0 items-center gap-0.5 text-[12px] font-medium whitespace-nowrap text-accent-ink hover:underline">
                {t("chat-activity.view_output")}
                <Icon name="chevronRight" size={12} stroke={2.2} />
              </button>
            )}
          </div>
        );
      })}
    </div>
  );
}

const goalChipTone = {
  active: "bg-accent-tint text-accent-ink",
  done: "bg-green-tint text-green",
  stopped: "bg-orange-tint text-orange",
} as const;
const goalTextTone = { active: "text-accent-ink", done: "text-green", stopped: "text-orange" } as const;

/** The goal it works toward (docs/agent-goal.md, iOS GoalBar / GoalSheet): ◎ and its status in the chat's header (the
 *  first line too where there is room); a click shows all of it, the latest check's reason and what it has spent. */
export function GoalChip({ goal }: { goal: GoalInfo }) {
  const [open, setOpen] = useState(false);
  const status = goalStatusLabel(goal);
  const headline = goalHeadline(goal);
  const usage = goalUsage(goal);
  const tone = goalChipTone[goalTone(goal)];
  const textTone = goalTextTone[goalTone(goal)];
  return (
    <>
      <button type="button" onClick={() => setOpen(true)} aria-label={t("agent-goal.chip_label", { status, headline })} title={t("agent-goal.chip_title", { status, headline })}
        className="touch-hit flex h-7 min-w-0 shrink items-center rounded-[8px] px-1 transition-colors hover:bg-hover">
        <span className={cx("flex h-5.5 min-w-0 items-center gap-1 rounded-full px-2 text-[11.5px] font-medium whitespace-nowrap", tone)}>
          <Icon name="target" size={12} stroke={2.2} className="shrink-0" />
          <span className="hidden max-w-[220px] truncate lg:inline">{headline}</span>
          <span className="shrink-0 max-sm:hidden lg:before:content-['·_']">{status}</span>
        </span>
      </button>
      <Dialog open={open} onClose={() => setOpen(false)} title={t("agent-goal.title")} width={520}>
        <div className="flex flex-col gap-3">
          <div className={cx("flex items-center gap-1.5 text-[12.5px] font-semibold", textTone)}>
            <Icon name="target" size={14} stroke={2.2} className="shrink-0" />
            <span className="whitespace-nowrap">{status}</span>
            {usage && <span className="truncate font-normal text-ink-3">{usage}</span>}
          </div>
          <p className="text-[13.5px] leading-relaxed whitespace-pre-wrap text-ink select-text">{goal.objective}</p>
          {goal.reason && (
            <div className="flex flex-col gap-1 rounded-card bg-stripe-bg px-3.5 py-3 shadow-hairline">
              <span className="text-[11.5px] font-semibold text-ink-3">{t(goal.status === "complete" ? "agent-goal.reason_met" : "agent-goal.reason_check")}</span>
              <p className="text-[12.5px] leading-relaxed whitespace-pre-wrap text-ink-2 select-text">{goal.reason}</p>
            </div>
          )}
          <p className="text-[12px] text-ink-3">{t("agent-goal.hint")}</p>
        </div>
      </Dialog>
    </>
  );
}
