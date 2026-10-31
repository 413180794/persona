import { api, setToken } from "./api";

function browserName() {
  const ua = navigator.userAgent;
  const b = /Edg\//.test(ua) ? "Edge" : /Chrome\//.test(ua) ? "Chrome" : /Firefox\//.test(ua) ? "Firefox" : /Safari\//.test(ua) ? "Safari" : "浏览器";
  const os = /Mac OS X/.test(ua) ? "Mac" : /Windows/.test(ua) ? "Windows" : /Linux/.test(ua) ? "Linux" : "";
  return [b, os].filter(Boolean).join(" · ");
}

// Pair with the hub's token (`jarvisd hub pair` prints it) or a device token. The pairing token is checked, then traded
// for this browser's own one right away (docs/protocol.md「鉴权」), so it can be revoked on its own and the hub's
// token is not kept here. Throws what the hub said (ApiError) when the token is wrong.
export async function pair(token: string): Promise<void> {
  const t = token.trim();
  await api("/machines", { token: t });
  let use = t;
  if (!t.startsWith("jd_")) {
    try {
      const r = await api<{ token: string; id: string }>("/auth/tokens", { method: "POST", json: { name: `网页（${browserName()}）`, kind: "web" }, token: t });
      use = r.token;
      localStorage.setItem("jarvis.tokenId", r.id);
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
