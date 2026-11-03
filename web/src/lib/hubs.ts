// The hubs this browser goes between (#1014, docs/web.md「切换 hub」). Each hub serves its own page and keeps its own
// sign-in on its own origin (no CORS, no token ever crosses origins), so switching is opening the other hub's page.
// The list holds names and addresses only and lives in this origin's localStorage; a switch carries it along in the
// address (`#hubs=`, never sent to a server) so the next hub's page knows the same hubs.

export interface SavedHub {
  /** The hub's origin, `scheme://host[:port]` as `URL.origin` writes it. */
  url: string;
  /** The hub's own name (hello's `hub_name`), refreshed whenever this browser opens it. */
  name?: string;
  /** What the user called it here; wins over `name`. */
  label?: string;
}

const KEY = "persona.hubs";
/** A link can carry a list in: keep it bounded. */
export const MAX_HUBS = 20;

/** The origin for what the user typed, or why it is not one. No scheme means https. */
export function normalizeHubURL(input: string): { url: string } | { error: string } {
  const raw = input.trim();
  if (!raw) return { error: "填 hub 的地址" };
  let u: URL;
  try {
    u = new URL(/^[a-z][a-z0-9+.-]*:\/\//i.test(raw) ? raw : `https://${raw}`);
  } catch {
    return { error: "不是一个地址" };
  }
  if (u.protocol !== "http:" && u.protocol !== "https:") return { error: "只能是 http:// 或 https:// 的地址" };
  if (u.username || u.password) return { error: "地址里不要带用户名和密码" };
  if ((u.pathname && u.pathname !== "/") || u.search || u.hash) return { error: "只填 hub 的地址，不要带路径" };
  if (!u.hostname) return { error: "不是一个地址" };
  return { url: u.origin };
}

/** The name to show: the user's, the hub's, else its host. */
export function hubTitle(h: SavedHub): string {
  return h.label?.trim() || h.name?.trim() || hostOf(h.url);
}

export function hostOf(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}

// What a stored or carried entry may hold: anything else (an old shape, a hand-edited value) is dropped.
function clean(list: unknown): SavedHub[] {
  if (!Array.isArray(list)) return [];
  const out: SavedHub[] = [];
  for (const e of list) {
    if (!e || typeof e !== "object") continue;
    const r = e as Record<string, unknown>;
    if (typeof r.url !== "string") continue;
    const n = normalizeHubURL(r.url);
    if (!("url" in n) || out.some((h) => h.url === n.url)) continue;
    const h: SavedHub = { url: n.url };
    if (typeof r.name === "string" && r.name.trim()) h.name = r.name.trim().slice(0, 80);
    if (typeof r.label === "string" && r.label.trim()) h.label = r.label.trim().slice(0, 80);
    out.push(h);
    if (out.length >= MAX_HUBS) break;
  }
  return out;
}

export function loadHubs(): SavedHub[] {
  try {
    return clean(JSON.parse(localStorage.getItem(KEY) ?? "[]"));
  } catch {
    return [];
  }
}

const listeners = new Set<() => void>();

function saveHubs(list: SavedHub[]): SavedHub[] {
  const kept = clean(list);
  localStorage.setItem(KEY, JSON.stringify(kept));
  listeners.forEach((fn) => fn());
  return kept;
}

/** Called when the list changes in this page (React re-reads with useHubs). */
export function onHubsChange(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** Note the hub this page is on, with the name it gave itself (empty keeps what was known). */
export function rememberHub(origin: string, name = ""): SavedHub[] {
  const n = normalizeHubURL(origin);
  if (!("url" in n)) return loadHubs();
  const list = loadHubs();
  const i = list.findIndex((h) => h.url === n.url);
  if (i < 0) return saveHubs([...list, { url: n.url, ...(name.trim() ? { name: name.trim() } : {}) }]);
  if (!name.trim() || list[i].name === name.trim()) return list;
  list[i] = { ...list[i], name: name.trim() };
  return saveHubs(list);
}

/** Add a hub by the address the user typed; the error is shown under the field. */
export function addHub(input: string, label = ""): { list: SavedHub[] } | { error: string } {
  const n = normalizeHubURL(input);
  if (!("url" in n)) return n;
  const list = loadHubs();
  if (list.some((h) => h.url === n.url)) return { error: "这台 hub 已经在列表里" };
  if (list.length >= MAX_HUBS) return { error: `最多记 ${MAX_HUBS} 台 hub` };
  return { list: saveHubs([...list, { url: n.url, ...(label.trim() ? { label: label.trim() } : {}) }]) };
}

export function renameHub(url: string, label: string): SavedHub[] {
  return saveHubs(loadHubs().map((h) => (h.url === url ? { url: h.url, ...(h.name ? { name: h.name } : {}), ...(label.trim() ? { label: label.trim() } : {}) } : h)));
}

export function removeHub(url: string): SavedHub[] {
  return saveHubs(loadHubs().filter((h) => h.url !== url));
}

/** Union by address: what this browser already has keeps its place and its label; new hubs go at the end. */
export function mergeHubs(local: SavedHub[], incoming: SavedHub[]): SavedHub[] {
  const out = local.map((h) => ({ ...h }));
  for (const h of incoming) {
    const mine = out.find((x) => x.url === h.url);
    if (mine) {
      if (!mine.name && h.name) mine.name = h.name;
      if (!mine.label && h.label) mine.label = h.label;
    } else if (out.length < MAX_HUBS) out.push({ ...h });
  }
  return out;
}

/** The address that opens another hub's page and hands it this list (no token in it, ever). */
export function switchURL(target: string, list: SavedHub[]): string {
  const hash = new URLSearchParams({ hubs: JSON.stringify(clean(list)) });
  return `${target}/#${hash.toString()}`;
}

/**
 * takeHubs finds a list handed over in the address (`#hubs=…`) and returns it with the link minus it, to put in the
 * address bar at once (like takeToken). A list that does not parse is dropped, the link is still cleaned.
 */
export function takeHubs(href: string): { hubs: SavedHub[]; rest: string } | null {
  const u = new URL(href);
  const hash = new URLSearchParams(u.hash.replace(/^#/, ""));
  if (!hash.has("hubs")) return null;
  let hubs: SavedHub[] = [];
  try {
    hubs = clean(JSON.parse(hash.get("hubs") ?? "[]"));
  } catch {
    hubs = [];
  }
  hash.delete("hubs");
  const h = hash.toString();
  u.hash = h ? "#" + h : "";
  return { hubs, rest: u.pathname + u.search + u.hash };
}

/** Merge a handed-over list into this browser's. */
export function acceptHubs(incoming: SavedHub[]): SavedHub[] {
  return incoming.length ? saveHubs(mergeHubs(loadHubs(), incoming)) : loadHubs();
}
