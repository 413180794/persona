// The unified entry in the assistant's conversation (docs/unified-entry.md; iOS Views/Assistant/Tasks): the plan card,
// task cards that follow their agents, the hub's lines (answered, receipt, routed), the 今天 card, message cards about
// a task, quick results, the dock above the composer, the tasks' questions, the route chip, and a task's detail in the
// side panel.
import { useMemo, useState } from "react";
import {
  answeredItems,
  dayLabel,
  dismissPlan,
  field,
  getFeedState,
  isToday,
  later,
  removeTask,
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
import { dockAgents, isRemovable, makeSessionLookup, mostUrgentAgent, todoStatus, TODO_STATUS_INFO, type SessionLookup, type Todo, type TodoStatus } from "../../lib/todos";
import { navigate } from "../../lib/router";
import { openPanel } from "../../lib/panel";
import { getState, useStore } from "../../lib/store";
import type { AgentState, Interaction, Session } from "../../lib/types";
import { Button, Collapse, cx, Menu, StatusDot, toast } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";
import { InteractionCard } from "../chat/Interactions";
import { sessionTitle } from "../../lib/format";

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
    (gone) => gone && toast(`已移除「${todo.title}」`, "ok"),
    (e) => toast(`没移除：${e instanceof Error ? e.message : e}`, "error"),
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
          items={[{ key: "remove", label: "移除任务…", icon: "trash", danger: true, onSelect: () => removeTaskNow(todo) }]}
        />
      ) : null,
  };
}

/** The task's state as an agent state, for its dot and tone. */
function dotState(st: TodoStatus): AgentState {
  return st === "needs_you" ? "needs_you" : st === "working" || st === "not_started" ? "working" : st === "unread" ? "unread" : st === "follow_up" ? "idle" : "offline"; // done, failed
}

const TONE: Record<string, string> = {
  red: "text-red",
  green: "text-green",
  amber: "text-orange",
  idle: "text-ink-2",
  off: "text-ink-3",
};

function TaskMark({ status, size = 9 }: { status: TodoStatus; size?: number }) {
  if (status === "failed") return <Icon name="info" size={size + 4} stroke={2.4} className="text-red" />;
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
  if (m < 1) return "刚开始";
  if (m < 60) return `${m} 分钟`;
  return m % 60 ? `${Math.floor(m / 60)} 小时 ${m % 60} 分` : `${m / 60} 小时`;
}

/** Opens a task's detail beside the conversation: its agent's own chat (the side panel). */
export function openTask(id: string) {
  openPanel({ kind: "task", todo: id });
}

// Dev only: `?task=<todo id>` opens that task's panel (screenshots, docs/testing.md).
if (import.meta.env.DEV && typeof location !== "undefined") {
  const t = new URLSearchParams(location.search).get("task");
  if (t) setTimeout(() => openTask(t), 0);
}

// ── the plan card ──

const PERMISSION: Record<string, string> = { acceptEdits: "可以改文件", plan: "先出方案", bypassPermissions: "全部放行", default: "每步都问" };

export function PlanCard({ plan }: { plan: TaskPlan }) {
  const busy = useFeed((s) => s.busy[plan.id]);
  const [remember, setRemember] = useState(false);
  if (plan.state !== "open") {
    const failed = plan.tasks.filter((t) => t.error);
    return (
      <div className="flex flex-col gap-1 text-[13px] text-ink-2">
        <span className="flex items-center gap-1.5">
          <Icon name={plan.state === "started" ? "check" : "x"} size={13} />
          {plan.state === "started" ? `开了 ${plan.tasks.length} 个任务` : `没开 · ${plan.tasks.length} 个任务`}
        </span>
        {failed.map((t) => (
          <span key={t.title} className="text-red">
            「{t.title}」没开起来：{t.error}
          </span>
        ))}
      </div>
    );
  }
  const run = (f: () => Promise<void>) => f().catch((e) => toast(`没开起来：${e instanceof Error ? e.message : e}`, "error"));
  return (
    <div className="flex flex-col gap-3 rounded-card bg-surface p-4 shadow-card">
      <div className="flex items-center gap-1.5 text-[12px] font-medium text-ink-2">
        <Icon name="list" size={13} />
        {plan.tasks.length > 1 ? `拆成 ${plan.tasks.length} 件事，这样开` : "这样开"}
      </div>
      {plan.tasks.map((t, i) => (
        <PlanTaskRow key={i} n={i + 1} task={t} numbered={plan.tasks.length > 1} />
      ))}
      <label className="flex cursor-pointer items-center gap-2 text-[12.5px] text-ink-2">
        <input type="checkbox" checked={remember} onChange={(e) => setRemember(e.target.checked)} className="accent-[var(--accent)]" />
        这类以后直接开
      </label>
      <div className="flex gap-2">
        <Button tone="accent" busy={busy} onClick={() => run(() => startPlan(plan, remember))} className="flex-1">
          {plan.tasks.length > 1 ? `开始 ${plan.tasks.length} 个任务` : "开始"}
        </Button>
        <Button disabled={busy} onClick={() => run(() => dismissPlan(plan))}>
          不开
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
    { icon: "agent", text: task.model || task.launcher || "默认模型" },
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
          {task.readonly ? "只读" : PERMISSION[task.permission || "default"] ?? task.permission}
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
  if (!todo || !status) return null;
  const lead = mostUrgentAgent(todo, lookup)?.session;
  const info = TODO_STATUS_INFO[status];
  const startError = todo.agents?.find((a) => a.start_error)?.start_error;
  const now =
    status === "needs_you"
      ? "有事等你回答"
      : status === "failed"
        ? `没开起来：${startFailure(todo, plans) ?? "原因不明"}`
        : lead?.last_text?.trim() || (startError ? `没开起来：${startError}` : status === "not_started" ? "正在开 Agent" : "还没有动静");
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
      <div className={cx("flex items-start gap-2 rounded-control bg-field px-3 py-2 text-[13px]", (status === "needs_you" || status === "failed") && "text-red")}>
        <Icon name={status === "needs_you" ? "hand" : status === "failed" ? "info" : "terminal"} size={13} className="mt-0.5 shrink-0 text-ink-2" />
        <span className="line-clamp-2">{now}</span>
      </div>
      {status !== "done" && (
        <div className="flex items-center text-[12.5px]">
          <span className={cx("font-medium", TONE[info.tone])}>{info.label}</span>
          <span className="flex-1" />
          {status === "failed" ? (
            // Nothing to say a word to: the way out is on the card.
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                removeTaskNow(todo);
              }}
              className="inline-flex items-center gap-1 font-semibold text-red hover:underline"
            >
              <Icon name="trash" size={12} />
              移除
            </button>
          ) : (
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                setRoute(conversation, todo.id);
              }}
              className="inline-flex items-center gap-1 font-semibold text-accent hover:underline"
            >
              <Icon name="send" size={12} />
              说一句
            </button>
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
  const title = useStore((s) => s.todos.find((t) => t.id === item.ref)?.title) ?? "任务";
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
            对「{title}」说
          </span>
          <div className="rounded-[18px] bg-field px-3.5 py-2 text-[14.5px]">{field(item, "text")}</div>
        </div>
      );
    case "today":
      return <TodayCard body={item.body as unknown as TodayBody} conversation={item.conversation} />;
    case "message":
      return item.ref && !isLater ? <MessageCard item={item} itemId={item.ref} /> : null;
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
          <span className="min-w-0 flex-1 truncate">· {field(item, "answer") ?? "回答了"}</span>
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
  const failed = field(item, "state") === "failed";
  const pr = field(item, "pr");
  const prN = pr?.split("/").at(-1);
  return (
    <div className={cx("flex gap-3 rounded-card p-4", failed ? "bg-red-tint" : "bg-green-tint")}>
      <Icon name={failed ? "info" : "check"} size={18} stroke={2.4} className={failed ? "text-red" : "text-green"} />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="text-[14.5px] font-semibold">{failed ? `${field(item, "title") ?? "任务"}没开起来` : `${field(item, "title") ?? "任务"} 完成`}</div>
        {field(item, "summary") && <div className="line-clamp-4 text-[13px] text-ink-2">{field(item, "summary")}</div>}
        <div className="mt-1 flex gap-2">
          {pr && (
            <Button icon="git" onClick={() => window.open(pr, "_blank", "noopener")}>
              PR #{prN}
            </Button>
          )}
          {!failed && item.ref && <Button onClick={() => openTask(item.ref!)}>看看</Button>}
        </div>
      </div>
    </div>
  );
}

export function QuickResultCard({ result }: { result: QuickResult }) {
  const when = result.when ? new Date(result.when).toLocaleString("zh-CN", { weekday: "short", hour: "2-digit", minute: "2-digit" }) : undefined;
  return (
    <div className="flex items-center gap-3 rounded-card bg-field px-3.5 py-3">
      <span className={cx("flex size-8 shrink-0 items-center justify-center rounded-control", result.kind === "reminder" ? "bg-orange-tint text-orange" : "bg-accent-tint text-accent")}>
        <Icon name={result.kind === "reminder" ? "bell" : "clock"} size={16} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="text-[13.5px] font-semibold">{result.kind === "reminder" ? "已加提醒" : "已加到日历"}</div>
        <div className="truncate text-[12px] text-ink-2">{[when, result.title, result.place].filter(Boolean).join(" · ")}</div>
      </div>
      <span className="shrink-0 text-[11.5px] text-ink-3">在 iPhone 上</span>
    </div>
  );
}

// ── 今天 ──

function TodayCard({ body, conversation }: { body: TodayBody; conversation: string }) {
  const say = useSayToPersona(conversation);
  const rows: { icon: IconName; tint: string; title: string; detail?: string; action?: string; run?: () => void }[] = [];
  if (body.done?.length)
    rows.push({ icon: "check", tint: "bg-green-tint text-green", title: body.done.length === 1 ? `做完了：${body.done[0].title}` : `做完了 ${body.done.length} 个任务`, detail: body.done.length > 1 ? body.done.map((d) => d.title).join("、") : undefined, action: "看看", run: () => openTask(body.done![0].id) });
  if (body.waiting?.length)
    rows.push({ icon: "hand", tint: "bg-red-tint text-red", title: body.waiting.length === 1 ? `等你：${body.waiting[0].title}` : `${body.waiting.length} 件等你`, action: "去看", run: () => openTask(body.waiting![0].id) });
  if (body.working) rows.push({ icon: "gear", tint: "bg-orange-tint text-orange", title: `${body.working} 个任务在做`, action: "全部", run: () => navigate({ name: "todos" }) });
  for (const m of (body.meetings ?? []).slice(0, 3)) {
    const t = new Date(m.start).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
    rows.push({ icon: "clock", tint: "bg-accent-tint text-accent", title: `${t} ${m.title}`, action: "准备", run: () => say(`帮我准备一下 ${t} 的「${m.title}」：要看的文档、上次的结论、要我拍板的事`) });
  }
  if (body.at_me?.length)
    rows.push({
      icon: "at",
      tint: "bg-accent-tint text-accent",
      title: `飞书 ${body.at_me.length} 个会话 @你${body.unread ? ` · 其他未读 ${body.unread}` : ""}`,
      detail: body.at_me.slice(0, 2).map((c) => c.name + (c.preview ? `：${c.preview}` : "")).join("\n"),
      action: "过一遍",
      run: () => say("帮我过一遍飞书里 @我 的消息：哪些要我回，能起草的先起草"),
    });
  return (
    <div className="flex flex-col rounded-card bg-surface p-4 shadow-card">
      <div className="mb-2 flex items-center gap-1.5 text-[12px] font-medium text-ink-2">
        <Icon name="sun" size={13} />
        今天要知道的
      </div>
      {rows.length === 0 && <div className="text-[13px] text-ink-2">没有等你的事，日程也空着。</div>}
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

function MessageCard({ item, itemId }: { item: FeedItem; itemId: string }) {
  const todo = useTodo(field(item, "todo_id"));
  const lookup = useTaskLookup();
  const sentHere = useFeed((s) => s.sent[itemId]);
  const [text, setText] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const draft = text ?? field(item, "draft") ?? "";
  const send = async () => {
    setBusy(true);
    try {
      await sendMessage(itemId, draft);
      toast("已发送，标为已处理", "ok");
    } catch (e) {
      toast(`回复没发出去：${e instanceof Error ? e.message : e}`, "error");
    } finally {
      setBusy(false);
    }
  };
  const plans = useFeed((s) => s.plans);
  const status = todo ? statusOf(todo, lookup, plans) : undefined;
  return (
    <div className="flex flex-col gap-2.5 rounded-card bg-surface p-4 shadow-card">
      <div className="flex items-center gap-1.5 text-[12px] font-medium text-ink-2">
        <Icon name={field(item, "source") === "mail" ? "mail" : "chat"} size={13} className="text-accent" />
        {[field(item, "sender"), field(item, "chat")].filter(Boolean).join(" · ")}
      </div>
      <div className="border-l-[3px] border-accent pl-3 text-[14.5px]">{field(item, "text")}</div>
      {todo && status && (
        <button type="button" onClick={() => openTask(todo.id)} className="flex items-center gap-2 rounded-control bg-field px-3 py-2 text-left text-[12.5px] hover:bg-hover">
          <TaskMark status={status} />
          <span className="font-semibold">{todo.title}</span>
          <span className="min-w-0 flex-1 truncate text-ink-2">{mostUrgentAgent(todo, lookup)?.session?.last_text ?? TODO_STATUS_INFO[status].label}</span>
          <Icon name="chevronRight" size={13} className="text-ink-3" />
        </button>
      )}
      <div className={cx("flex flex-col gap-1 rounded-control p-3", sentHere ? "bg-green-tint" : "bg-accent-tint")}>
        <span className={cx("flex items-center gap-1 text-[12px] font-semibold", sentHere ? "text-green" : "text-accent")}>
          <Icon name={sentHere ? "check" : "pencil"} size={12} />
          {sentHere ? "已以你的名义回复" : "按任务进度起草的回复"}
        </span>
        {!sentHere ? (
          <textarea value={draft} onChange={(e) => setText(e.target.value)} rows={Math.min(6, Math.max(2, draft.split("\n").length))} className="resize-none bg-transparent text-[14.5px] outline-none" />
        ) : (
          <div className="text-[14.5px]">{draft}</div>
        )}
      </div>
      {!sentHere && (
        <div className="flex gap-2">
          <Button tone="accent" busy={busy} onClick={send} className="flex-1">
            发送
          </Button>
          <Button disabled={busy} onClick={() => later(itemId)}>
            稍后
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
  const asks = useMemo(() => {
    const out: { todo: Todo; ix: Interaction }[] = [];
    for (const t of todos) {
      if (t.origin !== conversation || t.done_at) continue;
      const keys = new Set((t.agents ?? []).filter((a) => !a.released_at).map((a) => `${a.machine_id}/${a.session_id}`));
      for (const ix of interactions) if (keys.has(`${ix.machine_id}/${ix.session_id}`)) out.push({ todo: t, ix });
    }
    return out.sort((a, b) => (a.ix.created_at ?? "").localeCompare(b.ix.created_at ?? ""));
  }, [todos, interactions, conversation]);
  const first = asks[0];
  if (!first) return null;
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center gap-1.5 px-1 text-[12px] font-semibold text-red">
        <Icon name="hand" size={13} />
        <span className="truncate">{first.todo.title}</span>
        <span className="flex-1" />
        {asks.length > 1 && <span className="shrink-0">还有 {asks.length - 1} 件</span>}
      </div>
      <InteractionCard ix={first.ix} />
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
  const rank: Partial<Record<TodoStatus, number>> = { needs_you: 0, working: 1, not_started: 1, unread: 2 };
  const going = mine
    .map((t) => ({ t, st: statusOf(t, lookup, plans) }))
    .filter((x) => rank[x.st] !== undefined)
    .sort((a, b) => rank[a.st]! - rank[b.st]! || b.t.created_at.localeCompare(a.t.created_at));
  const pill = "inline-flex h-8 max-w-[240px] shrink-0 items-center gap-2 whitespace-nowrap rounded-full px-3 text-[12.5px] font-semibold";
  return (
    <div className="flex min-w-0 items-center gap-2">
      <button
        type="button"
        data-testid="task-dock-board"
        onClick={() => navigate({ name: "todos" })}
        className="inline-flex h-8 shrink-0 items-center whitespace-nowrap rounded-full bg-field px-3 text-[12.5px] font-medium text-ink-2 hover:bg-hover"
      >
        全部 {mine.length + agents.length}
      </button>
      {(going.length > 0 || agents.length > 0) && (
        <div className="flex min-w-0 flex-1 gap-2 overflow-x-auto pb-0.5 [scrollbar-width:none]">
          {going.map(({ t, st }) => (
            <button
              key={t.id}
              type="button"
              onClick={() => openTask(t.id)}
              onContextMenu={(e) => {
                if (!isRemovable(st)) return;
                e.preventDefault();
                setMenuFor({ todo: t, el: e.currentTarget });
              }}
              className={cx(pill, st === "needs_you" ? "bg-red-tint text-red" : "bg-field text-ink hover:bg-hover")}
            >
              <TaskMark status={st} size={8} />
              <span className="truncate">{t.title}</span>
              {st === "needs_you" && <span className="shrink-0">等你</span>}
            </button>
          ))}
          {agents.map(({ session: s, state }) => (
            <button
              key={`${s.machine_id}/${s.session_id}`}
              type="button"
              aria-label={`Agent ${sessionTitle(s)}，${state === "needs_you" ? "等你" : "在做"}`}
              onClick={() => openPanel({ kind: "agent", mid: s.machine_id, sid: s.session_id })}
              className={cx(pill, state === "needs_you" ? "bg-red-tint text-red" : "bg-field text-ink hover:bg-hover")}
            >
              <StatusDot state={state} size={8} />
              <Icon name="agent" size={13} className={cx("shrink-0", state !== "needs_you" && "text-ink-2")} />
              <span className="truncate">{sessionTitle(s)}</span>
              {state === "needs_you" && <span className="shrink-0">等你</span>}
            </button>
          ))}
        </div>
      )}
      {menuFor && (
        <Menu
          anchor={menuFor.el}
          side="top"
          onClose={() => setMenuFor(null)}
          items={[{ key: "remove", label: "移除任务…", icon: "trash", danger: true, onSelect: () => removeTaskNow(menuFor.todo) }]}
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
        对「{todo.title}」说
        <button type="button" aria-label="交给 Persona" onClick={() => setRoute(conversation, null)} className="flex size-5 items-center justify-center rounded-full hover:bg-hover">
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
