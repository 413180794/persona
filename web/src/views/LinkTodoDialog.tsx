// Dialog to link an agent session or project space to an uncompleted todo
import { useMemo, useState } from "react";
import { errorText } from "../lib/api";
import { refresh, refreshTodos, useStore } from "../lib/store";
import {
  createTodo,
  findTodoForSpace,
  linkAgentToTodo,
  linkSpaceToTodo,
  unlinkAgentFromTodo,
  unlinkSpaceFromTodo,
  type Todo,
} from "../lib/todos";
import type { Session } from "../lib/types";
import { Button, cx, Dialog, toast } from "../ui/atoms";
import { Icon } from "../ui/icons";

export interface LinkTargetSpace {
  machineId: string;
  space: string;
}

export function LinkTodoDialog({
  open,
  session,
  space,
  onClose,
}: {
  open: boolean;
  session?: Session;
  space?: LinkTargetSpace;
  onClose: () => void;
}) {
  const todos = useStore((s) => s.todos);
  const uncompleted = useMemo(() => todos.filter((t) => !t.done_at), [todos]);
  const [busy, setBusy] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newTitle, setNewTitle] = useState("");

  if (!session && !space) return null;

  const currentTodo = space
    ? findTodoForSpace(todos, space.machineId, space.space)
    : session?.todo?.id
      ? todos.find((t) => t.id === session.todo?.id)
      : undefined;

  const currentTodoId = currentTodo?.id;

  const handleSelect = async (todo: Todo) => {
    setBusy(true);
    try {
      if (space) {
        await linkSpaceToTodo(todo.id, space.machineId, space.space);
        await refreshTodos();
        await refresh();
        toast(`已挂到待办「${todo.title}」`, "ok");
      } else if (session) {
        await linkAgentToTodo(todo.id, session.machine_id, session.session_id);
        await refreshTodos();
        await refresh();
        toast(`已关联到待办「${todo.title}」`, "ok");
      }
      onClose();
    } catch (e) {
      toast(`${space ? "挂到待办" : "关联"}失败：${errorText(e)}`, "error");
    } finally {
      setBusy(false);
    }
  };

  const handleUnlink = async () => {
    if (!currentTodoId) return;
    setBusy(true);
    try {
      if (space) {
        await unlinkSpaceFromTodo(currentTodoId, space.machineId, space.space);
        await refreshTodos();
        await refresh();
        toast("已移出待办", "ok");
      } else if (session) {
        await unlinkAgentFromTodo(currentTodoId, session.machine_id, session.session_id);
        await refreshTodos();
        await refresh();
        toast("已解除关联", "ok");
      }
      onClose();
    } catch (e) {
      toast(`${space ? "移出待办" : "解除关联"}失败：${errorText(e)}`, "error");
    } finally {
      setBusy(false);
    }
  };

  const handleCreateAndLink = async () => {
    const trimmed = newTitle.trim();
    if (!trimmed) return;
    setBusy(true);
    try {
      const created = await createTodo(trimmed);
      if (space) {
        await linkSpaceToTodo(created.id, space.machineId, space.space);
        toast(`已新建并挂到待办「${created.title}」`, "ok");
      } else if (session) {
        await linkAgentToTodo(created.id, session.machine_id, session.session_id);
        toast(`已新建并关联到待办「${created.title}」`, "ok");
      }
      await refreshTodos();
      await refresh();
      onClose();
    } catch (e) {
      toast(`创建失败：${errorText(e)}`, "error");
    } finally {
      setBusy(false);
    }
  };

  const dialogTitle = space
    ? currentTodoId
      ? "改挂到其他待办"
      : "挂到待办"
    : currentTodoId
      ? "改关联到其他待办"
      : "关联到待办";

  const description = space
    ? `选择一个未完成的待办，把项目「${space.space}」整组挂上：`
    : `选择一个待办，把「${session?.title || session?.session_id}」挂到该待办上：`;

  return (
    <Dialog open={open} onClose={onClose} title={dialogTitle} width={420}>
      <div className="flex flex-col gap-3">
        <div className="text-[12.5px] text-ink-3">{description}</div>

        {uncompleted.length === 0 ? (
          <div className="py-4 text-center text-[13px] text-ink-3">暂无未完成的待办</div>
        ) : (
          <div className="flex max-h-56 flex-col gap-1 overflow-y-auto rounded-control border border-line p-1">
            {uncompleted.map((t) => {
              const isCurrent = t.id === currentTodoId;
              return (
                <button
                  key={t.id}
                  type="button"
                  disabled={busy}
                  onClick={() => handleSelect(t)}
                  className={cx(
                    "flex w-full items-center justify-between gap-2 rounded px-2.5 py-2 text-left text-[13px] transition-colors whitespace-nowrap",
                    isCurrent ? "bg-accent-tint text-accent font-medium" : "hover:bg-hover text-ink",
                  )}
                >
                  <span className="truncate">{t.title}</span>
                  {isCurrent && <Icon name="check" size={13} stroke={2.5} />}
                </button>
              );
            })}
          </div>
        )}

        {creating ? (
          <div className="flex flex-col gap-2 rounded-control border border-line bg-inset p-2.5">
            <input
              type="text"
              autoFocus
              value={newTitle}
              disabled={busy}
              onChange={(e) => setNewTitle(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  void handleCreateAndLink();
                } else if (e.key === "Escape") {
                  setCreating(false);
                }
              }}
              placeholder="输入新待办标题…"
              className="w-full bg-transparent text-[13px] text-ink outline-none placeholder:text-ink-3"
            />
            <div className="flex items-center justify-end gap-2">
              <Button tone="quiet" className="px-2 py-1 text-[12px] whitespace-nowrap" onClick={() => setCreating(false)} disabled={busy}>
                取消
              </Button>
              <Button tone="primary" className="px-2 py-1 text-[12px] whitespace-nowrap" onClick={() => void handleCreateAndLink()} disabled={busy || !newTitle.trim()}>
                创建并挂上
              </Button>
            </div>
          </div>
        ) : (
          <button
            type="button"
            disabled={busy}
            onClick={() => {
              setNewTitle("");
              setCreating(true);
            }}
            className="flex items-center gap-1.5 rounded-control px-2 py-1.5 text-left text-[12.5px] font-medium text-accent hover:bg-hover whitespace-nowrap"
          >
            <Icon name="plus" size={12} stroke={2.5} />
            <span>新建待办…</span>
          </button>
        )}

        <div className="mt-2 flex justify-between gap-2">
          {currentTodoId ? (
            <Button tone="quiet" onClick={handleUnlink} disabled={busy} className="text-red hover:bg-red-tint/50 whitespace-nowrap">
              {space ? "移出待办" : "解除关联"}
            </Button>
          ) : (
            <div />
          )}
          <Button tone="secondary" onClick={onClose} whitespace-nowrap>
            取消
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
