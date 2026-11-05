// The sidebar's entries around Persona (#1239): one entry for the main conversation, the other conversations below it,
// and which row a route lights up. Pure, so the order and the selection are tested without a DOM.
import type { Route } from "./router";
import type { Session } from "./types";
import { byTime } from "./times";

/** The sidebar row a route selects. Persona's main conversation is one row, under `/`, `/j` and `/j/<main>` alike. */
export function sidebarActiveKey(route: Route, main?: string): string {
  switch (route.name) {
    case "session":
      return `s:${route.mid}/${route.sid}`;
    case "jarvis":
      return !route.sid || route.sid === main ? "persona" : `j:${route.sid}`;
    case "persona-new":
      return "j:new";
    case "health":
      return "health";
    case "history":
      return route.origin ? `auto:${route.origin}` : "history";
    case "machine":
      return "machines";
    case "agents":
      return route.id ? `a:${route.id}` : "agents";
    case "extension":
      return extensionNavKey(route.extension, route.ui);
    default:
      return route.name;
  }
}

/** An extension page's sidebar row (docs/extensions.md「UI 插槽」 nav_page). */
export const extensionNavKey = (extension: string, ui: string) => `p:${extension}/${ui}`;

/** The conversations listed under「Persona 对话」: the most recent ones, without the main one (it has its own row), and
 *  the open one (`current`) after them when it is older, so the sidebar always has its row (#2283). */
export function otherConversations(conversations: readonly Session[], main: string | undefined, limit = 5, current?: string): Session[] {
  const sorted = conversations.filter((c) => c.session_id !== main).sort((a, b) => byTime(b.updated_at, a.updated_at));
  return firstAndCurrent(sorted, limit, (c) => c.session_id === current);
}

/** A shortened list's first `n`, and the open item after them when it is further down (#2283). */
export function firstAndCurrent<T>(list: readonly T[], n: number, isCurrent: (x: T) => boolean): T[] {
  const first = list.slice(0, n);
  const current = list.slice(n).find(isCurrent);
  return current ? [...first, current] : first;
}

/** The page entries under Persona, in the sidebar's order (the app's 助手 first, then the rest of its drawer). 消息 shows
 * only in a browser that turned on 首页显示消息 (lark-chats.ts), on a hub with feature `lark`. The extensions' pages (nav_page) go right before 设置. */
export const NAV_PAGES = ["home", "inbox", "health", "messages", "workspace-board", "todos", "routines", "kb", "machines", "terminal", "settings"] as const;
