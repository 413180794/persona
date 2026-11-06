// Which paths a conversation names are there on its machine (GET /machines/{mid}/stat; iOS FilePresence): only
// those are linked. Asked in batches a moment after messages show, per machine + working directory, and remembered:
// a path that is there for 30 minutes, a missing one for 2 (the agent may be about to write it), no answer for 1.
// Until the answer comes, and when none can (an old node, no network), a path is plain text. Paths with no answer are
// asked again when the hub says hello after a reconnect; at most CAP answers are kept, the stale and oldest go first.
import { useEffect, useMemo, useSyncExternalStore } from "react";
import { api, enc } from "./api";
import { onHubEvent } from "./store";

export type PathKind = "file" | "dir" | "denied" | "missing";
export type PathStat = { kind: PathKind; size?: number };

const LIFETIME: Record<PathKind | "none", number> = { file: 30 * 60e3, dir: 30 * 60e3, denied: 30 * 60e3, missing: 2 * 60e3, none: 60e3 };
const BATCH = 200; // the node answers at most this many at a time (maxStatPaths)
const WAIT = 60; // ms: rows that show together are asked about together
const CAP = 5000; // answers kept; past it, expired ones are dropped, then the oldest

type Entry = { stat?: PathStat; at: number };

const known = new Map<string, Entry>();
const waiting = new Map<string, Set<string>>(); // scope → paths
const asking = new Set<string>(); // keys in flight
const listeners = new Set<() => void>();
let version = 0;
let timer: ReturnType<typeof setTimeout> | undefined;
let generation = 0; // bumped by resetPathPresence: answers to what was asked before it are dropped

const scopeKey = (mid: string, cwd: string) => `${mid}\n${cwd}`;
const key = (mid: string, cwd: string, path: string) => `${mid}\n${cwd}\n${path}`;

function fresh(e: Entry | undefined, now: number) {
  return !!e && now - e.at < LIFETIME[e.stat?.kind ?? "none"];
}

/** Asks about the paths not known (or known too long ago), with whatever else is asked in the same moment. */
export function askPaths(mid: string, cwd: string, paths: string[]) {
  const now = Date.now();
  for (const p of paths) {
    const k = key(mid, cwd, p);
    if (asking.has(k) || fresh(known.get(k), now)) continue;
    const s = scopeKey(mid, cwd);
    const set = waiting.get(s) ?? new Set<string>();
    set.add(p);
    waiting.set(s, set);
  }
  if (waiting.size && !timer) timer = setTimeout(flush, WAIT);
}

function flush() {
  timer = undefined;
  const batches = [...waiting];
  waiting.clear();
  for (const [scope, set] of batches) {
    const [mid, cwd] = scope.split("\n");
    const all = [...set];
    for (let i = 0; i < all.length; i += BATCH) void ask(mid, cwd, all.slice(i, i + BATCH));
  }
}

async function ask(mid: string, cwd: string, paths: string[]) {
  const gen = generation;
  const keys = paths.map((p) => key(mid, cwd, p));
  keys.forEach((k) => asking.add(k));
  let got: Map<string, PathStat> | undefined;
  try {
    const r = await api<{ stats?: { path: string; kind: PathKind; size?: number }[] }>(
      `/machines/${enc(mid)}/stat?paths=${enc(paths.join("\n"))}&cwd=${enc(cwd)}`,
    );
    got = new Map((r.stats ?? []).map((s) => [s.path, { kind: s.kind, size: s.size }]));
  } catch {
    // no answer: plain text for now, asked again in a minute or when the hub is back
  }
  if (gen !== generation) return; // reset while out: this answer belongs to what was forgotten
  const at = Date.now();
  paths.forEach((p, i) => {
    asking.delete(keys[i]);
    known.delete(keys[i]); // re-inserted last: Map order is then oldest answer first
    known.set(keys[i], { stat: got ? (got.get(p) ?? { kind: "missing" }) : undefined, at });
  });
  bound(at);
  changed();
}

/** Keeps at most CAP answers: expired ones go first, then the oldest. */
function bound(now: number) {
  if (known.size <= CAP) return;
  for (const [k, e] of known) if (!fresh(e, now)) known.delete(k);
  for (const k of known.keys()) {
    if (known.size <= CAP) break;
    known.delete(k);
  }
}

function changed() {
  version++;
  listeners.forEach((l) => l());
}

/** The hub is back (hello): forget the paths that got no answer, so the shown ones are asked again now. */
export function onReconnect() {
  let dropped = false;
  for (const [k, e] of known) {
    if (e.stat) continue;
    known.delete(k);
    dropped = true;
  }
  if (dropped) changed();
}

onHubEvent("hello", onReconnect);

/** What is known of a path on a machine; undefined while unknown. */
export function pathStat(mid: string, cwd: string, path: string): PathStat | undefined {
  return known.get(key(mid, cwd, path))?.stat;
}

/** A path to link: there as a file or a folder, or in one macOS keeps from the node (opening it says what to do). */
export function isLinkable(s: PathStat | undefined): boolean {
  return s?.kind === "file" || s?.kind === "dir" || s?.kind === "denied";
}

function subscribe(l: () => void) {
  listeners.add(l);
  return () => listeners.delete(l);
}

/**
 * The linkable ones of `paths` on mid (relative ones in cwd), asking about them; re-renders when answers come.
 * Returns a stable string key too (the linkable paths joined), for memoizing what is drawn from them.
 */
export function usePathLinks(mid: string | undefined, cwd: string | undefined, paths: string[]): { linkable: (path: string) => boolean; key: string; stat: (path: string) => PathStat | undefined } {
  const v = useSyncExternalStore(subscribe, () => version, () => version);
  const joined = paths.join("\n");
  useEffect(() => {
    if (mid && joined) askPaths(mid, cwd ?? "", joined.split("\n"));
  }, [mid, cwd, joined, v]);
  return useMemo(() => {
    const stat = (p: string) => (mid ? pathStat(mid, cwd ?? "", p) : undefined);
    const yes = new Set(joined ? joined.split("\n").filter((p) => isLinkable(stat(p))) : []);
    return { linkable: (p: string) => yes.has(p), key: [...yes].join("\n"), stat };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mid, cwd, joined, v]); // v: answers came
}

/** Forget everything (tests). */
export function resetPathPresence() {
  known.clear();
  waiting.clear();
  asking.clear();
  if (timer) clearTimeout(timer);
  timer = undefined;
  generation++;
  version++;
}
