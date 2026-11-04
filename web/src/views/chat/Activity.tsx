// What keeps an agent going, beside the chat's title (iOS ActivityChips / ActivitySheet / AgentStrip / SubagentFooter):
// a /loop (its next wakeup, a way to stop it) and the background tasks (their output), and the subagents it launched
// (read one's conversation in the same chat, read-only; all of them, finished and nested, by who launched whom).
import { useState } from "react";
import { ago } from "../../lib/format";
import { openPanel } from "../../lib/panel";
import { backgroundTitle, elapsed, loopCadence, loopLabel, loopNext, loopShortLabel, loopTask, subagentTitle, subagentTree, type SubagentRow } from "../../lib/tasks";
import type { BackgroundTask, LoopInfo, Session, Subagent } from "../../lib/types";
import { Button, copyText, cx, Dialog, StatusDot, useNow } from "../../ui/atoms";
import { Icon } from "../../ui/icons";

/** "⟳ 11:57" and "后台 2" in the chat's header; a click opens them. */
export function ActivityChips({ session, onStopLoop }: { session: Session; onStopLoop: () => void }) {
  const [open, setOpen] = useState(false);
  const loop = session.loop;
  const bg = session.background ?? [];
  const now = useNow(30_000, Boolean(loop));
  if (!loop && !bg.length) return null;
  const label = [loop ? `循环中，${loopLabel(loop, now)}` : "", bg.length ? `${bg.length} 个后台任务` : ""].filter(Boolean).join("，");
  return (
    <>
      <button type="button" onClick={() => setOpen(true)} aria-label={`${label}，点按查看`} title={label} className="flex h-7 shrink-0 items-center gap-1 rounded-[8px] px-1 transition-colors hover:bg-hover">
        {loop && (
          <span className="flex h-5.5 items-center gap-1 rounded-full bg-accent-tint px-2 text-[11.5px] font-medium whitespace-nowrap text-accent-ink">
            <Icon name="loop" size={12} stroke={2.2} />
            <span className="hidden sm:inline">{loopShortLabel(loop, now)}</span>
          </span>
        )}
        {bg.length > 0 && (
          <span className="flex h-5.5 items-center gap-1 rounded-full bg-orange-tint px-2 text-[11.5px] font-medium whitespace-nowrap text-orange">
            <Icon name="gear" size={12} stroke={2.2} />
            <span className="hidden sm:inline">后台</span> {bg.length}
          </span>
        )}
      </button>
      <Dialog open={open} onClose={() => setOpen(false)} title={loop && !bg.length ? "循环" : !loop ? "后台任务" : "循环与后台任务"} width={520}>
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
              onOutput={(t) => {
                setOpen(false);
                openPanel({ kind: "file", mid: session.machine_id, path: t.output_file!, cwd: session.cwd });
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
          <span className="truncate text-[13px] font-semibold text-ink">循环中{cadence ? ` · ${cadence}` : ""}</span>
          {next && <span className="truncate text-[12px] text-ink-2">{next}</span>}
        </div>
        {!confirming && (
          <button
            type="button"
            onClick={() => setConfirming(true)}
            className="inline-flex h-8 shrink-0 items-center rounded-control bg-surface px-3 text-[12.5px] font-medium whitespace-nowrap text-red shadow-btn transition-colors hover:bg-hover"
          >
            停止循环
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
          <p className="text-[12.5px] leading-relaxed text-ink-2">会给 Agent 发一条消息，让它不再安排下一次唤醒。正在执行的这一轮会先做完。</p>
          <div className="flex justify-end gap-2">
            <Button tone="quiet" onClick={() => setConfirming(false)}>
              取消
            </Button>
            <Button tone="danger" onClick={onStop}>
              停止循环
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
        后台任务 {tasks.length}
      </div>
      {tasks.map((t, i) => {
        const cmd = t.command && t.command !== backgroundTitle(t) ? t.command : "";
        return (
          <div key={t.id} className={cx("flex items-start gap-2.5 px-3.5 py-2.5", i > 0 && "border-t border-line")}>
            <span className="mt-[3px] flex size-4 shrink-0 items-center justify-center text-orange">
              <Icon name={t.kind === "agent" ? "agent" : "run"} size={13} />
            </span>
            <div className="flex min-w-0 flex-1 flex-col gap-0.5">
              <span className="truncate text-[13px] font-medium text-ink">{backgroundTitle(t)}</span>
              {cmd && (
                <button type="button" onClick={() => copyText(cmd)} title="拷贝命令" className="line-clamp-2 text-left font-mono text-[11.5px] text-ink-3 hover:text-ink-2">
                  {cmd}
                </button>
              )}
            </div>
            <span className="shrink-0 text-[12px] whitespace-nowrap text-ink-3 tabular-nums">{elapsed(t.started_at, now)}</span>
            {t.output_file && (
              <button type="button" onClick={() => onOutput(t)} className="flex shrink-0 items-center gap-0.5 text-[12px] font-medium whitespace-nowrap text-accent-ink hover:underline">
                看输出
                <Icon name="chevronRight" size={12} stroke={2.2} />
              </button>
            )}
          </div>
        );
      })}
    </div>
  );
}

/** Under the header, while the session has subagents: 主 Agent and the running ones (plus the one being read) as chips,
 *  and at the end, when some are not chips, 「全部子 Agent N」: every subagent by who launched whom. */
export function AgentStrip({ agents, all, viewing, onSelect }: { agents: Subagent[]; all: Subagent[]; viewing?: string; onSelect: (a: Subagent | null) => void }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="flex shrink-0 gap-1.5 overflow-x-auto border-b border-line bg-page/85 px-3 py-2 scrollbar-none sm:px-4">
      <div className="flex shrink-0 gap-1.5" role="tablist" aria-label="子 Agent">
        <Chip on={!viewing} title="主 Agent" onClick={() => onSelect(null)} />
        {agents.map((a) => (
          <Chip key={a.id} on={viewing === a.id} title={subagentTitle(a)} running={a.running} done={!a.running} onClick={() => onSelect(a)} />
        ))}
      </div>
      {all.length > agents.length && (
        <button
          type="button"
          aria-haspopup="dialog"
          onClick={() => setOpen(true)}
          className="ml-auto flex h-7 shrink-0 items-center gap-1.5 rounded-full px-2.5 text-[12px] font-medium whitespace-nowrap text-ink-2 transition-colors hover:bg-hover-2"
        >
          <Icon name="list" size={12} />
          全部子 Agent
          <span className="text-ink-3 tabular-nums">{all.length}</span>
        </button>
      )}
      <Dialog open={open} onClose={() => setOpen(false)} title="全部子 Agent" width={520}>
        <SubagentTree
          rows={subagentTree(all)}
          viewing={viewing}
          onSelect={(a) => {
            setOpen(false);
            onSelect(a);
          }}
        />
      </Dialog>
    </div>
  );
}

// One step deeper per level; past the last, deeper ones line up with it.
const INDENT = ["pl-3.5", "pl-8.5", "pl-13.5", "pl-18.5", "pl-23.5"];

/** Every subagent under the one that launched it: how it is going, what it is, when it started; a click reads it. */
function SubagentTree({ rows, viewing, onSelect }: { rows: SubagentRow[]; viewing?: string; onSelect: (a: Subagent) => void }) {
  const now = useNow(30_000);
  return (
    <div className="flex flex-col overflow-hidden rounded-card bg-surface shadow-hairline">
      {rows.map(({ agent: a, depth }, i) => (
        <button
          key={a.id}
          type="button"
          onClick={() => onSelect(a)}
          aria-current={viewing === a.id || undefined}
          title={a.description || undefined}
          className={cx(
            "flex w-full items-start gap-2 py-2.5 pr-3.5 text-left transition-colors hover:bg-hover",
            INDENT[Math.min(depth, INDENT.length - 1)],
            i > 0 && "border-t border-line",
            viewing === a.id && "bg-accent-tint/50",
          )}
        >
          {depth > 0 && <span aria-hidden className="mt-[3px] h-2 w-2.5 shrink-0 rounded-bl-[4px] border-b border-l border-line-strong" />}
          <span className="mt-[5px] flex size-2 shrink-0 items-center justify-center">
            <StatusDot state={a.running ? "working" : "unread"} size={6} />
          </span>
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <span className="truncate text-[13px] font-medium text-ink">{subagentTitle(a)}</span>
            <span className="flex min-w-0 items-center gap-1.5 text-[12px] text-ink-3">
              <span className={cx("shrink-0", a.running && "text-orange")}>{a.running ? "运行中" : "已结束"}</span>
              {a.fork ? (
                <span className="flex shrink-0 items-center gap-0.5 rounded-[4px] bg-field px-1 text-[10.5px] font-medium text-ink-2">
                  <Icon name="git" size={10} />
                  分叉
                </span>
              ) : (
                a.type && <span className="truncate">· {a.type}</span>
              )}
            </span>
          </div>
          <span className="shrink-0 text-[12px] whitespace-nowrap text-ink-3 tabular-nums">{ago(a.started_at || a.updated_at, now)}</span>
        </button>
      ))}
    </div>
  );
}

function Chip({ on, title, running, done, onClick }: { on: boolean; title: string; running?: boolean; done?: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={on}
      onClick={onClick}
      title={title}
      aria-label={title + (running ? "，运行中" : done ? "，已完成" : "")}
      className={cx(
        "flex h-7 shrink-0 items-center gap-1.5 rounded-full px-2.5 text-[12px] whitespace-nowrap transition-colors",
        on ? "bg-accent-tint font-semibold text-accent-ink shadow-[inset_0_0_0_1px_var(--accent)]" : "bg-inset font-medium text-ink-2 shadow-hairline hover:bg-hover-2",
      )}
    >
      {running ? <StatusDot state="working" size={6} /> : done ? <StatusDot state="unread" size={6} /> : <Icon name="agent" size={12} />}
      <span className="max-w-40 truncate">{title}</span>
    </button>
  );
}

/** In place of the composer while a subagent is shown: what it is and the way back. */
export function SubagentFooter({ agent, onBack }: { agent: Subagent; onBack: () => void }) {
  return (
    <div className="flex items-center gap-3 rounded-[14px] bg-surface px-3.5 py-2.5 shadow-card">
      <Icon name="agent" size={16} className="shrink-0 text-accent" />
      <div className="flex min-w-0 flex-1 flex-col">
        <span className="truncate text-[12.5px] font-semibold text-ink">子 Agent · {agent.type || "Agent"}</span>
        <span className="truncate text-[11.5px] text-ink-3">{agent.running ? "运行中，只读" : "已完成，只读"}</span>
      </div>
      <Button icon="chevronLeft" onClick={onBack}>
        回到主 Agent
      </Button>
    </div>
  );
}
