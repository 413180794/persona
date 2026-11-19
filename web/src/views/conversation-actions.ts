import { api, enc, errorText, sessionPath } from "../lib/api";
import { sessionTitle } from "../lib/format";
import { refreshAssistant } from "../lib/store";
import type { Session } from "../lib/types";
import { copyText, toast, type MenuItem, type ToastAction } from "../ui/atoms";
import { t } from "../i18n";

/** What the hub does for the menu; tests swap it. */
export interface ConversationMenuDeps {
  rename: (s: Session, title: string) => Promise<unknown>;
  archive: (s: Session) => Promise<unknown>;
  unarchive: (s: Session) => Promise<unknown>;
  end: (s: Session) => Promise<unknown>;
  refresh: () => Promise<unknown>;
  /** Makes the conversation a leader (常驻) or ends it, on the hub for every device. */
  lead: (s: Session, on: boolean) => Promise<unknown>;
  copy: (text: string) => unknown;
  notify: (text: string, tone: "ok" | "error", action?: ToastAction) => void;
}

export const conversationMenuDeps: ConversationMenuDeps = {
  rename: (s, title) => api(`${sessionPath(s.machine_id, s.session_id)}/rename`, { method: "POST", json: { title } }),
  archive: (s) => api(`/assistant/conversations/${enc(s.session_id)}/archive`, { method: "POST", json: {} }),
  unarchive: (s) => api(`/assistant/conversations/${enc(s.session_id)}/unarchive`, { method: "POST", json: {} }),
  end: (s) => api(`${sessionPath(s.machine_id, s.session_id)}/close`, { method: "POST" }),
  refresh: () => refreshAssistant(),
  lead: (s, on) => api(`/assistant/directions/${enc(s.session_id)}/${on ? "make" : "end"}`, { method: "POST", json: {} }),
  copy: copyText,
  notify: (text, tone, action) => toast(text, tone, action),
};

export const reread = (deps: ConversationMenuDeps) => void deps.refresh().catch(() => {});

async function undoArchive(s: Session, title: string, deps: ConversationMenuDeps) {
  try {
    await deps.unarchive(s);
    deps.notify(t("conversation-menu.unarchived", { title }), "ok");
  } catch (e) {
    deps.notify(t("conversation-menu.undo_failed", { error: errorText(e) }), "error");
  }
  reread(deps);
}

/** Archive is the same reversible action in a sidebar row and the open conversation's title menu (#6565). */
export function conversationArchiveItem(s: Session, deps: ConversationMenuDeps = conversationMenuDeps): MenuItem {
  const title = sessionTitle(s);
  return {
    key: "archive",
    label: t("conversation-menu.archive"),
    icon: "archive",
    onSelect: async () => {
      try {
        await deps.archive(s);
        // Undo puts it back as it was (an open conversation stays open: its page reads the machine's sessions)
        deps.notify(t("conversation-menu.archived", { title }), "ok", { label: t("conversation-menu.undo"), run: () => void undoArchive(s, title, deps) });
      } catch (e) {
        deps.notify(errorText(e), "error");
      }
      reread(deps);
    },
  };
}
