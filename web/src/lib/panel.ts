// The page's right-hand panel: a session's terminal / files / Git, or a link opened in the page (docs/web.md「侧边面板」).
// One panel at a time; opening another replaces it. Session panels close when the page moves to another conversation;
// a link stays until closed (it is not tied to the conversation it came from).
import { useSyncExternalStore } from "react";

export type Panel =
  | { kind: "terminal"; mid: string; sid: string }
  | { kind: "files"; mid: string; sid: string; cwd: string; path?: string }
  | { kind: "git"; mid: string; sid: string; cwd: string }
  | { kind: "file"; mid: string; path: string; cwd?: string; line?: number }
  | { kind: "link"; url: string; title?: string }
  /** A task of the assistant's conversation: its agent's chat (docs/unified-entry.md). */
  | { kind: "task"; todo: string };

let current: Panel | null = null;
const listeners = new Set<() => void>();

export function openPanel(p: Panel | null) {
  current = p;
  listeners.forEach((l) => l());
}

export function closePanel() {
  openPanel(null);
}

export function getPanel() {
  return current;
}

/** Toggle a session panel from a header button: the same kind for the same session closes it. */
export function togglePanel(p: Panel) {
  const c = current;
  const same = c && c.kind === p.kind && "mid" in c && "mid" in p && c.mid === p.mid && ("sid" in c && "sid" in p ? c.sid === p.sid : true);
  openPanel(same ? null : p);
}

export function usePanel(): Panel | null {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => current,
  );
}
