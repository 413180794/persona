// The live screen's page, /screen/<machine id> (docs/computer-use.md「实时画面」): which machine, and which token.
// The app opens it in its web view with its own device token in the fragment (#token=…&embed=1): that one stays in
// this page's memory, is never stored and never traded for a browser token (the web's sign-in link would mint one per
// open). A signed-in browser uses its own.

import { getToken } from "../lib/api";

export type ScreenTarget = { mid: string; token: string; embedded: boolean };

/** /screen/<mid> → mid; anything else is not the live screen. */
export function screenMachine(pathname: string): string | null {
  const p = pathname.split("/").filter(Boolean);
  return p[0] === "screen" && p[1] ? decodeURIComponent(p[1]) : null;
}

/** The page's machine and token, the fragment's token taken out of the address bar's text (returned as rest). */
export function screenTarget(href: string, stored: () => string = getToken): { target: ScreenTarget | null; rest: string } {
  const u = new URL(href);
  const mid = screenMachine(u.pathname);
  const hash = new URLSearchParams(u.hash.replace(/^#/, ""));
  const token = hash.get("token")?.trim() ?? "";
  const embedded = hash.get("embed") === "1";
  hash.delete("token");
  hash.delete("embed");
  const rest = u.pathname + u.search + (hash.toString() ? "#" + hash.toString() : "");
  if (!mid) return { target: null, rest };
  return { target: { mid, token: token || stored(), embedded }, rest };
}

/** The WebSocket that carries the Mac's VNC (its port 5900) through the hub's port relay (docs/ports.md). */
export function screenSocket(location: { protocol: string; host: string }, mid: string, token: string): string {
  const scheme = location.protocol === "https:" ? "wss" : "ws";
  return `${scheme}://${location.host}/api/v1/machines/${encodeURIComponent(mid)}/ports/5900/connect?token=${encodeURIComponent(token)}`;
}

/** The Mac account's short name, from its home directory (/Users/<name>): what Screen Sharing's sign-in asks for. */
export function accountOf(home: string | undefined): string {
  const m = /^\/Users\/([^/]+)\/?$/.exec(home ?? "");
  return m ? m[1] : "";
}
