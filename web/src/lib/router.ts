// A tiny history router: the hub serves index.html for every path that isn't a file.
import { useSyncExternalStore } from "react";

// Persona's main conversation is the front door (like the app, which opens on 助手): `/` and `/j` open it, `/j/<id>` is
// one conversation, `/j/new` starts a one-off one, and 总览 lives at `/overview`.
export type Route =
  | { name: "home" }
  | { name: "persona-new" }
  | { name: "session"; mid: string; sid: string }
  | { name: "jarvis"; sid?: string }
  | { name: "history" }
  | { name: "profile" }
  | { name: "inbox"; id?: string }
  | { name: "routines" }
  | { name: "kb"; kind?: string; id?: string }
  | { name: "machines" }
  | { name: "machine"; mid: string }
  | { name: "settings" }
  | { name: "agents"; id?: string }
  | { name: "todos"; id?: string };

export function parse(path: string): Route {
  const p = path.split("/").filter(Boolean).map(decodeURIComponent);
  if (p[0] === "s" && p[1] && p[2]) return { name: "session", mid: p[1], sid: p[2] };
  if (!p.length) return { name: "jarvis" };
  if (p[0] === "j" || p[0] === "persona") return p[1] === "new" && !p[2] ? { name: "persona-new" } : { name: "jarvis", sid: p[1] };
  if (p[0] === "overview") return { name: "home" };
  if (p[0] === "history") return { name: "history" };
  if (p[0] === "assistant") return { name: "profile" };
  if (p[0] === "inbox") return { name: "inbox", id: p[1] };
  if (p[0] === "routines") return { name: "routines" };
  if (p[0] === "kb") return { name: "kb", kind: p[1], id: p[2] };
  if (p[0] === "machines") return p[1] ? { name: "machine", mid: p[1] } : { name: "machines" };
  if (p[0] === "a") return { name: "agents", id: p[1] };
  if (p[0] === "settings") return { name: "settings" };
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
    case "routines":
      return "/routines";
    case "kb":
      return ["/kb", r.kind, r.kind && r.id].filter(Boolean).map((x, i) => (i ? encodeURIComponent(x as string) : x)).join("/");
    case "machines":
      return "/machines";
    case "machine":
      return `/machines/${encodeURIComponent(r.mid)}`;
    case "settings":
      return "/settings";
    case "agents":
      return r.id ? `/a/${encodeURIComponent(r.id)}` : "/a";
    case "todos":
      return r.id ? `/todos?id=${encodeURIComponent(r.id)}` : "/todos";
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
  if (replace) history.replaceState(null, "", to);
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
