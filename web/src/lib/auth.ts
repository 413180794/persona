import { api, setToken } from "./api";
import { formatDate, t } from "../i18n";

function browserName() {
  const ua = navigator.userAgent;
  const b = /Edg\//.test(ua) ? "Edge" : /Chrome\//.test(ua) ? "Chrome" : /Firefox\//.test(ua) ? "Firefox" : /Safari\//.test(ua) ? "Safari" : t("lib-auth.browser");
  const os = /Mac OS X/.test(ua) ? "Mac" : /Windows/.test(ua) ? "Windows" : /Linux/.test(ua) ? "Linux" : "";
  return [b, os].filter(Boolean).join(" · ");
}

/** Where this browser keeps the id of its device token at the hub, to revoke it on signing out and to have it
 *  replaced when it signs in again. */
export const TOKEN_ID_KEY = "jarvis.tokenId";

/** The name this browser's device token goes by on the hub, with the day it signed in: two browsers of the same kind
 *  (or one browser on two days) can be told apart in 设置 › 设备. */
export function deviceName(now: Date = new Date()) {
  return t("lib-auth.device_name_dated", { browser: browserName(), date: formatDate(now, { month: "short", day: "numeric" }) });
}

// pairToken turns what was pasted on the login page into the token to try: a bare token (the hub's, `jp_…`, `jd_…`)
// as it is; the token in a `persona://pair?…` / `jarvis://pair?…` link (what `personad hub pair` prints for the app);
// or the token in any link carrying `?token=…` or `#token=…`. Anything else comes back trimmed, so the hub turns it
// down as before.
export function pairToken(input: string): string {
  const t = input.trim();
  if (!/^[a-z][a-z0-9+.-]*:/i.test(t)) return t;
  let u: URL;
  try {
    u = new URL(t);
  } catch {
    return t;
  }
  // URLSearchParams undoes the percent-encoding
  const token = u.searchParams.get("token")?.trim() || new URLSearchParams(u.hash.replace(/^#/, "")).get("token")?.trim();
  return token || t;
}

// Pair with the hub's token (`personad hub pair --legacy` prints it), the app's pairing link, or a device token
// (pairToken). The pairing token is checked, then traded for this browser's own one right away (docs/protocol.md
// 「鉴权」), so it can be revoked on its own and the hub's token is not kept here. The credential this browser had
// before (its id is kept when the hub turned the token down) goes in "replaces", and the hub takes it off: signing in
// again keeps one device for this browser. Throws what the hub said (ApiError) when the token is wrong.
export async function pair(input: string): Promise<void> {
  const t = pairToken(input);
  await api("/machines", { token: t });
  let use = t;
  if (!t.startsWith("jd_")) {
    try {
      const replaces = localStorage.getItem(TOKEN_ID_KEY) ?? "";
      const json = { name: deviceName(), kind: "web", ...(replaces ? { replaces } : {}) };
      const r = await api<{ token: string; id: string }>("/auth/tokens", { method: "POST", json, token: t });
      use = r.token;
      localStorage.setItem(TOKEN_ID_KEY, r.id);
    } catch {
      // an old hub without device tokens: keep the pairing token
    }
  }
  setToken(use);
}

// takeToken finds a token in a link (`?token=…`, or `#token=…`, which never reaches the server) and returns it with
// the link it came in minus the token, to put in the address bar at once: the token stays out of history and
// bookmarks of the page, and out of anything copied from the address bar.
export function takeToken(href: string): { token: string; rest: string } | null {
  const u = new URL(href);
  let token = u.searchParams.get("token")?.trim() ?? "";
  u.searchParams.delete("token");
  const hash = new URLSearchParams(u.hash.replace(/^#/, ""));
  if (hash.has("token")) {
    token ||= hash.get("token")?.trim() ?? "";
    hash.delete("token");
    const h = hash.toString();
    u.hash = h ? "#" + h : "";
  }
  if (!token && !href.includes("token=")) return null;
  return { token, rest: u.pathname + u.search + u.hash };
}
