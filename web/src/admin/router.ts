// The admin page's own tiny router: /admin, /admin/<page>. The hub serves admin/index.html for each of them.
import { useSyncExternalStore } from "react";
import type { Page } from "./nav";
import { NAV } from "./nav";

export function pageOf(pathname: string): Page {
  let seg = pathname.replace(/^\/admin\/?/, "").split("/")[0];
  if (seg === "plugins") seg = "extensions"; // a bookmark from before #1845

  return NAV.find((n) => n.page === seg)?.page ?? "overview";
}

export function pathOf(page: Page): string {
  return page === "overview" ? "/admin" : `/admin/${page}`;
}

const listeners = new Set<() => void>();
addEventListener("popstate", () => listeners.forEach((l) => l()));

export function navigate(page: Page) {
  const to = pathOf(page);
  if (to !== location.pathname) history.pushState(null, "", to + location.search);
  listeners.forEach((l) => l());
}

export function usePage(): Page {
  const path = useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => location.pathname,
  );
  return pageOf(path);
}

// Dev only (screenshots): ?dialog=<name> opens that page's dialog on load. Always "" in production builds.
export function devDialog(): string {
  return import.meta.env.DEV ? (new URLSearchParams(location.search).get("dialog") ?? "") : "";
}
