// What the sidebar offers the keyboard shortcuts (docs/web.md「快捷键」): fold or open every group, switch how Agents are
// grouped, walk the rows. Sidebar.tsx subscribes (`onFoldAll`, `onGrouping`) and marks its list `data-sidebar-rows`; the
// shortcuts call the rest. Kept out of Sidebar.tsx so that file only gains the few lines that listen.
import { AGENT_GROUPINGS, agentGrouping, type AgentGrouping } from "./agent-groups";

export const GROUPING_KEY = "jarvis.agentGrouping";

// ── fold all / open all ──────────────────────────────────

const foldListeners = new Set<(folded: boolean) => void>();

/** Every foldable group of the sidebar follows (the sidebar's useFolded subscribes here). */
export function onFoldAll(listener: (folded: boolean) => void): () => void {
  foldListeners.add(listener);
  return () => void foldListeners.delete(listener);
}

export function setAllFolded(folded: boolean) {
  foldListeners.forEach((f) => f(folded));
}

// ── how Agents are grouped ───────────────────────────────

const groupingListeners = new Set<(g: AgentGrouping) => void>();

export function onGrouping(listener: (g: AgentGrouping) => void): () => void {
  groupingListeners.add(listener);
  return () => void groupingListeners.delete(listener);
}

/** The grouping after `current`, among those offered (a hub without todos has no 按待办). */
export function nextGrouping(current: AgentGrouping, offered: readonly AgentGrouping[] = AGENT_GROUPINGS.map((g) => g.key)): AgentGrouping {
  // a choice the hub cannot offer is shown as the first one (the sidebar does the same)
  const i = Math.max(0, offered.indexOf(current));
  return offered[(i + 1) % offered.length] ?? current;
}

/** Switches the sidebar to its next grouping (kept in this browser, as the menu's choice is); returns it. */
export function cycleGrouping(offered?: readonly AgentGrouping[]): AgentGrouping {
  const next = nextGrouping(agentGrouping(localStorage.getItem(GROUPING_KEY)), offered);
  localStorage.setItem(GROUPING_KEY, next);
  groupingListeners.forEach((f) => f(next));
  return next;
}

export const groupingLabel = (g: AgentGrouping) => AGENT_GROUPINGS.find((x) => x.key === g)?.label ?? "";

// ── walking the rows ─────────────────────────────────────

/** The row after (`dir` 1) or before (-1) `current` among `count`, wrapping round; none is current → the first / last. */
export function stepIndex(count: number, current: number, dir: 1 | -1): number {
  if (count <= 0) return -1;
  if (current < 0) return dir === 1 ? 0 : count - 1;
  return (current + dir + count) % count;
}

/** Opens the previous / next row of the sidebar's list: the one you can see (not inside a folded group) and that is a page. */
export function stepSidebarRow(dir: 1 | -1, root: ParentNode = document): boolean {
  const rows = [...root.querySelectorAll<HTMLAnchorElement>("[data-sidebar-rows] a[href]:not([aria-label])")].filter((a) => !a.closest("[inert]") && a.getClientRects().length > 0);
  if (!rows.length) return false;
  const to = rows[stepIndex(rows.length, rows.findIndex((a) => a.getAttribute("aria-current") === "page"), dir)];
  if (!to) return false;
  // the row's own click opens it (and closes a drawer), as a tap would
  to.click();
  to.scrollIntoView({ block: "nearest" });
  return true;
}
