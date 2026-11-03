// The Agents list's menus (iOS AgentRowMenu / SpaceMenuItems): a row's ⋯ or right click, a space's ⋯ on its header,
// and the confirmation of 全部标为已读. What they do is in lib/agent-actions.ts.
import { flipMute, flipPin, flipSpaceMute, flipSpacePin, handToPersona } from "../lib/agent-actions";
import { isSpaceMuted, unreadOf, type SpaceLike } from "../lib/agent-filter";
import { errorText } from "../lib/api";
import { resumeCommand, sessionCan, sessionDeletable, sessionMenuActions } from "../lib/format";
import { machineWithHub } from "../lib/mesh";
import { isPinned, isSpacePinned } from "../lib/pins";
import { isMuted } from "../lib/session-actions";
import { getState, markSeen, refresh, refreshTodos, setSpacesSupported, useStore } from "../lib/store";
import { findTodoForSpace, isLegacySpacesRouteError, unlinkSpaceFromTodo } from "../lib/todos";
import type { Session } from "../lib/types";
import { Button, copyText, Dialog, Menu, toast, type MenuItem } from "../ui/atoms";

export function AgentRowMenu({ s, anchor, onClose, onRename, onDelete }: { s: Session; anchor: HTMLElement; onClose: () => void; onRename: (s: Session) => void; onDelete: (s: Session) => void }) {
  const pins = useStore((st) => st.pins);
  const mutes = useStore((st) => st.mutes);
  const pinned = isPinned(s, pins);
  const muted = isMuted(s, mutes);
  const items: MenuItem[] = [
    { key: "pin", label: pinned ? "取消置顶" : "置顶", icon: "pin", onSelect: () => void flipPin(s) },
    ...(sessionCan(s, "send") && !s.source_kind ? [{ key: "rename", label: "重命名…", icon: "pencil" as const, onSelect: () => onRename(s) }] : []),
    { key: "mute", label: muted ? "恢复通知" : "屏蔽通知", icon: muted ? "bell" : "bellOff", onSelect: () => void flipMute(s) },
    { key: "jarvis", label: "交给 Persona", icon: "spark", onSelect: () => void handToPersona(s, machineWithHub(s.machine_id, getState().machines)) },
    ...(s.cwd ? [{ key: "cwd", label: "拷贝目录", icon: "folder" as const, onSelect: () => copyText(s.cwd) }] : []),
    ...(sessionMenuActions(s).includes("resume") ? [{ key: "resume", label: "拷贝恢复命令", icon: "terminal" as const, onSelect: () => copyText(resumeCommand(s)) }] : []),
    ...(sessionDeletable(s) ? [{ key: "delete", label: "删除对话…", icon: "trash" as const, danger: true, onSelect: () => onDelete(s) }] : []),
  ];
  return <Menu anchor={anchor} onClose={onClose} align="right" items={items} />;
}

export function SpaceMenu({
  sp,
  anchor,
  onClose,
  onRename,
  onMarkSeen,
  onLinkTodo,
}: {
  sp: SpaceLike;
  anchor: HTMLElement;
  onClose: () => void;
  onRename: (s: Session) => void;
  onMarkSeen: (list: Session[]) => void;
  onLinkTodo?: (sp: SpaceLike) => void;
}) {
  const pins = useStore((st) => st.pins);
  const mutes = useStore((st) => st.mutes);
  const todos = useStore((st) => st.todos);
  const todosSupported = useStore((st) => st.todosSupported);
  const todosLoaded = useStore((st) => st.todosLoaded);
  const spacesSupported = useStore((st) => st.spacesSupported);
  const pinned = isSpacePinned(sp.machineId, sp.name, pins);
  const muted = isSpaceMuted(sp, mutes);
  const unread = unreadOf(sp.sessions);
  const first = sp.sessions[0];

  const supportSpaces = todosLoaded && todosSupported !== false && spacesSupported !== false;
  const linkedTodo = supportSpaces ? findTodoForSpace(todos, sp.machineId, sp.name) : undefined;

  const items: MenuItem[] = [
    ...(unread.length ? [{ key: "seen", label: `全部标为已读（${unread.length}）`, icon: "check" as const, onSelect: () => onMarkSeen(unread) }] : []),
    ...(first?.space_id && !first.source_kind ? [{ key: "rename", label: "重命名空间…", icon: "pencil" as const, onSelect: () => onRename(first) }] : []),
    ...(supportSpaces
      ? linkedTodo
        ? [
            {
              key: "relink-todo",
              label: "改挂到其他待办…",
              icon: "todo" as const,
              onSelect: () => onLinkTodo?.(sp),
            },
            {
              key: "unlink-todo",
              label: "移出待办",
              icon: "x" as const,
              onSelect: async () => {
                try {
                  await unlinkSpaceFromTodo(linkedTodo.id, sp.machineId, sp.name);
                  await refreshTodos();
                  await refresh();
                  toast("已移出待办", "ok");
                } catch (e) {
                  if (isLegacySpacesRouteError(e)) {
                    setSpacesSupported(false);
                  }
                  toast(`移出失败：${errorText(e)}`, "error");
                  void refreshTodos();
                }
              },
            },
          ]
        : [
            {
              key: "link-todo",
              label: "挂到待办…",
              icon: "todo" as const,
              onSelect: () => onLinkTodo?.(sp),
            },
          ]
      : []),
    { key: "pin", label: pinned ? "取消置顶空间" : "置顶空间", icon: "pin", onSelect: () => void flipSpacePin(sp.machineId, sp.name) },
    { key: "mute", label: muted ? "取消屏蔽空间" : "屏蔽这个空间", icon: muted ? "bell" : "bellOff", onSelect: () => void flipSpaceMute(sp.machineId, sp.name) },
  ];
  return <Menu anchor={anchor} onClose={onClose} align="right" header={`空间 ${sp.name}`} items={items} />;
}

/** 全部标为已读, asked first: it changes what the other devices show too. */
export function MarkSeenDialog({ list, onClose, onDone }: { list: Session[] | null; onClose: () => void; onDone?: () => void }) {
  const n = list?.length ?? 0;
  return (
    <Dialog
      open={Boolean(list)}
      onClose={onClose}
      title="全部标为已读"
      width={380}
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>
            取消
          </Button>
          <Button
            tone="primary"
            icon="check"
            onClick={() => {
              if (list) markSeen(list);
              onDone?.();
              onClose();
            }}
          >
            {`全部标为已读（${n}）`}
          </Button>
        </>
      }
    >
      <p className="text-[13px] leading-relaxed text-ink-2">把 {n} 个完成未看的会话标为已读？手机和其他浏览器也会一起变成已看。</p>
    </Dialog>
  );
}
