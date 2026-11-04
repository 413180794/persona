// Beautiful UI Todos View: left list (sorted by status & created_at), right detail, handover dialog.
import { useEffect, useMemo, useState } from "react";
import { api, enc, errorText } from "../lib/api";
import { ago, clock, shortPath, stateOf, tabLabel, uuid } from "../lib/format";
import { launchAgent } from "../lib/new-agent";
import { navigate } from "../lib/router";
import { plainMarkdown } from "../lib/plain-markdown";
import { useAgentSessions } from "../lib/sessions";
import { refresh, refreshTodos, useStore } from "../lib/store";
import {
  createTodo,
  deleteTodo,
  formatAgentSource,
  formatTodoContext,
  makeSessionLookup,
  mostUrgentAgent,
  sortTodos,
  TODO_STATUS_INFO,
  isAutoDone,
  todoStatusLabel,
  todoStatus,
  unlinkAgentFromTodo,
  unlinkSpaceFromTodo,
  updateTodo,
  type Todo,
  type TodoAgent,
  taskNow,
} from "../lib/todos";
import type { Interaction, Launcher, Machine } from "../lib/types";
import { Button, cx, Dialog, IconButton, Menu, StatusDot, toast, useMenu } from "../ui/atoms";
import { Icon } from "../ui/icons";

export function TodosView({
  selectedId,
  onToggleSidebar,
}: {
  selectedId?: string;
  onToggleSidebar?: () => void;
}) {
  const todos = useStore((s) => s.todos);
  const machines = useStore((s) => s.machines);
  const interactions = useStore((s) => s.interactions);
  const allSessions = useAgentSessions();
  const lookup = useMemo(() => makeSessionLookup(allSessions), [allSessions]);

  const [activeId, setActiveId] = useState<string | undefined>(selectedId);
  const [search, setSearch] = useState("");
  const [newOpen, setNewOpen] = useState(false);
  const [handoverOpen, setHandoverOpen] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState(false);
  const [showDone, setShowDone] = useState(false);
  const [showReleased, setShowReleased] = useState(false);

  // Sync selectedId from URL
  useEffect(() => {
    if (selectedId) setActiveId(selectedId);
  }, [selectedId]);

  // Separate uncompleted and completed todos
  const { uncompleted, completed } = useMemo(() => {
    const un: Todo[] = [];
    const comp: Todo[] = [];
    for (const t of todos) {
      if (t.done_at) comp.push(t);
      else un.push(t);
    }
    return {
      uncompleted: sortTodos(un, lookup),
      completed: sortTodos(comp, lookup),
    };
  }, [todos, lookup]);

  // Filter by search query
  const filterTodo = (t: Todo) => {
    if (!search.trim()) return true;
    const q = search.toLowerCase();
    return t.title.toLowerCase().includes(q) || (t.notes ?? "").toLowerCase().includes(q);
  };

  const filteredUncompleted = useMemo(() => uncompleted.filter(filterTodo), [uncompleted, search]);
  const filteredCompleted = useMemo(() => completed.filter(filterTodo), [completed, search]);

  // Active selected todo
  const activeTodo = useMemo(() => {
    if (activeId) {
      const found = todos.find((t) => t.id === activeId);
      if (found) return found;
    }
    return filteredUncompleted[0] ?? filteredCompleted[0];
  }, [activeId, todos, filteredUncompleted, filteredCompleted]);

  // Active agents of current selected todo
  const activeAgents = useMemo(() => {
    return (activeTodo?.agents ?? []).filter((a) => !a.released_at);
  }, [activeTodo]);

  const releasedAgents = useMemo(() => {
    return (activeTodo?.agents ?? []).filter((a) => a.released_at);
  }, [activeTodo]);

  const toggleDone = async (todo: Todo) => {
    try {
      await updateTodo(todo.id, { done: !todo.done_at });
      await refreshTodos();
      await refresh();
      toast(todo.done_at ? "已重新打开待办" : "已完成待办", "ok");
    } catch (e) {
      toast(`操作失败：${errorText(e)}`, "error");
    }
  };

  const handleDelete = async () => {
    if (!activeTodo) return;
    try {
      await deleteTodo(activeTodo.id);
      await refreshTodos();
      await refresh();
      setDeleteConfirm(false);
      setActiveId(undefined);
      toast("已删除待办", "ok");
    } catch (e) {
      toast(`删除失败：${errorText(e)}`, "error");
    }
  };

  return (
    <div className="flex h-full flex-col bg-surface">
      {/* Top Header */}
      <header className="flex h-13 shrink-0 items-center justify-between border-b border-line bg-surface px-4 sm:px-6">
        <div className="flex items-center gap-3">
          {onToggleSidebar && <IconButton icon="sidebar" label="侧边栏" onClick={onToggleSidebar} className="lg:hidden" />}
          <div className="flex items-baseline gap-2">
            <h1 className="text-[16px] font-semibold text-ink">待办</h1>
            <span className="text-[12px] text-ink-3">
              没完成 {uncompleted.length} · 已完成 {completed.length}
            </span>
          </div>
        </div>
        <Button tone="secondary" icon="plus" onClick={() => setNewOpen(true)} className="h-8 text-[12.5px]">
          新建待办
        </Button>
      </header>

      {/* Main Split Layout: Left List & Right Detail */}
      <div className="flex min-h-0 flex-1">
        {/* Left Column: Todo List */}
        <div className="flex w-full shrink-0 flex-col border-r border-line md:w-[380px] lg:w-[400px]">
          {/* Search Bar */}
          <div className="p-3">
            <div className="flex h-8 items-center gap-2 rounded-control bg-inset px-2.5 shadow-hairline">
              <Icon name="search" size={13} stroke={2} className="text-ink-3" />
              <input
                type="text"
                placeholder="搜索待办"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="w-full bg-transparent text-[13px] text-ink outline-none placeholder:text-ink-3"
              />
              {search && (
                <button type="button" onClick={() => setSearch("")} className="text-ink-3 hover:text-ink">
                  <Icon name="x" size={12} stroke={2.5} />
                </button>
              )}
            </div>
          </div>

          {/* List Content */}
          <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-4">
            {filteredUncompleted.length === 0 && filteredCompleted.length === 0 ? (
              <div className="py-12 text-center text-[13px] text-ink-3">
                {search ? "没有找到符合条件的待办" : "暂无待办，点击上方新建"}
              </div>
            ) : (
              <div className="flex flex-col gap-1">
                {filteredUncompleted.map((todo) => (
                  <TodoListItem
                    key={todo.id}
                    todo={todo}
                    lookup={lookup}
                    interactions={interactions}
                    selected={activeTodo?.id === todo.id}
                    onSelect={() => {
                      setActiveId(todo.id);
                      navigate(`/todos?id=${encodeURIComponent(todo.id)}`);
                    }}
                    onToggleDone={() => toggleDone(todo)}
                  />
                ))}

                {/* Completed Fold */}
                {filteredCompleted.length > 0 && (
                  <div className="mt-2 border-t border-line/60 pt-2">
                    <button
                      type="button"
                      onClick={() => setShowDone((v) => !v)}
                      className="flex h-8 w-full items-center justify-between rounded-control px-2.5 text-[12px] text-ink-3 transition-colors hover:bg-hover hover:text-ink-2"
                    >
                      <span>已完成 · {filteredCompleted.length}</span>
                      <Icon
                        name="chevron"
                        size={12}
                        className={cx("transition-transform duration-200", showDone ? "rotate-180" : "")}
                      />
                    </button>
                    {showDone && (
                      <div className="flex flex-col gap-1 pt-1">
                        {filteredCompleted.map((todo) => (
                          <TodoListItem
                            key={todo.id}
                            todo={todo}
                            lookup={lookup}
                            interactions={interactions}
                            selected={activeTodo?.id === todo.id}
                            onSelect={() => {
                              setActiveId(todo.id);
                              navigate(`/todos?id=${encodeURIComponent(todo.id)}`);
                            }}
                            onToggleDone={() => toggleDone(todo)}
                          />
                        ))}
                      </div>
                    )}
                  </div>
                )}
              </div>
            )}
          </div>
        </div>

        {/* Right Column: Todo Detail */}
        <div className="hidden min-h-0 flex-1 flex-col overflow-y-auto md:flex">
          {activeTodo ? (
            <TodoDetail
              key={activeTodo.id}
              todo={activeTodo}
              activeAgents={activeAgents}
              releasedAgents={releasedAgents}
              lookup={lookup}
              machines={machines}
              interactions={interactions}
              showReleased={showReleased}
              onToggleReleased={() => setShowReleased((v) => !v)}
              onToggleDone={() => toggleDone(activeTodo)}
              onOpenHandover={() => setHandoverOpen(true)}
              onDeleteClick={() => setDeleteConfirm(true)}
            />
          ) : (
            <div className="flex flex-1 items-center justify-center text-[13px] text-ink-3">
              选择左侧待办查看详情
            </div>
          )}
        </div>
      </div>

      {/* Dialogs */}
      <NewTodoDialog open={newOpen} onClose={() => setNewOpen(false)} onCreated={(id) => setActiveId(id)} />

      {activeTodo && (
        <HandoverDialog
          open={handoverOpen}
          todo={activeTodo}
          machines={machines}
          onClose={() => setHandoverOpen(false)}
        />
      )}

      {activeTodo && (
        <Dialog open={deleteConfirm} onClose={() => setDeleteConfirm(false)} title="删除待办">
          <div className="py-2 text-[13.5px] text-ink-2">
            确定要删除「{activeTodo.title}」吗？已关联的 Agent 不受影响。
          </div>
          <div className="mt-4 flex justify-end gap-2">
            <Button tone="secondary" onClick={() => setDeleteConfirm(false)}>
              取消
            </Button>
            <Button tone="danger" onClick={handleDelete}>
              删除
            </Button>
          </div>
        </Dialog>
      )}
    </div>
  );
}

// ── Todo List Item ───────────────────────────────────────────

function TodoListItem({
  todo,
  lookup,
  interactions,
  selected,
  onSelect,
  onToggleDone,
}: {
  todo: Todo;
  lookup: ReturnType<typeof makeSessionLookup>;
  interactions: Interaction[];
  selected: boolean;
  onSelect: () => void;
  onToggleDone: () => void;
}) {
  const status = todoStatus(todo, lookup);
  const statusInfo = TODO_STATUS_INFO[status];
  const urgent = mostUrgentAgent(todo, lookup);
  const urgentNow = urgent?.session && taskNow(urgent.session, stateOf(urgent.session) === "working");
  const activeCount = (todo.agents ?? []).filter((a) => !a.released_at).length;

  const toneClass = {
    red: "text-red",
    green: "text-green",
    amber: "text-orange",
    idle: "text-ink-3",
    off: "text-ink-3",
  }[statusInfo.tone];

  const dotState =
    status === "needs_you" || status === "interrupted"
      ? "needs_you"
      : status === "unread"
        ? "unread"
        : status === "working"
          ? "working"
          : status === "follow_up"
            ? "idle"
            : "offline";

  const urgentIx = urgent
    ? interactions.find((i) => i.machine_id === urgent.agent.machine_id && i.session_id === urgent.agent.session_id)
    : undefined;
  const urgentKicker = urgentIx?.kind === "plan" ? "计划待批准" : urgentIx?.kind === "permission" ? "需要授权" : urgentIx?.kind === "screen" ? "终端对话框" : "在问你";

  return (
    <div
      onClick={onSelect}
      className={cx(
        "group/item flex cursor-pointer gap-2.5 rounded-control p-2.5 transition-colors",
        selected ? "bg-accent-tint/70" : "hover:bg-hover",
      )}
    >
      {/* Checkbox button */}
      <button
        type="button"
        aria-label={todo.done_at ? "重新打开待办" : "完成待办"}
        onClick={(e) => {
          e.stopPropagation();
          onToggleDone();
        }}
        className={cx(
          "mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full border transition-colors",
          todo.done_at
            ? "border-accent bg-accent text-white"
            : "border-line-strong hover:border-accent hover:text-accent text-transparent",
        )}
      >
        <Icon name="check" size={10} stroke={2.6} />
      </button>

      {/* Body */}
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-2">
          <span className={cx("truncate text-[13.5px] font-semibold text-ink", todo.done_at && "line-through text-ink-3 font-normal")}>
            {todo.title}
          </span>
          <span className="shrink-0 text-[11px] text-ink-3">{ago(todo.created_at)}</span>
        </div>

        {/* Status Line */}
        <div className="mt-1 flex items-center gap-1.5 text-[12px]">
          {!todo.done_at && <StatusDot state={dotState} size={6} />}
          <span className={cx("font-medium", toneClass)}>{todoStatusLabel(todo, status)}</span>
          {!todo.done_at && activeCount > 0 && (
            <>
              <span className="text-ink-3">·</span>
              <span className="text-ink-3">{activeCount} 个 Agent</span>
            </>
          )}
          {!todo.done_at && activeCount === 0 && (
            <span className="text-ink-3">· 还没交给 Agent</span>
          )}
        </div>

        {/* Why the hub closed it */}
        {isAutoDone(todo) && todo.done_reason && (
          <div className="mt-1 truncate text-[11.5px] text-ink-3">{todo.done_reason}</div>
        )}

        {/* Most Urgent Agent Line */}
        {!todo.done_at && urgent?.session && urgentNow && (
          <div className="mt-1 truncate text-[11.5px] text-ink-3">
            {urgent.status === "needs_you" && (
              <span className="font-medium text-red">
                {urgent.agent.title || urgent.session.title || "Agent"} {urgentKicker}：
              </span>
            )}
            {urgentNow.kind === "words" ? plainMarkdown(urgentNow.text) : urgentNow.text}
          </div>
        )}
      </div>
    </div>
  );
}

// ── Todo Detail ──────────────────────────────────────────────

function TodoDetail({
  todo,
  activeAgents,
  releasedAgents,
  lookup,
  machines,
  interactions,
  showReleased,
  onToggleReleased,
  onToggleDone,
  onOpenHandover,
  onDeleteClick,
}: {
  todo: Todo;
  activeAgents: TodoAgent[];
  releasedAgents: TodoAgent[];
  lookup: ReturnType<typeof makeSessionLookup>;
  machines: Machine[];
  interactions: Interaction[];
  showReleased: boolean;
  onToggleReleased: () => void;
  onToggleDone: () => void;
  onOpenHandover: () => void;
  onDeleteClick: () => void;
}) {
  const [title, setTitle] = useState(todo.title);
  const [notes, setNotes] = useState(todo.notes ?? "");
  const [savingTitle, setSavingTitle] = useState(false);
  const [savingNotes, setSavingNotes] = useState(false);
  const menu = useMenu();
  const allSessions = useAgentSessions();
  const spacesSupported = useStore((s) => s.spacesSupported);

  const hasLiveAgent = (mid: string, spaceName: string) => {
    return allSessions.some(
      (s) =>
        s.machine_id === mid &&
        (s.space_name || s.space_id) === spaceName &&
        stateOf(s) !== "offline" &&
        machines.some((m) => m.id === mid && m.online),
    );
  };

  const removeSpace = async (mid: string, space: string) => {
    try {
      await unlinkSpaceFromTodo(todo.id, mid, space);
      await refreshTodos();
      await refresh();
      toast("已移出项目", "ok");
    } catch (e) {
      toast(`移出失败：${errorText(e)}`, "error");
    }
  };

  useEffect(() => {
    setTitle(todo.title);
    setNotes(todo.notes ?? "");
  }, [todo]);

  const saveTitle = async () => {
    if (!title.trim() || title.trim() === todo.title) {
      setTitle(todo.title);
      return;
    }
    setSavingTitle(true);
    try {
      await updateTodo(todo.id, { title: title.trim() });
      await refreshTodos();
    } catch (e) {
      toast(`保存标题失败：${errorText(e)}`, "error");
    } finally {
      setSavingTitle(false);
    }
  };

  const saveNotes = async () => {
    if ((notes.trim() || undefined) === (todo.notes?.trim() || undefined)) return;
    setSavingNotes(true);
    try {
      await updateTodo(todo.id, { notes: notes.trim() });
      await refreshTodos();
    } catch (e) {
      toast(`保存说明失败：${errorText(e)}`, "error");
    } finally {
      setSavingNotes(false);
    }
  };

  const removeFailedAgent = async (agent: TodoAgent) => {
    try {
      await unlinkAgentFromTodo(todo.id, agent.machine_id, agent.session_id);
      await refreshTodos();
      await refresh();
      toast("已移除启动失败的 Agent", "ok");
    } catch (e) {
      toast(`移除失败：${errorText(e)}`, "error");
    }
  };

  const status = todoStatus(todo, lookup);
  const statusInfo = TODO_STATUS_INFO[status];
  const toneClass = {
    red: "bg-red-tint text-red",
    green: "bg-green-tint text-green",
    amber: "bg-orange-tint text-orange",
    idle: "bg-hover text-ink-2",
    off: "bg-hover text-ink-3",
  }[statusInfo.tone];

  const dotState =
    status === "needs_you" || status === "interrupted"
      ? "needs_you"
      : status === "unread"
        ? "unread"
        : status === "working"
          ? "working"
          : status === "follow_up"
            ? "idle"
            : "offline";

  return (
    <div className="flex min-h-full flex-col p-6 sm:p-8">
      {/* Title Row with ⋯ menu */}
      <div className="flex items-center justify-between gap-3">
        <input
          type="text"
          value={title}
          disabled={savingTitle}
          onChange={(e) => setTitle(e.target.value)}
          onBlur={saveTitle}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.currentTarget.blur();
            }
          }}
          placeholder="待办标题"
          className="min-w-0 flex-1 bg-transparent text-[22px] font-bold text-ink outline-none transition-colors focus:border-b focus:border-accent"
        />
        <IconButton icon="more" label="更多选项" onClick={menu.open} active={Boolean(menu.anchor)} />
        {menu.anchor && (
          <Menu
            anchor={menu.anchor}
            onClose={menu.close}
            align="right"
            items={[
              {
                key: "delete",
                label: "删除待办…",
                icon: "x" as const,
                danger: true,
                onSelect: onDeleteClick,
              },
            ]}
          />
        )}
      </div>

      {/* Meta Line */}
      <div className="mt-2.5 flex flex-wrap items-center gap-2.5 text-[12.5px] text-ink-2">
        <span className={cx("inline-flex h-5.5 items-center gap-1.5 rounded-md px-2 text-[12px] font-semibold", toneClass)}>
          <StatusDot state={dotState} size={6} />
          {todoStatusLabel(todo, status)}
        </span>
        <span className="text-ink-3">·</span>
        <span>{activeAgents.length} 个 Agent</span>
        <span className="text-ink-3">·</span>
        <span>{clock(todo.created_at)} 创建</span>
      </div>
      {isAutoDone(todo) && (
        <p className="mt-2 text-[12.5px] leading-[1.6] text-ink-3">
          Agent 停下后判断已经做完，自动标成了完成{todo.done_reason ? `：${todo.done_reason}` : ""}。没做完就点「重新打开」。
        </p>
      )}

      {/* Notes / Description */}
      <div className="mt-4">
        <div className="relative rounded-control bg-inset p-3.5 shadow-hairline">
          <textarea
            rows={3}
            value={notes}
            disabled={savingNotes}
            onChange={(e) => setNotes(e.target.value)}
            onBlur={saveNotes}
            placeholder="填写待办说明，交给 Agent 时作为上下文一起发出…"
            className="w-full resize-none bg-transparent text-[13.5px] leading-[1.65] text-ink outline-none placeholder:text-ink-3"
          />
        </div>
      </div>

      {/* Spaces Section */}
      {spacesSupported !== false && todo.spaces && todo.spaces.length > 0 && (
        <div className="mt-6">
          <div className="mb-2 text-[11.5px] font-medium tracking-[0.04em] text-ink-3 uppercase">
            项目 · {todo.spaces.length}
          </div>
          <div className="flex flex-col gap-2">
            {todo.spaces.map((sp) => {
              const mName = machines.find((m) => m.id === sp.machine_id)?.name || sp.machine_id;
              const live = hasLiveAgent(sp.machine_id, sp.space);
              return (
                <div
                  key={`${sp.machine_id}/${sp.space}`}
                  className="flex items-center justify-between gap-3 rounded-control border border-line bg-canvas p-3"
                >
                  <div className="flex min-w-0 items-center gap-2">
                    <span className="truncate text-[13px] font-semibold text-ink">
                      {sp.space} · {mName}
                    </span>
                    {!live && (
                      <span className="shrink-0 rounded bg-hover px-1.5 py-0.5 text-[10.5px] text-ink-3">
                        当前没有 Agent
                      </span>
                    )}
                  </div>
                  <Button
                    tone="quiet"
                    className="shrink-0 text-ink-3 hover:bg-red-tint/50 hover:text-red whitespace-nowrap px-2 py-1 text-[12px]"
                    onClick={() => void removeSpace(sp.machine_id, sp.space)}
                  >
                    移出
                  </Button>
                </div>
              );
            })}
          </div>
        </div>
      )}

      {/* Current Agents */}
      <div className="mt-6 flex-1">
        <div className="mb-2 text-[11.5px] font-medium tracking-[0.04em] text-ink-3 uppercase">
          AGENTS · {activeAgents.length}
        </div>

        {activeAgents.length === 0 ? (
          <div className="rounded-control border border-dashed border-line p-6 text-center text-[13px] text-ink-3">
            还没把这件事交给 Agent 执行，点击下方按钮开始
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            {activeAgents.map((agent) => {
              const session = lookup(agent.machine_id, agent.session_id);
              const st = session ? stateOf(session) : "offline";
              const now = taskNow(session, st === "working");
              const isExpired = !session && !agent.start_error;
              const displayTitle = session?.title || agent.title || "未命名会话";

              // 电脑 · 空间 › tab（电脑名用 machines 里的显示名）
              const mName = machines.find((m) => m.id === agent.machine_id)?.name || agent.machine_id;
              const spaceTab = session ? [session.space_name, tabLabel(session)].filter(Boolean).join(" › ") : "";
              const displaySub = spaceTab
                ? `${mName} · ${spaceTab}`
                : `${mName} · ${shortPath(session?.cwd || agent.cwd)}`;

              const ix = interactions.find((i) => i.machine_id === agent.machine_id && i.session_id === agent.session_id);
              const kicker = ix?.kind === "plan" ? "计划待批准" : ix?.kind === "permission" ? "需要授权" : ix?.kind === "screen" ? "终端对话框" : "在问你";
              const sourceLabel = formatAgentSource(agent, todo.spaces, lookup);

              return (
                <div
                  key={`${agent.machine_id}/${agent.session_id}`}
                  onClick={() => {
                    if (session) {
                      navigate({ name: "session", mid: agent.machine_id, sid: agent.session_id });
                    }
                  }}
                  className={cx(
                    "flex items-start gap-3 rounded-control border border-line bg-canvas p-3 transition-colors",
                    session ? "cursor-pointer hover:bg-hover" : "",
                  )}
                >
                  <div className="mt-1">
                    <StatusDot state={agent.start_error ? "offline" : isExpired ? "offline" : st} size={7} />
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-[13.5px] font-semibold text-ink">{displayTitle}</span>
                      {sourceLabel && (
                        <span className="shrink-0 rounded bg-hover px-1.5 py-0.5 text-[10.5px] text-ink-3">
                          {sourceLabel}
                        </span>
                      )}
                      {isExpired && (
                        <span className="rounded bg-hover px-1.5 py-0.5 text-[10.5px] text-ink-3">已结束</span>
                      )}
                    </div>
                    <div className="mt-0.5 truncate text-[12px] text-ink-3">{displaySub}</div>

                    {/* Error / Last Text */}
                    {agent.start_error ? (
                      <div className="mt-1.5 flex items-center justify-between gap-2 rounded bg-red-tint/50 px-2 py-1 text-[12px] text-red">
                        <span className="truncate">启动失败：{agent.start_error}</span>
                        <button
                          type="button"
                          onClick={(e) => {
                            e.stopPropagation();
                            removeFailedAgent(agent);
                          }}
                          className="shrink-0 font-medium hover:underline"
                        >
                          移除
                        </button>
                      </div>
                    ) : now ? (
                      <div className="mt-1 line-clamp-2 text-[12.5px] text-ink-2">
                        {st === "needs_you" && (
                          <span className="font-medium text-red">{kicker}：</span>
                        )}
                        {now.kind === "words" ? plainMarkdown(now.text) : now.text}
                      </div>
                    ) : null}
                  </div>
                  <div className="shrink-0 text-[11px] text-ink-3">
                    {ago(session?.updated_at || agent.linked_at)}
                  </div>
                </div>
              );
            })}
          </div>
        )}

        {/* Released Agents (Participated) */}
        {releasedAgents.length > 0 && (
          <div className="mt-4">
            <button
              type="button"
              onClick={onToggleReleased}
              className="flex items-center gap-1 text-[12px] text-ink-3 hover:text-ink-2"
            >
              <Icon
                name="chevron"
                size={11}
                className={cx("transition-transform duration-200", showReleased ? "rotate-180" : "")}
              />
              <span>参与过 · {releasedAgents.length}</span>
            </button>
            {showReleased && (
              <div className="mt-2 flex flex-col gap-1.5">
                {releasedAgents.map((agent) => {
                  const mName = machines.find((m) => m.id === agent.machine_id)?.name || agent.machine_id;
                  const displaySub = `${mName} · ${shortPath(agent.cwd)}`;
                  return (
                    <div
                      key={`${agent.machine_id}/${agent.session_id}`}
                      className="flex items-center justify-between rounded-control border border-line/60 bg-hover/40 px-3 py-2 text-[12px] text-ink-3"
                    >
                      <div className="min-w-0 flex-1">
                        <span className="truncate font-medium text-ink-2">{agent.title || agent.session_id}</span>
                        <div className="text-[11px] text-ink-3">{displaySub}</div>
                      </div>
                      <span className="shrink-0 text-[11px]">{ago(agent.released_at ?? undefined)} 离开</span>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        )}
      </div>

      {/* Bottom Action Bar */}
      <div className="mt-8 flex flex-wrap items-center gap-2.5 border-t border-line pt-4">
        <Button tone="accent" icon="hand" onClick={onOpenHandover} className="h-8.5 px-3.5 text-[13px]">
          交给 Agent
        </Button>
        <Button tone="secondary" icon="check" onClick={onToggleDone} className="h-8.5 px-3 text-[13px]">
          {todo.done_at ? "重新打开" : "完成"}
        </Button>
      </div>
    </div>
  );
}

// ── New Todo Dialog ──────────────────────────────────────────

function NewTodoDialog({
  open,
  onClose,
  onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: (id: string) => void;
}) {
  const [title, setTitle] = useState("");
  const [notes, setNotes] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (open) {
      setTitle("");
      setNotes("");
    }
  }, [open]);

  const submit = async (e?: React.FormEvent) => {
    e?.preventDefault();
    if (!title.trim()) return;
    setBusy(true);
    try {
      const todo = await createTodo(title, notes);
      await refreshTodos();
      await refresh();
      toast("已创建待办", "ok");
      onClose();
      onCreated(todo.id);
      navigate(`/todos?id=${encodeURIComponent(todo.id)}`);
    } catch (err) {
      toast(`创建失败：${errorText(err)}`, "error");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} title="新建待办">
      <form onSubmit={submit} className="flex flex-col gap-3.5">
        <div>
          <label className="mb-1 block text-[12px] font-medium text-ink-2">标题</label>
          <input
            type="text"
            required
            autoFocus
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="待办要做什么…"
            className="w-full rounded-control border border-line bg-canvas px-3 py-1.5 text-[13.5px] text-ink outline-none focus:border-accent"
          />
        </div>

        <div>
          <label className="mb-1 block text-[12px] font-medium text-ink-2">说明（选填）</label>
          <textarea
            rows={3}
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            placeholder="详细内容，交给 Agent 时作为上下文一起发出…"
            className="w-full resize-y rounded-control border border-line bg-canvas px-3 py-1.5 text-[13px] text-ink outline-none focus:border-accent"
          />
        </div>

        <div className="mt-2 flex justify-end gap-2">
          <Button tone="secondary" onClick={onClose}>
            取消
          </Button>
          <Button tone="accent" type="submit" disabled={!title.trim() || busy} busy={busy}>
            创建
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// ── Handover to Agent Dialog ─────────────────────────────────

function HandoverDialog({
  open,
  todo,
  machines,
  onClose,
}: {
  open: boolean;
  todo: Todo;
  machines: Machine[];
  onClose: () => void;
}) {
  const [tab, setTab] = useState<"new" | "existing">("new");
  const [prompt, setPrompt] = useState("");
  const [busy, setBusy] = useState(false);

  // For "new":
  const onlineMachines = useMemo(() => machines.filter((m) => m.online), [machines]);
  const [mid, setMid] = useState(() => onlineMachines[0]?.id ?? "");
  const [cwd, setCwd] = useState("");
  const [launchers, setLaunchers] = useState<Launcher[]>([]);
  const [launcher, setLauncher] = useState("");

  // For "existing":
  const allSessions = useAgentSessions();
  const liveSessions = useMemo(() => {
    return allSessions.filter((s) => !s.assistant && stateOf(s) !== "offline");
  }, [allSessions]);
  const [selectedSid, setSelectedSid] = useState(() => liveSessions[0]?.session_id ?? "");

  useEffect(() => {
    if (open) {
      setPrompt("");
      if (onlineMachines[0] && (!mid || !onlineMachines.some((m) => m.id === mid))) {
        setMid(onlineMachines[0].id);
      }
      if (liveSessions[0]) {
        setSelectedSid(liveSessions[0].session_id);
      }
    }
  }, [open, onlineMachines, liveSessions]);

  useEffect(() => {
    if (!open || !mid) return;
    const controller = new AbortController();
    api<{ launchers: Launcher[] }>(`/machines/${enc(mid)}/launchers`, { signal: controller.signal })
      .then((r) => {
        setLaunchers(r.launchers ?? []);
        if (r.launchers?.[0]) setLauncher(r.launchers[0].name);
      })
      .catch(() => {});
    return () => controller.abort();
  }, [open, mid]);

  const previewCard = useMemo(() => {
    return formatTodoContext(todo.title, todo.notes);
  }, [todo]);

  const submitNew = async () => {
    if (!mid) return;
    setBusy(true);
    try {
      const fullText = [previewCard, prompt.trim()].filter(Boolean).join("\n\n");
      launchAgent({
        machineId: mid,
        cwd: cwd.trim(),
        launcher,
        text: fullText,
        attachments: [],
        todoId: todo.id,
      });
      toast("已启动并交给 Agent", "ok");
      onClose();
    } catch (e) {
      toast(`启动失败：${errorText(e)}`, "error");
    } finally {
      setBusy(false);
    }
  };

  const submitExisting = async () => {
    const s = liveSessions.find((x) => x.session_id === selectedSid);
    if (!s) return;
    setBusy(true);
    try {
      const fullText = [previewCard, prompt.trim()].filter(Boolean).join("\n\n");
      const clientMsgId = uuid();
      await api(`/sessions/${enc(s.machine_id)}/${enc(s.session_id)}/messages`, {
        method: "POST",
        json: {
          text: fullText,
          client_msg_id: clientMsgId,
          todo_id: todo.id,
        },
      });
      await refreshTodos();
      await refresh();
      toast("已发送并交给 Agent", "ok");
      onClose();
      navigate({ name: "session", mid: s.machine_id, sid: s.session_id });
    } catch (e) {
      toast(`发送失败：${errorText(e)}`, "error");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} title="交给 Agent" width={520}>
      <div className="flex flex-col gap-4">
        {/* Tab Switcher */}
        <div className="flex rounded-control bg-field p-0.5">
          <button
            type="button"
            onClick={() => setTab("new")}
            className={cx(
              "flex-1 rounded-[6px] py-1 text-[13px] font-medium transition-colors",
              tab === "new" ? "bg-surface text-ink shadow-hairline" : "text-ink-3 hover:text-ink-2",
            )}
          >
            新开一个 Agent
          </button>
          <button
            type="button"
            onClick={() => setTab("existing")}
            className={cx(
              "flex-1 rounded-[6px] py-1 text-[13px] font-medium transition-colors",
              tab === "existing" ? "bg-surface text-ink shadow-hairline" : "text-ink-3 hover:text-ink-2",
            )}
          >
            交给已有的 Agent
          </button>
        </div>

        {/* Tab 1: New Agent */}
        {tab === "new" && (
          <div className="flex flex-col gap-3">
            <div>
              <label className="mb-1 block text-[12px] font-medium text-ink-2">电脑</label>
              <select
                value={mid}
                onChange={(e) => setMid(e.target.value)}
                className="w-full rounded-control border border-line bg-canvas px-2.5 py-1.5 text-[13px] text-ink outline-none"
              >
                {onlineMachines.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.name}
                  </option>
                ))}
              </select>
            </div>

            <div>
              <label className="mb-1 block text-[12px] font-medium text-ink-2">工作目录（选填）</label>
              <input
                type="text"
                placeholder="~/project 或留空"
                value={cwd}
                onChange={(e) => setCwd(e.target.value)}
                className="w-full rounded-control border border-line bg-canvas px-2.5 py-1.5 text-[13px] text-ink outline-none focus:border-accent"
              />
            </div>

            {launchers.length > 0 && (
              <div>
                <label className="mb-1 block text-[12px] font-medium text-ink-2">启动器</label>
                <select
                  value={launcher}
                  onChange={(e) => setLauncher(e.target.value)}
                  className="w-full rounded-control border border-line bg-canvas px-2.5 py-1.5 text-[13px] text-ink outline-none"
                >
                  {launchers.map((l) => (
                    <option key={l.name} value={l.name}>
                      {l.name}
                    </option>
                  ))}
                </select>
              </div>
            )}
          </div>
        )}

        {/* Tab 2: Existing Agent */}
        {tab === "existing" && (
          <div>
            <label className="mb-1 block text-[12px] font-medium text-ink-2">选择 Agent</label>
            {liveSessions.length === 0 ? (
              <div className="py-6 text-center text-[12.5px] text-ink-3">当前没有可用的在线 Agent</div>
            ) : (
              <div className="max-h-44 overflow-y-auto rounded-control border border-line p-1">
                {liveSessions.map((s) => {
                  const isSel = s.session_id === selectedSid;
                  const st = stateOf(s);
                  return (
                    <div
                      key={s.session_id}
                      onClick={() => setSelectedSid(s.session_id)}
                      className={cx(
                        "flex cursor-pointer items-center justify-between gap-2 rounded px-2.5 py-1.5 text-[12.5px] transition-colors",
                        isSel ? "bg-accent text-white" : "hover:bg-hover text-ink",
                      )}
                    >
                      <div className="flex min-w-0 items-center gap-2">
                        <StatusDot state={st} size={6} />
                        <span className="truncate font-medium">{s.title || s.cwd || "未命名"}</span>
                        <span className={cx("text-[11px]", isSel ? "text-white/80" : "text-ink-3")}>
                          {s.space_name || s.machine_id}
                        </span>
                      </div>
                      {isSel && <Icon name="check" size={13} stroke={2.5} />}
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        )}

        {/* Prompt Input */}
        <div>
          <label className="mb-1 block text-[12px] font-medium text-ink-2">要它做什么</label>
          <textarea
            rows={2}
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder="例如：请把网络层的测试补齐，覆盖请求重试和超时…"
            className="w-full resize-none rounded-control border border-line bg-canvas px-3 py-1.5 text-[13px] text-ink outline-none focus:border-accent"
          />
        </div>

        {/* Context Preview Card */}
        <div>
          <label className="mb-1 block text-[11px] font-medium text-ink-3">发送内容预览</label>
          <div className="flex flex-col gap-1 rounded-control bg-field p-2.5 shadow-hairline">
            <div className="rounded border-l-2 border-accent bg-surface px-2.5 py-1.5 text-left shadow-hairline">
              <div className="text-[11.5px] font-medium text-ink-2">待办 · {todo.title}</div>
              {todo.notes && <div className="mt-0.5 line-clamp-2 text-[12px] text-ink-3">{todo.notes}</div>}
            </div>
            {prompt.trim() && (
              <div className="mt-1 px-1 text-[13px] whitespace-pre-wrap text-ink">{prompt.trim()}</div>
            )}
          </div>
        </div>

        {/* Footer Buttons */}
        <div className="mt-2 flex justify-end gap-2">
          <Button tone="secondary" onClick={onClose}>
            取消
          </Button>
          {tab === "new" ? (
            <Button tone="accent" onClick={submitNew} disabled={!mid || busy} busy={busy}>
              启动并交给 Agent
            </Button>
          ) : (
            <Button
              tone="accent"
              onClick={submitExisting}
              disabled={!selectedSid || busy || liveSessions.length === 0}
              busy={busy}
            >
              发送并交给 Agent
            </Button>
          )}
        </div>
      </div>
    </Dialog>
  );
}
