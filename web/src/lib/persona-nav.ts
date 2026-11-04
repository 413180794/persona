// The sidebar's entries around Persona (#1239): one entry for the main conversation, the other conversations below it,
// and which row a route lights up. Pure, so the order and the selection are tested without a DOM.
import type { Route } from "./router";
import type { Session } from "./types";

/** The sidebar row a route selects. Persona's main conversation is one row, under `/`, `/j` and `/j/<main>` alike. */
export function sidebarActiveKey(route: Route, main?: string): string {
  switch (route.name) {
    case "session":
      return `s:${route.mid}/${route.sid}`;
    case "jarvis":
      return !route.sid || route.sid === main ? "persona" : `j:${route.sid}`;
    case "persona-new":
      return "j:new";
    case "machine":
      return "machines";
    case "agents":
      return route.id ? `a:${route.id}` : "agents";
    default:
      return route.name;
  }
}

/** The conversations listed under「Persona 对话」: the most recent ones, without the main one (it has its own row). */
export function otherConversations(conversations: readonly Session[], main: string | undefined, limit = 5): Session[] {
  return conversations
    .filter((c) => c.session_id !== main)
    .sort((a, b) => (b.updated_at ?? "").localeCompare(a.updated_at ?? ""))
    .slice(0, limit);
}

/** The page entries under Persona, in the sidebar's order (the app's 助手 first, then the rest of its drawer). 消息 shows
 * only in a browser that turned on 首页显示消息 (lark-chats.ts). */
export const NAV_PAGES = ["home", "inbox", "messages", "todos", "routines", "kb", "machines", "settings"] as const;
