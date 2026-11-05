// A task's detail beside the assistant's conversation (docs/unified-entry.md「任务详情」; iOS TaskDetailSheet): its
// agent's own chat — everything the Agents page does works here — and, with several agents, a switch between them.
import { useState } from "react";
import { isRemovable, TODO_STATUS_INFO } from "../../lib/todos";
import { useStore } from "../../lib/store";
import { Button, cx, IconButton, Menu, StatusDot, useMenu } from "../../ui/atoms";
import { ChatView } from "../chat/ChatView";
import { PanelHeader } from "../SidePanel";
import { place, removeTaskNow, retryTaskNow, useRetrying, useTaskState, useTodo } from "./Tasks";
import { sessionTitle, stateOf } from "../../lib/format";
import { href, type Route } from "../../lib/router";
import { t } from "../../i18n";

/** The header's 在新窗口打开: the page the panel shows, as its own tab (⌘-click on the link does the same). */
function OpenInWindow({ to }: { to: Route }) {
  return <IconButton icon="external" label={t("common.open_in_new_window")} onClick={() => window.open(href(to), "_blank", "noopener")} />;
}

export function TaskPanel({ todoId, onClose }: { todoId: string; onClose: () => void }) {
  const todo = useTodo(todoId);
  const st = useTaskState(todo);
  const sessions = useStore((s) => s.sessions);
  const [picked, setPicked] = useState<string | null>(null);
  const more = useMenu();
  const retrying = useRetrying(todoId);
  if (!todo || !st) {
    return (
      <>
        <PanelHeader icon="todo" title={t("assistant-taskpanel.title")} onClose={onClose} />
        <div className="p-6 text-[13px] text-ink-2">{t("assistant-taskpanel.gone")}</div>
      </>
    );
  }
  const agents = (todo.agents ?? []).filter((a) => !a.released_at);
  const lead = st.lead;
  const chosen = agents.find((a) => `${a.machine_id}/${a.session_id}` === picked) ?? (lead ? agents.find((a) => a.session_id === lead.session_id) : agents[0]);
  const info = TODO_STATUS_INFO[st.status];
  return (
    <>
      <PanelHeader
        icon="todo"
        title={todo.title}
        subtitle={[info.label, place(todo, lead)].filter(Boolean).join(" · ")}
        actions={
          <>
            <OpenInWindow to={{ name: "todos", id: todo.id }} />
            {isRemovable(st.status) && (
              <>
                <IconButton icon="more" label={t("assistant-taskpanel.more")} onClick={more.open} active={Boolean(more.anchor)} />
                {more.anchor && (
                  <Menu
                    anchor={more.anchor}
                    onClose={more.close}
                    align="right"
                    items={[{ key: "remove", label: t("assistant-taskpanel.remove_ellipsis"), icon: "trash", danger: true, onSelect: () => removeTaskNow(todo) }]}
                  />
                )}
              </>
            )}
          </>
        }
        onClose={onClose}
      />
      {agents.length > 1 && (
        <div className="flex gap-2 overflow-x-auto border-b border-line px-3 py-2 scrollbar-none">
          {agents.map((a) => {
            const s = (sessions[a.machine_id] ?? []).find((x) => x.session_id === a.session_id);
            const key = `${a.machine_id}/${a.session_id}`;
            return (
              <button
                key={key}
                type="button"
                onClick={() => setPicked(key)}
                className={cx("inline-flex h-7 shrink-0 items-center gap-1.5 rounded-full px-3 text-[12px] font-semibold", chosen === a ? "bg-accent-tint text-accent" : "bg-field text-ink-2")}
              >
                {s && <StatusDot state={stateOf(s)} size={6} />}
                {s?.title || a.title || a.agent || a.session_id.slice(0, 8)}
              </button>
            );
          })}
        </div>
      )}
      <div className="flex min-h-0 flex-1 flex-col">
        {chosen ? (
          <ChatView key={`${chosen.machine_id}/${chosen.session_id}`} mid={chosen.machine_id} sid={chosen.session_id} />
        ) : st.failure || todo.agents?.some((a) => a.start_error) ? (
          <div className="flex flex-col items-start gap-3 p-6 text-[13px]">
            <div className="font-semibold text-red">{t("assistant-taskpanel.start_failed")}</div>
            <div className="text-ink-2">{st.failure ?? todo.agents?.find((a) => a.start_error)?.start_error}</div>
            <div className="flex gap-2">
              {st.status === "failed" && (
                <Button icon="retry" busy={retrying} disabled={retrying} onClick={() => retryTaskNow(todo)}>
                  {t("assistant-taskpanel.retry")}
                </Button>
              )}
              <Button icon="trash" onClick={() => removeTaskNow(todo)}>
                {t("assistant-taskpanel.remove")}
              </Button>
            </div>
          </div>
        ) : (
          <div className="p-6 text-[13px] text-ink-2">{t("assistant-taskpanel.starting")}</div>
        )}
      </div>
    </>
  );
}

/** An agent the main conversation's dock shows that no task stands for (#1113; iOS opens it in the task's sheet): its
 * state, title and machine, over its own chat. */
export function AgentPanel({ mid, sid, onClose }: { mid: string; sid: string; onClose: () => void }) {
  const session = useStore((s) => (s.sessions[mid] ?? []).find((x) => x.session_id === sid));
  const machine = useStore((s) => s.machines.find((m) => m.id === mid)?.name ?? mid);
  return (
    <>
      <PanelHeader
        icon="agent"
        title={
          <span className="flex min-w-0 items-center gap-1.5">
            {session && <StatusDot state={stateOf(session)} size={7} />}
            <span className="truncate">{session ? sessionTitle(session) : "Agent"}</span>
          </span>
        }
        subtitle={[machine, session?.agent].filter(Boolean).join(" · ")}
        actions={<OpenInWindow to={{ name: "session", mid, sid }} />}
        onClose={onClose}
      />
      <div className="flex min-h-0 flex-1 flex-col">
        <ChatView key={`${mid}/${sid}`} mid={mid} sid={sid} />
      </div>
    </>
  );
}
