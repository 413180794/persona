// A task's detail beside the assistant's conversation (docs/unified-entry.md「任务详情」; iOS TaskDetailSheet): its
// agent's own chat — everything the Agents page does works here — and, with several agents, a switch between them.
import { useState } from "react";
import { TODO_STATUS_INFO } from "../../lib/todos";
import { useStore } from "../../lib/store";
import { cx, StatusDot } from "../../ui/atoms";
import { ChatView } from "../chat/ChatView";
import { PanelHeader } from "../SidePanel";
import { place, useTaskState, useTodo } from "./Tasks";
import { stateOf } from "../../lib/format";

export function TaskPanel({ todoId, onClose }: { todoId: string; onClose: () => void }) {
  const todo = useTodo(todoId);
  const st = useTaskState(todo);
  const sessions = useStore((s) => s.sessions);
  const [picked, setPicked] = useState<string | null>(null);
  if (!todo || !st) {
    return (
      <>
        <PanelHeader icon="todo" title="任务" onClose={onClose} />
        <div className="p-6 text-[13px] text-ink-2">没有这个任务了。</div>
      </>
    );
  }
  const agents = (todo.agents ?? []).filter((a) => !a.released_at);
  const lead = st.lead;
  const chosen = agents.find((a) => `${a.machine_id}/${a.session_id}` === picked) ?? (lead ? agents.find((a) => a.session_id === lead.session_id) : agents[0]);
  const info = TODO_STATUS_INFO[st.status];
  return (
    <>
      <PanelHeader icon="todo" title={todo.title} subtitle={`${info.label} · ${place(todo, lead)}`} onClose={onClose} />
      {agents.length > 1 && (
        <div className="flex gap-2 overflow-x-auto border-b border-line px-3 py-2 [scrollbar-width:none]">
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
        ) : (
          <div className="p-6 text-[13px] text-ink-2">{todo.agents?.find((a) => a.start_error)?.start_error ? `Agent 没开起来：${todo.agents.find((a) => a.start_error)!.start_error}` : "正在开 Agent，开好以后这里就是它的对话。"}</div>
        )}
      </div>
    </>
  );
}
