// A tiny history router: the hub serves index.html for every path that isn't a file.
import { useSyncExternalStore } from "react";

export type Route =
  | { name: "home" }
  | { name: "session"; mid: string; sid: string }
  | { name: "jarvis"; sid?: string }
  | { name: "machines" }
  | { name: "settings" };

export function parse(path: string): Route {
  const p = path.split("/").filter(Boolean).map(decodeURIComponent);
  if (p[0] === "s" && p[1] && p[2]) return { name: "session", mid: p[1], sid: p[2] };
  if (p[0] === "j") return { name: "jarvis", sid: p[1] };
  if (p[0] === "machines") return { name: "machines" };
  if (p[0] === "settings") return { name: "settings" };
  return { name: "home" };
}

export function href(r: Route): string {
  switch (r.name) {
    case "session":
      return `/s/${encodeURIComponent(r.mid)}/${encodeURIComponent(r.sid)}`;
    case "jarvis":
      return r.sid ? `/j/${encodeURIComponent(r.sid)}` : "/j";
    case "machines":
      return "/machines";
    case "settings":
      return "/settings";
    default:
      return "/";
  }
}

const listeners = new Set<() => void>();
addEventListener("popstate", () => listeners.forEach((l) => l()));

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
