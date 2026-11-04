// The agent panes of one herdr tab, as tabs over the chat (lib/herdr-tree.ts tabSessions): which agent, what it is
// doing, its state. Arrow keys move between them like any tab list; a tab with one pane has none.
import { useEffect, useRef } from "react";
import { sessionTitle, stateOf, STATE_LABEL } from "../../lib/format";
import { agentLabel } from "../../lib/mentions";
import type { Session } from "../../lib/types";
import { cx, StatusDot } from "../../ui/atoms";

/** The tab an arrow key (or Home / End) moves to from `i` among `n`; undefined for any other key. */
export function nextTab(i: number, key: string, n: number): number | undefined {
  if (key === "ArrowRight") return (i + 1) % n;
  if (key === "ArrowLeft") return (i - 1 + n) % n;
  if (key === "Home") return 0;
  if (key === "End") return n - 1;
  return undefined;
}

export function PaneTabs({ sessions, current, onSelect }: { sessions: Session[]; current: string; onSelect: (s: Session) => void }) {
  const tabs = useRef<(HTMLButtonElement | null)[]>([]);
  const at = Math.max(0, sessions.findIndex((s) => s.session_id === current));
  useEffect(() => {
    // the open pane stays in sight when the row scrolls (narrow screens, long titles)
    tabs.current[at]?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
  }, [at]);
  if (sessions.length < 2) return null;
  return (
    <div role="tablist" aria-label="这个标签页里的 Agent" className="sticky top-0 z-10 flex shrink-0 gap-1 overflow-x-auto border-b border-line bg-page px-3 py-1 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
      {sessions.map((s, i) => {
        const on = i === at;
        const st = stateOf(s);
        return (
          <button
            key={s.session_id}
            ref={(el) => {
              tabs.current[i] = el;
            }}
            type="button"
            role="tab"
            aria-selected={on}
            tabIndex={on ? 0 : -1}
            title={`${agentLabel(s)} · ${sessionTitle(s)} · ${STATE_LABEL[st]}`}
            onClick={() => !on && onSelect(s)}
            onKeyDown={(e) => {
              const to = nextTab(i, e.key, sessions.length);
              if (to === undefined) return;
              e.preventDefault();
              tabs.current[to]?.focus();
              if (to !== i) onSelect(sessions[to]!);
            }}
            className={cx(
              "flex h-9 max-w-[240px] min-w-0 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-control px-2.5 text-[12.5px] transition-[background-color,color,box-shadow] duration-150 [@media(pointer:coarse)]:h-11",
              on ? "bg-surface font-medium text-ink shadow-btn" : "text-ink-2 hover:bg-hover hover:text-ink",
            )}
          >
            <StatusDot state={st} size={6} />
            <span className={cx("shrink-0", on ? "text-ink-2" : "text-ink-3")}>{agentLabel(s)}</span>
            <span className="min-w-0 truncate">{sessionTitle(s)}</span>
          </button>
        );
      })}
    </div>
  );
}
