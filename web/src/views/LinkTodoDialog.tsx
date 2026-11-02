// Dialog to link an agent session to an uncompleted todo
import { useState } from "react";
import { errorText } from "../lib/api";
import { refresh, refreshTodos, useStore } from "../lib/store";
import { linkAgentToTodo, unlinkAgentFromTodo, type Todo } from "../lib/todos";
import type { Session } from "../lib/types";
import { Button, cx, Dialog, toast } from "../ui/atoms";
import { Icon } from "../ui/icons";

export function LinkTodoDialog({
  open,
  session,
  onClose,
}: {
  open: boolean;
  session?: Session;
  onClose: () => void;
}) {
  const todos = useStore((s) => s.todos);
  const uncompleted = todos.filter((t) => !t.done_at);
  const [busy, setBusy] = useState(false);

  if (!session) return null;

  const currentTodoId = session.todo?.id;

  const handleSelect = async (todo: Todo) => {
    setBusy(true);
    try {
      await linkAgentToTodo(todo.id, session.machine_id, session.session_id);
      await refreshTodos();
      await refresh();
      toast(`已关联到待办「${todo.title}」`, "ok");
      onClose();
    } catch (e) {
      toast(`关联失败：${errorText(e)}`, "error");
    } finally {
      setBusy(false);
    }
  };

  const handleUnlink = async () => {
    if (!currentTodoId) return;
    setBusy(true);
    try {
      await unlinkAgentFromTodo(currentTodoId, session.machine_id, session.session_id);
      await refreshTodos();
      await refresh();
      toast("已解除关联", "ok");
      onClose();
    } catch (e) {
      toast(`解除关联失败：${errorText(e)}`, "error");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} title="关联到待办" width={420}>
      <div className="flex flex-col gap-3">
        <div className="text-[12.5px] text-ink-3">
          选择一个待办，把「{session.title || session.session_id}」挂到该待办上：
        </div>

        {uncompleted.length === 0 ? (
          <div className="py-6 text-center text-[13px] text-ink-3">暂无未完成的待办</div>
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
                    "flex w-full items-center justify-between gap-2 rounded px-2.5 py-2 text-left text-[13px] transition-colors",
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

        <div className="mt-2 flex justify-between gap-2">
          {currentTodoId ? (
            <Button tone="quiet" onClick={handleUnlink} disabled={busy} className="text-red hover:bg-red-tint/50">
              解除关联
            </Button>
          ) : (
            <div />
          )}
          <Button tone="secondary" onClick={onClose}>
            取消
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
