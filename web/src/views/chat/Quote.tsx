// 引用, drawn instead of `> ` lines (WeChat / Slack's reply quote): above the input, a bar with who said it and a
// two-line plain summary, × to drop it; in a sent bubble, the leading `> ` block as a three-line card that opens to
// the whole passage. The message itself still carries the `> ` block (lib/drafts.ts withQuoteBlock / splitQuote).
import { useLayoutEffect, useRef, useState } from "react";
import type { DraftQuote } from "../../lib/drafts";
import { clock } from "../../lib/format";
import { plainMarkdown } from "../../lib/plain-markdown";
import { cx, EASE } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Markdown } from "../../ui/Markdown";

/** 「引用 Persona · 16:45」: who said it and when, as far as is known. */
export function quoteSource(q: DraftQuote): string {
  return ["引用" + (q.from ? ` ${q.from}` : ""), clock(q.ts)].filter(Boolean).join(" · ");
}

/** The quote waiting above the composer's input. */
export function QuoteBar({ quote, onRemove }: { quote: DraftQuote; onRemove: () => void }) {
  const summary = plainMarkdown(quote.text) || quote.text.trim();
  return (
    <div
      className="flex min-w-0 items-start gap-2 rounded-[10px] bg-inset py-1.5 pr-1 pl-2 shadow-hairline"
      title={quote.text}
      aria-label="引用的消息"
      style={{ animation: `pop-in 180ms ${EASE} both` }}
    >
      <span aria-hidden className="w-[3px] shrink-0 self-stretch rounded-full bg-accent" />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5 py-px">
        <span className="flex min-w-0 items-center gap-1 text-[11.5px] font-medium whitespace-nowrap text-ink-3">
          <Icon name="quote" size={12} className="shrink-0" />
          <span className="min-w-0 truncate">{quoteSource(quote)}</span>
        </span>
        <span className="line-clamp-2 text-[12.5px] leading-[1.5] text-ink-2 [overflow-wrap:anywhere]">{summary}</span>
      </div>
      <button
        type="button"
        aria-label="取消引用"
        title="取消引用"
        onClick={onRemove}
        className="flex size-6 shrink-0 items-center justify-center rounded-full text-ink-3 transition-colors hover:bg-hover-2 hover:text-ink-2"
      >
        <Icon name="x" size={12} stroke={2.4} />
      </button>
    </div>
  );
}

/** A sent message's leading `> ` block, at the top of its bubble: three plain lines, a click opens the whole passage, as Markdown. */
export function QuoteBlock({ text }: { text: string }) {
  const clampRef = useRef<HTMLSpanElement>(null);
  const [long, setLong] = useState(false);
  const [open, setOpen] = useState(false);
  const summary = plainMarkdown(text) || text;
  useLayoutEffect(() => {
    const el = clampRef.current;
    if (el && !open) setLong(el.scrollHeight > el.clientHeight + 1);
  }, [summary, open]);
  return (
    <div className="flex min-w-0 gap-2 rounded-[8px] bg-ink/[0.05] py-1.5 pr-2.5 pl-2 whitespace-normal" data-quote>
      <span aria-hidden className="w-[3px] shrink-0 self-stretch rounded-full bg-ink/20" />
      <div className="flex min-w-0 flex-1 flex-col items-start gap-0.5">
        {open ? (
          <div className="max-h-[320px] w-full overflow-y-auto text-ink-2 [overflow-wrap:anywhere]">
            <Markdown text={text} small compact />
          </div>
        ) : (
          // the words open on a click too; the button under them is the keyboard's way
          <span
            ref={clampRef}
            onClick={long ? () => setOpen(true) : undefined}
            className={cx("line-clamp-3 text-[13px] leading-[1.55] text-ink-2 [overflow-wrap:anywhere]", long && "cursor-pointer")}
          >
            {summary}
          </span>
        )}
        {long && (
          <button
            type="button"
            onClick={() => setOpen((o) => !o)}
            aria-expanded={open}
            className="-ml-1 rounded-[6px] px-1 text-[12px] font-medium whitespace-nowrap text-ink-3 transition-colors hover:bg-hover-2 hover:text-ink-2"
          >
            {open ? "收起引用" : "展开引用"}
          </button>
        )}
      </div>
    </div>
  );
}
