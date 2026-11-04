// The agent panes of one herdr tab, as tabs over the chat (lib/herdr-tree.ts tabSessions): which agent, what it is
// doing, its state. Arrow keys move between them like any tab list; a tab with one pane has none.
import { useEffect, useRef } from "react";
import { sessionTitle, stateOf, STATE_LABEL } from "../../lib/format";
import { agentLabel } from "../../lib/mentions";
import type { Session } from "../../lib/types";
import { NARROW, useMedia } from "../../lib/viewport";
import { cx, StatusDot } from "../../ui/atoms";

/** The tab an arrow key (or Home / End) moves to from `i` among `n`; undefined for any other key. */
export function nextTab(i: number, key: string, n: number): number | undefined {
  if (key === "ArrowRight") return (i + 1) % n;
  if (key === "ArrowLeft") return (i - 1 + n) % n;
  if (key === "Home") return 0;
  if (key === "End") return n - 1;
  return undefined;
}

/** How the tabs sit on a narrow screen (#1996): up to three share the width; more scroll, with faded ends and the open
 *  one brought to the middle. Wide screens always size them to their titles. */
export function paneTabsFit(n: number): "fill" | "scroll" {
  return n <= 3 ? "fill" : "scroll";
}

export function PaneTabs({ sessions, current, onSelect }: { sessions: Session[]; current: string; onSelect: (s: Session) => void }) {
  const tabs = useRef<(HTMLButtonElement | null)[]>([]);
  const at = Math.max(0, sessions.findIndex((s) => s.session_id === current));
  const fit = paneTabsFit(sessions.length);
  const centre = useMedia(NARROW) && fit === "scroll";
  useEffect(() => {
    // the open pane stays in sight when the row scrolls (narrow screens, long titles); a phone centres it
    tabs.current[at]?.scrollIntoView?.({ block: "nearest", inline: centre ? "center" : "nearest" });
  }, [at, centre]);
  if (sessions.length < 2) return null;
  return (
    <div
      role="tablist"
      aria-label="这个标签页里的 Agent"
      className={cx(
        "sticky top-0 z-10 flex shrink-0 gap-1 overflow-x-auto border-b border-line bg-page px-3 py-1 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden",
        // a short screen (a phone on its side): one 36px row, the taps still 44 (touch-hit)
        "[@media(max-height:500px)]:py-0",
        fit === "scroll" && "max-lg:[mask-image:linear-gradient(to_right,transparent,#000_12px,#000_calc(100%-12px),transparent)]",
        // sharing the width there is nothing to scroll: the bar doesn't clip, so the taps reach past its 36px
        fit === "fill" && "max-lg:overflow-visible",
      )}
    >
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
              "flex h-9 max-w-[240px] min-w-0 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-control px-2.5 text-[12.5px] transition-[background-color,color,box-shadow] duration-150 touch-hit [@media(pointer:coarse)_and_(min-height:501px)]:h-11",
              fit === "fill" && "max-lg:max-w-none max-lg:flex-1 max-lg:basis-0 max-lg:justify-center",
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
