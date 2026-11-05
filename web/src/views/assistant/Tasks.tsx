// The unified entry in the assistant's conversation (docs/unified-entry.md; iOS Views/Assistant/Tasks): the plan card,
// task cards that follow their agents, the hub's lines (answered, receipt, routed), the 今天 card, message cards about
// a task, quick results, the dock above the composer, the tasks' questions, the route chip, and a task's detail in the
// side panel.
import { useMemo, useState } from "react";
import {
  answeredItems,
  cardSent,
  dayLabel,
  dismissPlan,
  draftPlaceholder,
  field,
  getFeedState,
  receiptState,
  isToday,
  later,
  removeTask,
  retryKey,
  retryTask,
  sendMessage,
  startFailure,
  setRoute,
  startPlan,
  useFeed,
  type FeedItem,
  type PlanTask,
  type QuickResult,
  type TaskPlan,
  type TodayBody,
} from "../../lib/feed";
import { dockAgents, interruption, isRemovable, makeSessionLookup, mostUrgentAgent, runningSubagents, taskNow, todoStatus, TODO_STATUS_INFO, type SessionLookup, type Todo, type TodoStatus } from "../../lib/todos";
import { parseAPIError } from "../../lib/api-errors";
import { navigate } from "../../lib/router";
import { openPanel } from "../../lib/panel";
import { plainMarkdown } from "../../lib/plain-markdown";
import { Markdown } from "../../ui/Markdown";
import { LiveCard } from "../../ui/AssistantCard";
import type { AssistantView } from "../../lib/extension-wire";
import { getState, useStore } from "../../lib/store";
import type { AgentState, Interaction, Session } from "../../lib/types";
import { Button, Collapse, cx, Menu, StatusDot, toast, useNow } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";
import { AnsweredCard, InteractionCard } from "../chat/Interactions";
import { MessageActions } from "../chat/MessageActions";
import { clock, sessionTitle, stateOf } from "../../lib/format";
import { formatDate, t } from "../../i18n";

// ── what a task is now ──

function useTaskLookup() {
  const sessions = useStore((s) => s.sessions);
  return useMemo(() => makeSessionLookup(sessions), [sessions]);
}

export function useTodo(id: string | undefined): Todo | undefined {
  return useStore((s) => (id ? s.todos.find((t) => t.id === id) : undefined));
}

/** A task's status, failed too when a hub from before #1077 left it open and only its plan says the start failed. */
function statusOf(todo: Todo, lookup: SessionLookup, plans: Record<string, TaskPlan>): TodoStatus {
  const st = todoStatus(todo, lookup);
  return st === "not_started" && startFailure(todo, plans) ? "failed" : st;
}

/** Removes a task (lib/feed removeTask), saying so when it did not go. */
export function removeTaskNow(todo: Todo) {
  removeTask(todo).then(
    (gone) => gone && toast(t("assistant-tasks.removed", { title: todo.title }), "ok"),
    (e) => toast(t("assistant-tasks.remove_failed", { error: String(e instanceof Error ? e.message : e) }), "error"),
  );
}

/** Right-click on a task's card, line or pill: 移除任务 when nothing of it runs (iOS: the long press). */
function useTaskMenu(todo: Todo | undefined, status: TodoStatus | undefined) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const can = Boolean(todo && status && isRemovable(status));
  return {
    onContextMenu: (e: React.MouseEvent<HTMLElement>) => {
      if (!can) return;
      e.preventDefault();
      setAnchor(e.currentTarget);
    },
    menu:
      anchor && todo ? (
        <Menu
          anchor={anchor}
          onClose={() => setAnchor(null)}
          items={[{ key: "remove", label: t("assistant-tasks.remove_ellipsis"), icon: "trash", danger: true, onSelect: () => removeTaskNow(todo) }]}
        />
      ) : null,
  };
}

/** The task's state as an agent state, for its dot and tone. */
function dotState(st: TodoStatus): AgentState {
  return st === "needs_you" || st === "interrupted" ? "needs_you" : st === "working" || st === "not_started" ? "working" : st === "unread" ? "unread" : st === "follow_up" ? "idle" : "offline"; // done, failed
}

const TONE: Record<string, string> = {
  red: "text-red",
  green: "text-green",
  amber: "text-orange",
  idle: "text-ink-2",
  off: "text-ink-3",
};

function TaskMark({ status, size = 9 }: { status: TodoStatus; size?: number }) {
  if (status === "failed" || status === "interrupted") return <Icon name="info" size={size + 4} stroke={2.4} className="text-red" />;
  if (status === "done" || status === "unread")
    return <Icon name="check" size={size + 4} stroke={2.4} className={status === "done" ? "text-ink-3" : "text-green"} />;
  return <StatusDot state={dotState(status)} size={size} />;
}

function place(todo: Todo, lead?: Session): string {
  const a = (todo.agents ?? []).find((x) => !x.released_at) ?? todo.agents?.[0];
  const parts: string[] = [];
  const machineName = (id: string) => getState().machines.find((m) => m.id === id)?.name ?? id;
  if (a) {
    parts.push(machineName(a.machine_id));
    if (a.cwd) parts.push(a.cwd.split("/").filter(Boolean).at(-1) ?? a.cwd);
  } else {
    // No agent (yet, or ever): where the plan meant it to run.
    const planned = todo.plan_id ? getFeedState().plans[todo.plan_id]?.tasks.find((t) => t.todo_id === todo.id) : undefined;
    if (planned) {
      parts.push(machineName(planned.machine));
      parts.push(planned.cwd.split("/").filter(Boolean).at(-1) ?? planned.cwd);
    }
  }
  if (lead?.agent) parts.push(lead.agent);
  return parts.join(" · ");
}

function since(iso?: string): string | undefined {
  if (!iso) return undefined;
  const m = Math.max(0, Math.floor((Date.now() - Date.parse(iso)) / 60000));
  if (m < 1) return t("assistant-tasks.just_started");
  if (m < 60) return t("assistant-tasks.minutes", { count: m });
  return m % 60 ? t("assistant-tasks.hours_minutes", { hours: Math.floor(m / 60), minutes: m % 60 }) : t("assistant-tasks.hours", { count: m / 60 });
}

/** What a task's lead agent says or does now, in a line (lib/todos taskNow): its words, or the step it is on. */
function progressOf(lead: Session | undefined) {
  const now = taskNow(lead, !!lead && stateOf(lead) === "working");
  return now && { text: now.kind === "words" ? plainMarkdown(now.text) : now.text, for: now.kind === "step" ? since(now.since) : undefined };
}

/** Opens a task's detail beside the conversation: its agent's own chat (the side panel). */
export function openTask(id: string) {
  openPanel({ kind: "task", todo: id });
}

// Dev only: `?task=<todo id>` opens that task's panel (screenshots, docs/testing.md).
if (import.meta.env.DEV && typeof location !== "undefined") {
  const id = new URLSearchParams(location.search).get("task");
  if (id) setTimeout(() => openTask(id), 0);
}

/** 重试 on a task that did not start, or 继续 on one the model service cut off (lib/feed retryTask), saying why when the
 *  hub refused (the machine offline…). */
export function retryTaskNow(todo: Todo, verb: "retry" | "continue" = "retry") {
  retryTask(todo).catch((e) => toast(verb === "continue" ? t("assistant-tasks.continue_failed", { error: String(e instanceof Error ? e.message : e) }) : t("assistant-tasks.retry_failed", { error: String(e instanceof Error ? e.message : e) }), "error"));
}


/** A task's 重试 is on its way to the hub. */
export function useRetrying(todoId: string | undefined): boolean {
  return useFeed((s) => (todoId ? Boolean(s.busy[retryKey(todoId)]) : false));
}

// ── the plan card ──

const PERMISSION: Record<string, string> = { acceptEdits: t("assistant-tasks.perm_accept_edits"), plan: t("assistant-tasks.perm_plan"), bypassPermissions: t("assistant-tasks.perm_bypass"), default: t("assistant-tasks.perm_default") };

export function PlanCard({ plan }: { plan: TaskPlan }) {
  const busy = useFeed((s) => s.busy[plan.id]);
  const [remember, setRemember] = useState(false);
  if (plan.state !== "open") {
    const failed = plan.tasks.filter((t) => t.error);
    return (
      <div className="flex flex-col gap-1 text-[13px] text-ink-2">
        <span className="flex items-center gap-1.5">
          <Icon name={plan.state === "started" ? "check" : "x"} size={13} />
          {plan.state === "started" ? t("assistant-tasks.plan_started", { count: plan.tasks.length }) : t("assistant-tasks.plan_dismissed", { count: plan.tasks.length })}
        </span>
        {failed.map((task) => (
          <span key={task.title} className="text-red">
            {t("assistant-tasks.plan_task_failed", { title: task.title, error: task.error ?? "" })}
          </span>
        ))}
      </div>
    );
  }
  const run = (f: () => Promise<void>) => f().catch((e) => toast(t("assistant-tasks.start_failed", { error: String(e instanceof Error ? e.message : e) }), "error"));
  return (
    <div className="flex flex-col gap-3 rounded-card bg-surface p-4 shadow-card">
      <div className="flex items-center gap-1.5 text-[12px] font-medium text-ink-2">
        <Icon name="list" size={13} />
        {plan.tasks.length > 1 ? t("assistant-tasks.plan_split", { count: plan.tasks.length }) : t("assistant-tasks.plan_single")}
      </div>
      {plan.tasks.map((t, i) => (
        <PlanTaskRow key={i} n={i + 1} task={t} numbered={plan.tasks.length > 1} />
      ))}
      <label className="flex cursor-pointer items-center gap-2 text-[12.5px] text-ink-2">
        <input type="checkbox" checked={remember} onChange={(e) => setRemember(e.target.checked)} className="accent-[var(--accent)]" />
        {t("assistant-tasks.plan_remember")}
      </label>
      <div className="flex gap-2">
        <Button tone="accent" busy={busy} onClick={() => run(() => startPlan(plan, remember))} className="flex-1">
          {plan.tasks.length > 1 ? t("assistant-tasks.plan_start_n", { count: plan.tasks.length }) : t("assistant-tasks.plan_start")}
        </Button>
        <Button disabled={busy} onClick={() => run(() => dismissPlan(plan))}>
          {t("assistant-tasks.plan_dismiss")}
        </Button>
      </div>
    </div>
  );
}

function PlanTaskRow({ n, task, numbered }: { n: number; task: PlanTask; numbered: boolean }) {
  const machines = useStore((s) => s.machines);
  const machine = machines.find((m) => m.id === task.machine)?.name ?? task.machine;
  const chips: { icon: IconName; text: string }[] = [
    { icon: "machine", text: machine },
    { icon: "folder", text: task.cwd.split("/").filter(Boolean).at(-1) ?? task.cwd },
    { icon: "agent", text: task.model || task.launcher || t("assistant-tasks.default_model") },
  ];
  return (
    <div className={cx("flex flex-col gap-1.5", n > 1 && "border-t border-line pt-3")}>
      <div className="flex items-baseline gap-2 text-[14.5px] font-semibold">
        {numbered && <span className="flex size-[18px] shrink-0 items-center justify-center rounded-full bg-accent-tint text-[11px] font-bold text-accent">{n}</span>}
        {task.title}
      </div>
      <div className="flex flex-wrap gap-1.5">
        {chips.map((c) => (
          <span key={c.icon} className="inline-flex h-6 items-center gap-1 rounded-full bg-field px-2 text-[12px] text-ink-2">
            <Icon name={c.icon} size={12} />
            {c.text}
          </span>
        ))}
        <span className={cx("inline-flex h-6 items-center rounded-full px-2 text-[12px]", task.readonly ? "bg-accent-tint text-accent" : "bg-field text-ink-2")}>
          {task.readonly ? t("assistant-tasks.readonly") : PERMISSION[task.permission || "default"] ?? task.permission}
        </span>
      </div>
      {task.why && <div className="text-[12px] text-ink-2">{task.why}</div>}
    </div>
  );
}

// ── task cards ──

export function TaskCard({ todoId, conversation }: { todoId: string; conversation: string }) {
  const todo = useTodo(todoId);
  const lookup = useTaskLookup();
  const plans = useFeed((s) => s.plans);
  const status = todo ? statusOf(todo, lookup, plans) : undefined;
  const menu = useTaskMenu(todo, status);
  const retrying = useRetrying(todoId);
  useNow(30_000, status === "working"); // how long its step has gone on
  if (!todo || !status) return null;
  const lead = mostUrgentAgent(todo, lookup)?.session;
  const info = TODO_STATUS_INFO[status];
  const startError = todo.agents?.find((a) => a.start_error)?.start_error;
  const cut = status === "interrupted" ? interruption(lead) : undefined;
  const progress = progressOf(lead);
  const now =
    status === "needs_you"
      ? t("assistant-tasks.needs_answer")
      : cut
        ? t("assistant-tasks.interrupted_reason", { reason: cut.reason })
        : status === "failed"
        ? t("assistant-tasks.start_failed", { error: startFailure(todo, plans) ?? t("assistant-tasks.unknown_reason") })
        : progress?.text || (startError ? t("assistant-tasks.start_failed", { error: startError }) : status === "not_started" ? t("assistant-tasks.starting_agent") : t("assistant-tasks.no_activity"));
  const stepFor = now === progress?.text ? progress.for : undefined;
  // Its subagents add to its own progress, below it, and never stand in for it.
  const subagents = status === "working" || status === "needs_you" ? runningSubagents(lead) : [];
  return (
    <div
      role="button"
      tabIndex={0}
      onClick={() => openTask(todo.id)}
      onKeyDown={(e) => e.key === "Enter" && openTask(todo.id)}
      onContextMenu={menu.onContextMenu}
      className={cx("flex cursor-pointer flex-col gap-2 rounded-card bg-surface p-4 shadow-card transition-shadow hover:shadow-raised", status === "needs_you" && "ring-[1.5px] ring-red")}
    >
      <div className="flex items-center gap-2">
        <TaskMark status={status} />
        <span className="min-w-0 flex-1 truncate text-[14.5px] font-semibold">{todo.title}</span>
        {status !== "done" && status !== "failed" && <span className="shrink-0 text-[12px] text-ink-3">{since(todo.created_at)}</span>}
      </div>
      <div className="truncate text-[12px] text-ink-2">{place(todo, lead)}</div>
      <div className={cx("flex items-start gap-2 rounded-control bg-field px-3 py-2 text-[13px]", (status === "needs_you" || status === "failed" || cut) && "text-red")}>
        <Icon name={status === "needs_you" ? "hand" : status === "failed" || cut ? "info" : "terminal"} size={13} className="mt-0.5 shrink-0 text-ink-2" />
        <span className="flex min-w-0 flex-col gap-0.5">
          <span className="line-clamp-2">
            {now}
            {stepFor && <span className="text-ink-3"> · {stepFor}</span>}
          </span>
          {subagents.length > 0 && (
            <span className="truncate text-[12px] text-ink-3">
              {subagents.length > 3
                ? t("assistant-tasks.subagents_running_more", { names: subagents.slice(0, 3).join(t("assistant-tasks.list_separator")), count: subagents.length })
                : t("assistant-tasks.subagents_running", { names: subagents.slice(0, 3).join(t("assistant-tasks.list_separator")) })}
            </span>
          )}
        </span>
      </div>
      {status !== "done" && (
        <div className="flex items-center text-[12.5px]">
          <span className={cx("font-medium", TONE[info.tone])}>{info.label}</span>
          <span className="flex-1" />
          {status === "failed" ? (
            // Nothing to say a word to: the ways out are on the card — start it again as it was, or let it go.
            <span className="flex items-center gap-4">
              <button
                type="button"
                disabled={retrying}
                onClick={(e) => {
                  e.stopPropagation();
                  retryTaskNow(todo);
                }}
                className="inline-flex items-center gap-1 whitespace-nowrap font-semibold text-accent hover:underline disabled:opacity-50 disabled:hover:no-underline"
              >
                <Icon name="retry" size={12} />
                {retrying ? t("assistant-tasks.retrying") : t("assistant-tasks.retry")}
              </button>
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  removeTaskNow(todo);
                }}
                className="inline-flex items-center gap-1 whitespace-nowrap font-semibold text-red hover:underline"
              >
                <Icon name="trash" size={12} />
                {t("assistant-tasks.remove")}
              </button>
            </span>
          ) : (
            <span className="flex items-center gap-4">
              {cut?.canContinue && (
                // The model service cut its turn off: 继续 tells its agent to go on, as the chat's 重试 does (#1516).
                <button
                  type="button"
                  disabled={retrying}
                  onClick={(e) => {
                    e.stopPropagation();
                    retryTaskNow(todo, "continue");
                  }}
                  className="inline-flex items-center gap-1 whitespace-nowrap font-semibold text-accent hover:underline disabled:opacity-50 disabled:hover:no-underline"
                >
                  <Icon name="retry" size={12} />
                  {t("assistant-tasks.continue")}
                </button>
              )}
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  setRoute(conversation, todo.id);
                }}
                className="inline-flex items-center gap-1 whitespace-nowrap font-semibold text-accent hover:underline"
              >
                <Icon name="send" size={12} />
                {t("assistant-tasks.say_something")}
              </button>
            </span>
          )}
        </div>
      )}
      {menu.menu}
    </div>
  );
}

/** A task of another day, folded into a line. */
export function TaskLine({ todoId }: { todoId: string }) {
  const todo = useTodo(todoId);
  const lookup = useTaskLookup();
  const plans = useFeed((s) => s.plans);
  const status = todo ? statusOf(todo, lookup, plans) : undefined;
  const menu = useTaskMenu(todo, status);
  if (!todo || !status) return null;
  const info = TODO_STATUS_INFO[status];
  return (
    <>
    <button type="button" onClick={() => openTask(todo.id)} onContextMenu={menu.onContextMenu} className="flex w-full items-center gap-2 rounded-control bg-field px-3 py-2 text-left text-[13px] hover:bg-hover">
      <TaskMark status={status} />
      <span className="min-w-0 flex-1 truncate font-medium">{todo.title}</span>
      <span className={cx("shrink-0 text-[12px]", TONE[info.tone])}>{info.label}</span>
      <Icon name="chevronRight" size={13} className="text-ink-3" />
    </button>
    {menu.menu}
    </>
  );
}

// ── the hub's lines ──

export function FeedLineView({ item }: { item: FeedItem }) {
  const plan = useFeed((s) => (item.kind === "plan" && item.ref ? s.plans[item.ref] : undefined));
  const isLater = useFeed((s) => (item.ref ? s.later[item.ref] : false));
  const title = useStore((s) => s.todos.find((todo) => todo.id === item.ref)?.title) ?? t("assistant-tasks.task_fallback");
  const known = useStore((s) => s.todos.some((t) => t.id === item.ref));
  switch (item.kind) {
    case "plan":
      return plan ? <PlanCard plan={plan} /> : null;
    case "task":
      return item.ref ? isToday(item.at) ? <TaskCard todoId={item.ref} conversation={item.conversation} /> : <TaskLine todoId={item.ref} /> : null;
    case "answered":
      return <AnsweredLine item={item} title={title} onOpenTask={known && item.ref ? () => openTask(item.ref!) : undefined} />;
    case "receipt":
      return <ReceiptCard item={item} />;
    case "routed":
      return (
        <div className="flex flex-col items-end gap-1 pl-10">
          <span className="inline-flex items-center gap-1 text-[12px] font-medium text-accent">
            <Icon name="send" size={11} />
            {t("assistant-tasks.say_to", { title })}
          </span>
          <div className="rounded-[18px] bg-field px-3.5 py-2 text-[14.5px]">{field(item, "text")}</div>
        </div>
      );
    case "today":
      return <TodayCard body={item.body as unknown as TodayBody} conversation={item.conversation} />;
    case "message":
      return item.ref && !isLater ? <MessageCard item={item} /> : null;
    case "view":
      // a live card: drawn where it was first shown, again at each version (ui/AssistantCard.tsx)
      return item.ref ? <LiveCard conversation={item.conversation} id={item.ref} first={(item.body as { view?: AssistantView } | undefined)?.view} /> : null;
    default:
      return null;
  }
}

/** A card of a task's agent the user dealt with, kept as a line「✓ 任务 · 回答」. Click it for every question with its
 *  answer in full (#1231), selectable, and again to fold it; the task's name opens the task. */
export function AnsweredLine({ item, title, onOpenTask }: { item: FeedItem; title: string; onOpenTask?: () => void }) {
  const [open, setOpen] = useState(false);
  const items = answeredItems(item);
  const expandable = items.length > 0;
  return (
    <div className={cx("flex max-w-full flex-col self-start bg-field text-[12px] text-ink-2", open ? "w-full rounded-card" : "rounded-full")}>
      <div className="flex min-w-0 items-center gap-1.5 px-3 py-1.5">
        <Icon name="check" size={12} className="shrink-0" />
        {onOpenTask ? (
          <button type="button" onClick={onOpenTask} className="max-w-[45%] shrink-0 truncate text-left hover:text-ink hover:underline">
            {title}
          </button>
        ) : (
          <span className="max-w-[45%] shrink-0 truncate">{title}</span>
        )}
        <button
          type="button"
          aria-expanded={expandable ? open : undefined}
          disabled={!expandable}
          onClick={() => setOpen(!open)}
          className="flex min-w-0 flex-1 items-center gap-1.5 text-left enabled:cursor-pointer"
        >
          <span className="min-w-0 flex-1 truncate">· {field(item, "answer") ?? t("assistant-tasks.answered")}</span>
          {expandable && (
            <Icon name="chevronRight" size={12} stroke={2.2} className="shrink-0 text-ink-3 transition-transform duration-200" style={{ transform: open ? "rotate(90deg)" : "none" }} />
          )}
        </button>
      </div>
      <Collapse open={open} lazy>
        <div className="flex flex-col gap-2.5 px-3 pt-0.5 pb-2.5 select-text" data-testid="answered-detail">
          {items.map((x, i) => (
            <div key={i} className="flex flex-col gap-0.5 whitespace-pre-wrap [overflow-wrap:anywhere]">
              {x.question && <div className="font-medium text-ink-3">{x.question}</div>}
              {x.answer && <div className="text-[13px] leading-[1.55] text-ink">{x.answer}</div>}
            </div>
          ))}
        </div>
      </Collapse>
    </div>
  );
}

function ReceiptCard({ item }: { item: FeedItem }) {
  const state = receiptState(item);
  if (state === "interrupted") return <InterruptedReceipt item={item} />;
  return <OutcomeReceipt item={item} failed={state === "failed"} />;
}

/** A task the model service cut off (#1516): red, why in words, 继续 while it is still cut off; once its agent went on,
 *  a quiet line that opens the task (a later receipt says how it ended). */
function InterruptedReceipt({ item }: { item: FeedItem }) {
  const todo = useTodo(item.ref);
  const lookup = useTaskLookup();
  const plans = useFeed((s) => s.plans);
  const retrying = useRetrying(todo?.id);
  const status = todo ? statusOf(todo, lookup, plans) : undefined;
  const title = field(item, "title") ?? t("assistant-tasks.task_fallback");
  if (todo && status && status !== "interrupted") {
    return (
      <button type="button" onClick={() => openTask(todo.id)} className="flex items-center gap-1.5 self-start text-left text-[13px] text-ink-2 hover:text-ink">
        <Icon name={status === "done" ? "check" : "retry"} size={13} />
        {status === "done" ? t("assistant-tasks.receipt_done", { title }) : t("assistant-tasks.receipt_continued", { title })}
      </button>
    );
  }
  const cut = todo ? interruption(mostUrgentAgent(todo, lookup)?.session) : undefined;
  const raw = field(item, "error") ?? field(item, "summary");
  const reason = cut?.reason ?? parseAPIError(raw)?.reason;
  const canContinue = todo && (cut ? cut.canContinue : true);
  return (
    <div className="flex gap-3 rounded-card bg-red-tint p-4">
      <Icon name="info" size={18} stroke={2.4} className="text-red" />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="text-[14.5px] font-semibold">{t("assistant-tasks.interrupted_title", { title })}</div>
        <div className="line-clamp-4 text-[13px] text-ink-2">{reason ? t("assistant-tasks.interrupted_reason", { reason }) : t("assistant-tasks.interrupted")}</div>
        <div className="mt-1 flex gap-2">
          {canContinue && (
            <Button icon="retry" busy={retrying} disabled={retrying} onClick={() => retryTaskNow(todo, "continue")}>
              {t("assistant-tasks.continue")}
            </Button>
          )}
          {item.ref && <Button onClick={() => openTask(item.ref!)}>{t("assistant-tasks.look")}</Button>}
        </div>
      </div>
    </div>
  );
}

function OutcomeReceipt({ item, failed }: { item: FeedItem; failed: boolean }) {
  // A failed start's banner follows its task: 重试 while it is still failed (with its latest error), a quiet line once
  // it was started again, so no stale error stays in the conversation (#1396).
  const todo = useTodo(failed ? item.ref : undefined);
  const lookup = useTaskLookup();
  const plans = useFeed((s) => s.plans);
  const retrying = useRetrying(todo?.id);
  const status = todo ? statusOf(todo, lookup, plans) : undefined;
  const pr = field(item, "pr");
  const prN = pr?.split("/").at(-1);
  const title = field(item, "title") ?? t("assistant-tasks.task_fallback");
  if (failed && todo && status && status !== "failed") {
    const again = status === "not_started" ? t("assistant-tasks.receipt_restarting", { title }) : t("assistant-tasks.receipt_restarted", { title });
    return (
      <button type="button" onClick={() => openTask(todo.id)} className="flex items-center gap-1.5 self-start text-left text-[13px] text-ink-2 hover:text-ink">
        <Icon name={status === "not_started" ? "retry" : "check"} size={13} />
        {again}
      </button>
    );
  }
  const failure = failed && todo ? startFailure(todo, plans) : undefined;
  const summary = failure ? t("assistant-tasks.start_failed", { error: failure }) : field(item, "summary");
  const heading = failed ? t("assistant-tasks.outcome_failed", { title }) : t("assistant-tasks.outcome_done", { title });
  // 复制 / 引用 like a reply's (#2462): the quote names the task and its todo id for the assistant
  return (
    <MessageActions text={summary?.trim() ? summary : heading} ts={item.at} task={{ title, ...(item.ref ? { id: item.ref } : {}) }} corner>
      <div className={cx("flex gap-3 rounded-card p-4", failed ? "bg-red-tint" : "bg-green-tint")} data-receipt>
        <Icon name={failed ? "info" : "check"} size={18} stroke={2.4} className={failed ? "text-red" : "text-green"} />
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="text-[14.5px] font-semibold">{heading}</div>
          {summary && <ReceiptSummary text={summary} plain={failed} />}
          <div className="mt-1 flex gap-2">
            {failed && todo && (
              <Button icon="retry" busy={retrying} disabled={retrying} onClick={() => retryTaskNow(todo)}>
                {t("assistant-tasks.retry")}
              </Button>
            )}
            {pr && (
              <Button icon="git" onClick={() => window.open(pr, "_blank", "noopener")}>
                PR #{prN}
              </Button>
            )}
            {!failed && item.ref && <Button onClick={() => openTask(item.ref!)}>{t("assistant-tasks.look")}</Button>}
          </div>
        </div>
      </div>
    </MessageActions>
  );
}

/** What the task's agent said last, as Markdown (a start error is plain text), in about four lines (#1508). */
export function ReceiptSummary({ text, plain }: { text: string; plain?: boolean }) {
  if (plain) return <div className="line-clamp-4 text-[13px] text-ink-2">{text}</div>;
  return (
    <div className="line-clamp-4 text-ink-2">
      <Markdown text={text} compact />
    </div>
  );
}

export function QuickResultCard({ result }: { result: QuickResult }) {
  const when = result.when ? formatDate(result.when, { weekday: "short", hour: "2-digit", minute: "2-digit" }) : undefined;
  return (
    <div className="flex items-center gap-3 rounded-card bg-field px-3.5 py-3">
      <span className={cx("flex size-8 shrink-0 items-center justify-center rounded-control", result.kind === "reminder" ? "bg-orange-tint text-orange" : "bg-accent-tint text-accent")}>
        <Icon name={result.kind === "reminder" ? "bell" : "clock"} size={16} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="text-[13.5px] font-semibold">{result.kind === "reminder" ? t("assistant-tasks.reminder_added") : t("assistant-tasks.calendar_added")}</div>
        <div className="truncate text-[12px] text-ink-2">{[when, result.title, result.place].filter(Boolean).join(" · ")}</div>
      </div>
      <span className="shrink-0 text-[11.5px] text-ink-3">{t("assistant-tasks.on_iphone")}</span>
    </div>
  );
}

// ── 今天 ──

function TodayCard({ body, conversation }: { body: TodayBody; conversation: string }) {
  const say = useSayToPersona(conversation);
  const rows: { icon: IconName; tint: string; title: string; detail?: string; action?: string; run?: () => void }[] = [];
  if (body.done?.length)
    rows.push({ icon: "check", tint: "bg-green-tint text-green", title: body.done.length === 1 ? t("assistant-tasks.today_done_single", { title: body.done[0].title }) : t("assistant-tasks.today_done_n", { count: body.done.length }), detail: body.done.length > 1 ? body.done.map((d) => d.title).join(t("assistant-tasks.list_separator")) : undefined, action: t("assistant-tasks.look"), run: () => openTask(body.done![0].id) });
  if (body.waiting?.length)
    rows.push({ icon: "hand", tint: "bg-red-tint text-red", title: body.waiting.length === 1 ? t("assistant-tasks.today_waiting_single", { title: body.waiting[0].title }) : t("assistant-tasks.today_waiting_n", { count: body.waiting.length }), action: t("assistant-tasks.go_look"), run: () => openTask(body.waiting![0].id) });
  if (body.working) rows.push({ icon: "gear", tint: "bg-orange-tint text-orange", title: t("assistant-tasks.today_working", { count: body.working }), action: t("assistant-tasks.all"), run: () => navigate({ name: "todos" }) });
  for (const m of (body.meetings ?? []).slice(0, 3)) {
    const at = formatDate(m.start, { hour: "2-digit", minute: "2-digit" });
    rows.push({ icon: "clock", tint: "bg-accent-tint text-accent", title: t("assistant-tasks.today_meeting", { time: at, title: m.title }), action: t("assistant-tasks.prepare"), run: () => say(t("assistant-tasks.prepare_prompt", { time: at, title: m.title })) });
  }
  if (body.at_me?.length)
    rows.push({
      icon: "at",
      tint: "bg-accent-tint text-accent",
      title: body.unread ? t("assistant-tasks.today_at_me_unread", { count: body.at_me.length, unread: body.unread }) : t("assistant-tasks.today_at_me", { count: body.at_me.length }),
      detail: body.at_me.slice(0, 2).map((c) => (c.preview ? t("assistant-tasks.chat_preview", { name: c.name, preview: c.preview }) : c.name)).join("\n"),
      action: t("assistant-tasks.review"),
      run: () => say(t("assistant-tasks.review_prompt")),
    });
  return (
    <div className="flex flex-col rounded-card bg-surface p-4 shadow-card">
      <div className="mb-2 flex items-center gap-1.5 text-[12px] font-medium text-ink-2">
        <Icon name="sun" size={13} />
        {t("assistant-tasks.today_title")}
      </div>
      {rows.length === 0 && <div className="text-[13px] text-ink-2">{t("assistant-tasks.today_empty")}</div>}
      {rows.map((r, i) => (
        <div key={i} className={cx("flex items-center gap-3 py-2", i > 0 && "border-t border-line")}>
          <span className={cx("flex size-8 shrink-0 items-center justify-center rounded-control", r.tint)}>
            <Icon name={r.icon} size={15} />
          </span>
          <div className="min-w-0 flex-1">
            <div className="text-[13.5px] font-medium">{r.title}</div>
            {r.detail && <div className="line-clamp-2 whitespace-pre-line text-[12px] text-ink-2">{r.detail}</div>}
          </div>
          {r.action && r.run && <Button onClick={r.run}>{r.action}</Button>}
        </div>
      ))}
    </div>
  );
}

/** Says something to Persona in this conversation (the 今天 card's actions). */
function useSayToPersona(conversation: string) {
  const assistant = useStore((s) => s.assistant);
  return (text: string) => {
    const conv = assistant?.conversations.find((c) => c.session_id === conversation);
    if (!conv) return;
    window.dispatchEvent(new CustomEvent("persona:say", { detail: { mid: conv.machine_id, sid: conv.session_id, text } }));
  };
}

// ── message cards ──

function MessageCard({ item }: { item: FeedItem }) {
  const todo = useTodo(field(item, "todo_id"));
  const lookup = useTaskLookup();
  const sent = cardSent(item);
  const busy = useFeed((s) => !!s.busy[item.id]);
  const [text, setText] = useState<string | null>(null);
  const draft = text ?? field(item, "draft") ?? "";
  const blank = sent ? undefined : draftPlaceholder(draft);
  const send = async () => {
    try {
      await sendMessage(item, draft);
      toast(t("assistant-tasks.sent_done"), "ok");
    } catch (e) {
      toast(t("assistant-tasks.send_failed", { error: String(e instanceof Error ? e.message : e) }), "error");
    }
  };
  const plans = useFeed((s) => s.plans);
  const status = todo ? statusOf(todo, lookup, plans) : undefined;
  return (
    <div className="flex flex-col gap-2.5 rounded-card bg-surface p-4 shadow-card" data-testid="message-card">
      <div className="flex items-center gap-1.5 text-[12px] font-medium text-ink-2">
        <Icon name={field(item, "source") === "mail" ? "mail" : "chat"} size={13} className="text-accent" />
        {[field(item, "sender"), field(item, "chat")].filter(Boolean).join(" · ")}
      </div>
      <div className="border-l-[3px] border-accent pl-3 text-[14.5px]">{field(item, "text")}</div>
      {todo && status && (
        <button type="button" onClick={() => openTask(todo.id)} className="flex items-center gap-2 rounded-control bg-field px-3 py-2 text-left text-[12.5px] hover:bg-hover">
          <TaskMark status={status} />
          <span className="font-semibold">{todo.title}</span>
          <span className="min-w-0 flex-1 truncate text-ink-2">{progressOf(mostUrgentAgent(todo, lookup)?.session)?.text || TODO_STATUS_INFO[status].label}</span>
          <Icon name="chevronRight" size={13} className="text-ink-3" />
        </button>
      )}
      <div className={cx("flex flex-col gap-1 rounded-control p-3", sent ? "bg-green-tint" : "bg-accent-tint")}>
        <span className={cx("flex items-center gap-1 text-[12px] font-semibold", sent ? "text-green" : "text-accent")}>
          <Icon name={sent ? "check" : "pencil"} size={12} />
          {sent ? t("assistant-tasks.replied_as_you") : t("assistant-tasks.draft_from_progress")}
          {sent && <span className="ml-auto font-normal text-ink-3">{clock(sent.at)}</span>}
        </span>
        {!sent ? (
          <textarea value={draft} onChange={(e) => setText(e.target.value)} rows={Math.min(6, Math.max(2, draft.split("\n").length))} className="resize-none bg-transparent text-[14.5px] outline-none" />
        ) : (
          <div className="whitespace-pre-line text-[14.5px]">{sent.text}</div>
        )}
      </div>
      {blank && <div className="text-[12px] text-red">{t("assistant-tasks.draft_blank", { blank })}</div>}
      {!sent && (
        <div className="flex gap-2">
          <Button tone="accent" busy={busy} disabled={!!blank || !draft.trim()} onClick={send} className="flex-1">
            {t("assistant-tasks.send")}
          </Button>
          <Button disabled={busy} onClick={() => later(item.ref!)}>
            {t("assistant-tasks.later")}
          </Button>
        </div>
      )}
    </div>
  );
}

// ── above the composer ──

/** The oldest question the conversation's tasks' agents ask, under its task's name (还有 N 件). */
export function TaskQuestions({ conversation }: { conversation: string }) {
  const todos = useStore((s) => s.todos);
  const interactions = useStore((s) => s.interactions);
  const answeredAll = useStore((s) => s.answered);
  const { asks, answered } = useMemo(() => {
    const out: { todo: Todo; ix: Interaction }[] = [];
    const keys = new Set<string>();
    for (const t of todos) {
      if (t.origin !== conversation || t.done_at) continue;
      const mine = (t.agents ?? []).filter((a) => !a.released_at).map((a) => `${a.machine_id}/${a.session_id}`);
      mine.forEach((k) => keys.add(k));
      for (const ix of interactions) if (mine.includes(`${ix.machine_id}/${ix.session_id}`)) out.push({ todo: t, ix });
    }
    // just answered here: shown answered for a moment above the next one
    const done = Object.values(answeredAll).filter((a) => keys.has(`${a.ix.machine_id}/${a.ix.session_id}`));
    return { asks: out.sort((a, b) => (a.ix.created_at ?? "").localeCompare(b.ix.created_at ?? "")), answered: done };
  }, [todos, interactions, answeredAll, conversation]);
  const first = asks[0];
  if (!first && !answered.length) return null;
  return (
    <div className="flex flex-col gap-1">
      {answered.map((a) => (
        <AnsweredCard key={a.ix.id} card={a} />
      ))}
      {first && (
        <>
          <div className="flex items-center gap-1.5 px-1 text-[12px] font-semibold text-red">
            <Icon name="hand" size={13} />
            <span className="truncate">{first.todo.title}</span>
            <span className="flex-1" />
            {asks.length > 1 && <span className="shrink-0">{t("assistant-tasks.more_asks", { count: asks.length - 1 })}</span>}
          </div>
          <InteractionCard ix={first.ix} />
        </>
      )}
    </div>
  );
}

/** The dock: 全部 N held at its left edge (it does not scroll), then a pill for each task going on (waiting first) and,
 * in the main conversation, each agent going on that no task stands for (#1113), scrolling on their own (iOS TaskDock). */
export function TaskDock({ conversation }: { conversation: string }) {
  const todos = useStore((s) => s.todos);
  const sessions = useStore((s) => s.sessions);
  const main = useStore((s) => s.assistant?.main);
  const lookup = useTaskLookup();
  const plans = useFeed((s) => s.plans);
  const [menuFor, setMenuFor] = useState<{ todo: Todo; el: HTMLElement } | null>(null);
  const agents = useMemo(() => dockAgents(Object.values(sessions).flat(), todos, conversation, main), [sessions, todos, conversation, main]);
  const mine = todos.filter((t) => t.origin === conversation);
  if (mine.length === 0 && agents.length === 0) return null;
  // Failed, done and stopped tasks are not in the dock.
  const rank: Partial<Record<TodoStatus, number>> = { needs_you: 0, interrupted: 0, working: 1, not_started: 1, unread: 2 };
  const going = mine
    .map((t) => ({ t, st: statusOf(t, lookup, plans) }))
    .filter((x) => rank[x.st] !== undefined)
    .sort((a, b) => rank[a.st]! - rank[b.st]! || b.t.created_at.localeCompare(a.t.created_at));
  // 全部 and the pills share one capsule (height, radius, padding, size), like iOS DockPillLabel.
  const capsule = "inline-flex h-8 shrink-0 items-center whitespace-nowrap rounded-full px-3 text-[12.5px]";
  const pill = cx(capsule, "max-w-[240px] gap-2 font-semibold");
  return (
    // Top-aligned, so a scrollbar an engine still draws under the pills cannot push 全部 down from them (#1381).
    <div className="flex min-w-0 items-start gap-2" data-testid="task-dock">
      <button
        type="button"
        data-testid="task-dock-board"
        onClick={() => navigate({ name: "todos" })}
        className={cx(capsule, "bg-field font-medium text-ink-2 hover:bg-hover")}
      >
        {t("assistant-tasks.dock_all", { count: mine.length + agents.length })}
      </button>
      {(going.length > 0 || agents.length > 0) && (
        // The scroller pads by the focus ring's width and takes it back, so the ring is not clipped and the row
        // stays a pill high.
        <div className="scrollbar-none -my-1 flex min-w-0 flex-1 items-center gap-2 overflow-x-auto py-1">
          {going.map(({ t: todo, st }) => (
            <button
              key={todo.id}
              type="button"
              onClick={() => openTask(todo.id)}
              onContextMenu={(e) => {
                if (!isRemovable(st)) return;
                e.preventDefault();
                setMenuFor({ todo, el: e.currentTarget });
              }}
              className={cx(pill, st === "needs_you" || st === "interrupted" ? "bg-red-tint text-red" : "bg-field text-ink hover:bg-hover")}
            >
              <TaskMark status={st} size={8} />
              <span className="truncate">{todo.title}</span>
              {st === "needs_you" && <span className="shrink-0">{t("assistant-tasks.waiting")}</span>}
              {st === "interrupted" && <span className="shrink-0">{t("assistant-tasks.interrupted_short")}</span>}
            </button>
          ))}
          {agents.map(({ session: s, state }) => (
            <button
              key={`${s.machine_id}/${s.session_id}`}
              type="button"
              aria-label={state === "needs_you" ? t("assistant-tasks.dock_agent_waiting", { title: sessionTitle(s) }) : t("assistant-tasks.dock_agent_working", { title: sessionTitle(s) })}
              onClick={() => openPanel({ kind: "agent", mid: s.machine_id, sid: s.session_id })}
              className={cx(pill, state === "needs_you" ? "bg-red-tint text-red" : "bg-field text-ink hover:bg-hover")}
            >
              <StatusDot state={state} size={8} />
              <Icon name="agent" size={13} className={cx("shrink-0", state !== "needs_you" && "text-ink-2")} />
              <span className="truncate">{sessionTitle(s)}</span>
              {state === "needs_you" && <span className="shrink-0">{t("assistant-tasks.waiting")}</span>}
            </button>
          ))}
        </div>
      )}
      {menuFor && (
        <Menu
          anchor={menuFor.el}
          side="top"
          onClose={() => setMenuFor(null)}
          items={[{ key: "remove", label: t("assistant-tasks.remove_ellipsis"), icon: "trash", danger: true, onSelect: () => removeTaskNow(menuFor.todo) }]}
        />
      )}
    </div>
  );
}

/** 「对「xx」说」 while the next message goes straight to a task; × gives it back to Persona. */
export function RouteChip({ conversation }: { conversation: string }) {
  const todoId = useFeed((s) => s.routes[conversation]);
  const todo = useTodo(todoId);
  if (!todo) return null;
  return (
    <div className="flex">
      <span className="inline-flex h-7 items-center gap-1.5 rounded-full bg-accent-tint pl-3 pr-1 text-[12.5px] font-semibold text-accent">
        <Icon name="send" size={12} />
        {t("assistant-tasks.say_to", { title: todo.title })}
        <button type="button" aria-label={t("assistant-tasks.give_back")} onClick={() => setRoute(conversation, null)} className="flex size-5 items-center justify-center rounded-full hover:bg-hover">
          <Icon name="x" size={11} />
        </button>
      </span>
    </div>
  );
}

export function DayLine({ date }: { date: Date }) {
  return <div className="py-1 text-center text-[12px] text-ink-3">{dayLabel(date)}</div>;
}

/** A task's state for other views (the side panel's header). */
export function useTaskState(todo?: Todo): { status: TodoStatus; lead?: Session; failure?: string } | undefined {
  const lookup = useTaskLookup();
  const plans = useFeed((s) => s.plans);
  if (!todo) return undefined;
  const status = statusOf(todo, lookup, plans);
  return { status, lead: mostUrgentAgent(todo, lookup)?.session, failure: status === "failed" ? startFailure(todo, plans) : undefined };
}

export { TaskMark, place };
