import { lazy, Suspense, useState } from "react";
import { useCanDeleteConversation } from "../lib/conversation-delete";
// A Persona conversation's menu in the sidebar (the iOS sidebar's long press, Android's drawer): "设为常驻" / "取消常驻",
// "Rename…", "Archive", "Copy session ID", then "End…" while it runs. The main conversation is never a lead, renamed or
// archived; the background supervisor (#5082, lib/background-supervisor.ts) is built in and only has "Copy session ID".
// All of it is the hub's (the same calls as "All chats"): 设为常驻 makes the chat a leader (#4331,
// POST …/directions/{id}/make), 取消常驻 ends it (…/end; the tasks it handed out keep their supervisor).
import { errorText } from "../lib/api";
import { sessionTitle } from "../lib/format";
import { conversationArchiveItem, conversationMenuDeps, reread, type ConversationMenuDeps } from "./conversation-actions";
import type { Session } from "../lib/types";
import { Dialog, Loading, Menu, type MenuItem } from "../ui/atoms";
import type { MenuAt } from "../ui/menu-model";
import { t } from "../i18n";

/** A running conversation can be ended (iOS `status.isLive`). */
export const conversationLive = (s: Session) => s.status !== "offline";

/** 设为常驻 / 取消常驻 for a conversation that is not the main one (`leader`: it is one now). Both ask a second time
 * before the hub call (#6214): the first choice only arms the row, the second runs it, and the detail says what moves —
 * unpinning sends the chat back to 最近 and returns its open tasks to the main chat's supervision, nothing deleted. */
export function leaderItem(s: Session, leader: boolean, deps: ConversationMenuDeps = conversationMenuDeps): MenuItem {
  const title = sessionTitle(s);
  return {
    key: "leader",
    label: leader ? t("conversation-menu.end_leader") : t("conversation-menu.make_leader"),
    icon: "pin",
    accel: "p",
    confirm: leader ? t("conversation-menu.end_leader_confirm") : t("conversation-menu.make_leader_confirm"),
    confirmDetail: leader
      ? t("conversation-menu.end_leader_confirm_detail")
      : t("conversation-menu.make_leader_confirm_detail"),
    // The menu stays open while the hub call runs, shows a refusal in place, and closes only on success.
    inline: true,
    onSelect: async () => {
      try {
        await deps.lead(s, !leader);
        deps.notify(leader ? t("conversation-menu.leader_ended", { title }) : t("conversation-menu.leader_made", { title }), "ok");
      } catch (e) {
        deps.notify(t("conversation-menu.leader_failed", { error: errorText(e) }), "error");
      }
      reread(deps);
    },
  };
}

/** `leader`: the conversation is a leader (常驻) now. `builtIn`: it is the background supervisor, which the hub keeps
 *  listed and running: no 设为常驻, Rename, Archive or End. */
export function conversationMenuItems(s: Session, o: { main: boolean; leader?: boolean; builtIn?: boolean; canDelete?: boolean; onDelete?: () => void }, deps: ConversationMenuDeps = conversationMenuDeps): MenuItem[] {
  const copy: MenuItem = { key: "copy", label: t("conversation-menu.copy_id"), icon: "copy", accel: "c", onSelect: () => void deps.copy(s.session_id) };
  if (o.builtIn) return [copy];
  const own: MenuItem[] = o.main
    ? []
    : [
        leaderItem(s, !!o.leader, deps),
        {
          key: "rename",
          label: t("conversation-menu.rename"),
          icon: "pencil",
          accel: "r",
          onSelect: () => {},
          rename: {
            value: s.title ?? "",
            placeholder: t("conversation-menu.title_placeholder"),
            commit: async (name) => {
              await deps.rename(s, name).catch((e) => Promise.reject(new Error(t("conversation-menu.rename_failed", { error: errorText(e) }))));
              deps.notify(t("conversation-menu.renamed"), "ok");
              reread(deps);
            },
          },
        },
        conversationArchiveItem(s, deps),
      ];
  return [
    ...own,
    copy,
    ...(!o.main && o.canDelete && o.onDelete ? [{ key: "delete", label: t("conversation-menu.delete"), icon: "trash" as const, danger: true, divider: true, keepOpen: true, onSelect: o.onDelete }] : []),
    ...(conversationLive(s)
      ? [
          {
            key: "end",
            label: t("conversation-menu.end"),
            icon: "stop" as const,
            danger: true,
            divider: true,
            closes: true,
            confirm: t("conversation-menu.end_confirm"),
            inline: true,
            onSelect: async () => {
              await deps.end(s).catch((e) => Promise.reject(new Error(t("conversation-menu.end_failed", { error: errorText(e) }))));
              deps.notify(t("conversation-menu.ended"), "ok");
              reread(deps);
            },
          },
        ]
      : []),
  ];
}

/** The menu of a row (`at`: where a right click opened it; without it, beside `anchor`). */
export function ConversationRowMenu({ s, main, leader, builtIn, anchor, at, onClose }: { s: Session; main: boolean; leader?: boolean; builtIn?: boolean; anchor: HTMLElement; at?: MenuAt; onClose: () => void }) {
  const p = at ?? { anchor };
  const canDelete = useCanDeleteConversation(s, main, builtIn);
  const [deleting, setDeleting] = useState(false);
  if (deleting) return <ConversationDeleteConfirmation s={s} onClose={onClose} />;
  return <Menu anchor={p.anchor ?? null} point={p.point} onClose={onClose} align="right" width={200} items={conversationMenuItems(s, { main, leader, builtIn, canDelete, onDelete: () => setDeleting(true) })} />;
}


const DeleteConversationDialog = lazy(() => import("./DeleteConversationDialog").then((m) => ({ default: m.DeleteConversationDialog })));

/** The destructive confirmation loads on intent, keeping the common first screen small. */
export function ConversationDeleteConfirmation(props: { s: Session; onClose: () => void }) {
  return <Suspense fallback={<Dialog open title={t("conversation-menu.delete")} onClose={props.onClose}><Loading /></Dialog>}>
    <DeleteConversationDialog {...props} />
  </Suspense>;
}
