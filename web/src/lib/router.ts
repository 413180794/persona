// A tiny history router: the hub serves index.html for every path that isn't a file.
import { useSyncExternalStore } from "react";
import { takeOverlayEntry } from "./back-close";

// Persona's main conversation is the front door (like the app, which opens on 助手): `/` and `/j` open it, `/j/<id>` is
// one conversation, `/j/new` starts a one-off one, and 总览 lives at `/overview`. `/extension/<id>/<ui>` is an extension's page
// (docs/extensions.md「UI 插槽」 nav_page).
export type Route =
  | { name: "home" }
  | { name: "workspace-board" }
  | { name: "persona-new" }
  | { name: "session"; mid: string; sid: string }
  // one herdr tab (a slot) of a workspace; `sid` picks its pane (lib/herdr-tree.ts resolveTab)
  | { name: "tab"; mid: string; space: string; slot: string; sid?: string }
  | { name: "jarvis"; sid?: string }
  | { name: "history" }
  | { name: "profile" }
  | { name: "inbox"; id?: string }
  | { name: "messages"; id?: string }
  | { name: "routines" }
  | { name: "kb"; kind?: string; id?: string }
  | { name: "machines" }
  | { name: "machine"; mid: string }
  // the terminal browser (docs/terminal.md「独立终端」): every pane of every machine; `id` is the terminal opened (an agent
  // session's id, or "pane:<pane>" for a plain shell)
  | { name: "terminal"; mid?: string; id?: string }
  | { name: "settings" }
  | { name: "agents"; id?: string }
  // the phone's「Agents」page (#1996): the sidebar's Agents block on a page of its own, next to 助手
  | { name: "agent-list" }
  | { name: "todos"; id?: string }
  | { name: "extension"; extension: string; ui: string };

export function parse(path: string): Route {
  const p = path.split("/").filter(Boolean).map(decodeURIComponent);
  if (p[0] === "s" && p[1] && p[2]) return { name: "session", mid: p[1], sid: p[2] };
  if (p[0] === "t" && p[1] && p[2] && p[3]) return { name: "tab", mid: p[1], space: p[2], slot: p[3], sid: p[4] || undefined };
  if (!p.length) return { name: "jarvis" };
  if (p[0] === "j" || p[0] === "persona") return p[1] === "new" && !p[2] ? { name: "persona-new" } : { name: "jarvis", sid: p[1] };
  if (p[0] === "workspace-board") return { name: "workspace-board" };
  if (p[0] === "overview") return { name: "home" };
  if (p[0] === "history") return { name: "history" };
  if (p[0] === "assistant") return { name: "profile" };
  if (p[0] === "inbox") return { name: "inbox", id: p[1] };
  if (p[0] === "messages") return { name: "messages", id: p[1] };
  if (p[0] === "routines") return { name: "routines" };
  if (p[0] === "kb") return { name: "kb", kind: p[1], id: p[2] };
  if (p[0] === "machines") return p[1] ? { name: "machine", mid: p[1] } : { name: "machines" };
  if (p[0] === "terminal") return p[1] && p[2] ? { name: "terminal", mid: p[1], id: p[2] } : { name: "terminal" };
  if (p[0] === "a") return { name: "agents", id: p[1] };
  if (p[0] === "agents") return { name: "agent-list" };
  if (p[0] === "settings") return { name: "settings" };
  // /plugin/<id>/<ui>: the same page under its name before #1845 (bookmarks)
  if ((p[0] === "extension" || p[0] === "plugin") && p[1] && p[2]) return { name: "extension", extension: p[1], ui: p[2] };
  if (p[0] === "todos") {
    const q = typeof window !== "undefined" ? new URLSearchParams(window.location.search).get("id") : null;
    return { name: "todos", id: p[1] || q || undefined };
  }
  return { name: "home" };
}

export function href(r: Route): string {
  switch (r.name) {
    case "session":
      return `/s/${encodeURIComponent(r.mid)}/${encodeURIComponent(r.sid)}`;
    case "tab":
      return ["/t", r.mid, r.space, r.slot, ...(r.sid ? [r.sid] : [])].map((x, i) => (i ? encodeURIComponent(x) : x)).join("/");
    case "jarvis":
      return r.sid ? `/j/${encodeURIComponent(r.sid)}` : "/";
    case "persona-new":
      return "/j/new";
    case "history":
      return "/history";
    case "profile":
      return "/assistant";
    case "inbox":
      return r.id ? `/inbox/${encodeURIComponent(r.id)}` : "/inbox";
    case "messages":
      return r.id ? `/messages/${encodeURIComponent(r.id)}` : "/messages";
    case "routines":
      return "/routines";
    case "kb":
      return ["/kb", r.kind, r.kind && r.id].filter(Boolean).map((x, i) => (i ? encodeURIComponent(x as string) : x)).join("/");
    case "machines":
      return "/machines";
    case "machine":
      return `/machines/${encodeURIComponent(r.mid)}`;
    case "terminal":
      return r.mid && r.id ? `/terminal/${encodeURIComponent(r.mid)}/${encodeURIComponent(r.id)}` : "/terminal";
    case "workspace-board":
      return "/workspace-board";
    case "settings":
      return "/settings";
    case "agents":
      return r.id ? `/a/${encodeURIComponent(r.id)}` : "/a";
    case "agent-list":
      return "/agents";
    case "todos":
      return r.id ? `/todos?id=${encodeURIComponent(r.id)}` : "/todos";
    case "extension":
      return `/extension/${encodeURIComponent(r.extension)}/${encodeURIComponent(r.ui)}`;
    default:
      return "/overview";
  }
}

const listeners = new Set<() => void>();
if (typeof addEventListener !== "undefined") {
  addEventListener("popstate", () => listeners.forEach((l) => l()));
}

export function navigate(r: Route | string, replace = false) {
  const to = typeof r === "string" ? r : href(r);
  if (to === location.pathname) return;
  // From inside an open overlay (a drawer's link, a sheet's action): the new page takes the overlay's entry, so Back
  // from it goes to the page under the overlay, not to a dead copy of it (#1996). A replace keeps the entry's state:
  // an overlay open over the page keeps its marker.
  if (replace) history.replaceState(history.state, "", to);
  else if (takeOverlayEntry()) history.replaceState(null, "", to);
  else history.pushState(null, "", to);
  listeners.forEach((l) => l());
}

export function useRoute(): Route {
  const path = useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => location.pathname,
  );
  return parse(path);
}

// <a> that navigates in-app on a plain click and still opens a new tab with ⌘ / middle click.
export function onLinkClick(e: React.MouseEvent, to: Route) {
  if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
  e.preventDefault();
  navigate(to);
}
