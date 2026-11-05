// The terminal's find bar (docs/terminal.md「搜索」): a small bar over the top of the panel with the field, how many
// matches there are, previous / next, 区分大小写 and close. Enter steps to the next match, Shift+Enter to the previous,
// Esc closes. The state is lib/terminal-search.ts's reducer; what is searched is the feed's.
import { useEffect, useRef } from "react";
import { findLabel, type FindState } from "../../../lib/terminal-search";
import { cx, EASE, IconButton } from "../../../ui/atoms";
import { Icon } from "../../../ui/icons";
import type { FeedView, TerminalFeed } from "./feed";

export function FindBar({
  feed,
  v,
  state,
  onQuery,
  onToggleCase,
  onClose,
}: {
  feed: TerminalFeed;
  v: FeedView;
  state: FindState;
  onQuery: (query: string) => void;
  onToggleCase: () => void;
  onClose: () => void;
}) {
  const input = useRef<HTMLInputElement>(null);

  // Opening (or ⌘F again) puts the cursor in the field with its text selected, as a browser's bar does.
  useEffect(() => {
    input.current?.focus();
    input.current?.select();
  }, []);

  // The search follows the field and the case switch.
  useEffect(() => {
    feed.find(state.query, state.caseSensitive, 0);
  }, [feed, state.query, state.caseSensitive]);

  const step = (dir: 1 | -1) => feed.find(state.query, state.caseSensitive, dir);
  const label = findLabel(state.query, v.find);
  const none = Boolean(state.query) && (v.find?.count ?? 0) === 0;

  return (
    <div
      role="search"
      aria-label="在终端里搜索"
      className="pointer-events-auto absolute top-2 right-2 z-20 flex h-9 max-w-[calc(100%-1rem)] items-center gap-0.5 rounded-control bg-surface py-1 pr-1 pl-2.5 shadow-overlay"
      style={{ animation: `pop-in 160ms ${EASE} both`, transformOrigin: "top right" }}
    >
      <Icon name="search" size={14} className="mr-1.5 shrink-0 text-ink-3" />
      <input
        ref={input}
        value={state.query}
        onChange={(e) => onQuery(e.target.value)}
        onKeyDown={(e) => {
          if (e.nativeEvent.isComposing) return;
          if (e.key === "Enter") {
            e.preventDefault();
            step(e.shiftKey ? -1 : 1);
          } else if (e.key === "Escape") {
            e.preventDefault();
            e.stopPropagation();
            onClose();
          }
        }}
        placeholder="搜索终端"
        aria-label="搜索终端"
        spellCheck={false}
        autoComplete="off"
        autoCapitalize="off"
        className="min-w-0 flex-1 bg-transparent text-[12.5px] text-ink outline-none placeholder:text-ink-3 @sm:w-40 @sm:flex-none"
      />
      <span
        aria-live="polite"
        className={cx("min-w-10 shrink-0 text-right font-mono text-[11.5px] whitespace-nowrap tabular-nums", none ? "text-red" : "text-ink-3")}
      >
        {label}
      </span>
      <IconButton icon="up" label="上一个（Shift+Enter）" size={26} disabled={!state.query} onClick={() => step(-1)} />
      <IconButton icon="arrowDown" label="下一个（Enter）" size={26} disabled={!state.query} onClick={() => step(1)} />
      <button
        type="button"
        role="switch"
        aria-checked={state.caseSensitive}
        aria-label="区分大小写"
        title="区分大小写"
        onClick={onToggleCase}
        className={cx(
          "flex h-[26px] w-[26px] shrink-0 items-center justify-center rounded-[8px] font-mono text-[11.5px] font-medium whitespace-nowrap transition-colors duration-150",
          state.caseSensitive ? "bg-accent-tint text-accent-ink" : "text-ink-3 hover:bg-hover hover:text-ink",
        )}
      >
        Aa
      </button>
      <IconButton icon="x" label="关闭搜索（Esc）" size={26} onClick={onClose} />
    </div>
  );
}
