// Comments on lines of a file or a diff (#2437, Moshi's code comments): the line number turns into a + when the pointer
// is over the row, a click (or a drag, or shift-click, over several) picks the lines, and a box under the code takes
// the note: ⌘↵ / "Add" puts it above the message box as a comment card with the location and the code, Esc cancels. The
// box is pinned under the list rather than set between its rows: the lists only draw the rows on screen, at a fixed height.
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { lineRange, MAX_COMMENT_CHARS } from "../../../lib/comments";
import { Button, Kbd } from "../../../ui/atoms";
import { Icon } from "../../../ui/icons";
import { NumberDot } from "../../chat/CommentCard";
import { t } from "../../../i18n";
import { composing } from "../../../lib/send-history";

export interface LinePick {
  /** The lines picked, once the pointer let go. */
  range: { from: number; to: number } | null;
  /** The lines being swept right now (the box waits for the pointer). */
  sweeping: { from: number; to: number } | null;
  down: (n: number, e: React.PointerEvent) => void;
  enter: (n: number) => void;
  clear: () => void;
}

/** Picking lines by their numbers: pointer down starts (shift extends), moving over other rows while it is down sweeps. */
export function useLinePick(enabled: boolean): LinePick {
  const [range, setRange] = useState<{ from: number; to: number } | null>(null);
  const [sweeping, setSweeping] = useState<{ from: number; to: number } | null>(null);
  const anchor = useRef<number | null>(null);
  const drag = useRef(false);
  useEffect(() => {
    const up = () => {
      if (!drag.current) return;
      drag.current = false;
      setSweeping((s) => {
        if (s) setRange(s);
        return null;
      });
    };
    window.addEventListener("pointerup", up);
    window.addEventListener("pointercancel", up);
    return () => {
      window.removeEventListener("pointerup", up);
      window.removeEventListener("pointercancel", up);
    };
  }, []);
  return {
    range,
    sweeping,
    down: (n, e) => {
      if (!enabled || e.button !== 0) return;
      e.preventDefault(); // no text selection while sweeping the numbers
      if (e.shiftKey && anchor.current !== null) setSweeping(lineRange(anchor.current, n));
      else {
        anchor.current = n;
        setSweeping({ from: n, to: n });
      }
      setRange(null);
      drag.current = true;
    },
    enter: (n) => {
      if (drag.current && anchor.current !== null) setSweeping(lineRange(anchor.current, n));
    },
    clear: () => {
      anchor.current = null;
      setRange(null);
      setSweeping(null);
    },
  };
}

/** A row's number: the digits, a + under the pointer, the card's number where a comment waits. */
export function GutterNumber({ n, at = n, pick, marked, enabled, className, style }: { n: number | undefined; /** What the pick counts by when it is not the number itself (a diff counts rows). */ at?: number; pick: LinePick; marked?: number; enabled: boolean; className?: string; style?: React.CSSProperties }) {
  if (n === undefined || at === undefined || !enabled) return <span className={className} style={style}>{n ?? ""}</span>;
  return (
    <button
      type="button"
      tabIndex={-1}
      aria-label={marked ? t("chat-comments.gutter_marked", { n: marked }) : t("chat-comments.gutter_add", { line: n })}
      title={t("chat-comments.gutter_add", { line: n })}
      onPointerDown={(e) => pick.down(at, e)}
      onPointerEnter={() => pick.enter(at)}
      className={`${className ?? ""} relative flex cursor-pointer items-center justify-end`}
      style={style}
      data-gutter={n}
    >
      <span className="[@media(hover:hover)]:group-hover/row:invisible">{marked ? <NumberDot n={marked} /> : n}</span>
      <span aria-hidden className="absolute inset-y-0 right-1.5 hidden items-center text-accent-ink [@media(hover:hover)]:group-hover/row:flex">
        <Icon name="plus" size={13} stroke={2.4} />
      </span>
    </button>
  );
}

/** The box that takes the note for the lines picked, under the code. */
export function LineBox({ where, onAdd, onCancel }: { where: string; onAdd: (body: string) => void; onCancel: () => void }) {
  const [body, setBody] = useState("");
  const field = useRef<HTMLTextAreaElement>(null);
  useLayoutEffect(() => {
    field.current?.focus({ preventScroll: true });
  }, [where]);
  useLayoutEffect(() => {
    const el = field.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 120)}px`;
  }, [body]);
  const mod = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘↵" : "Ctrl ↵";
  return (
    <div className="flex shrink-0 flex-col gap-2 border-t border-line bg-surface px-3 py-2.5" data-line-box>
      <div className="flex min-w-0 items-center gap-1.5 text-[12px] font-medium text-ink-3">
        <Icon name="chat" size={13} className="shrink-0" />
        <span className="min-w-0 truncate font-mono text-[11.5px]" title={where}>
          {t("chat-comments.box_title", { where })}
        </span>
      </div>
      <textarea
        ref={field}
        value={body}
        rows={2}
        maxLength={MAX_COMMENT_CHARS}
        aria-label={t("chat-comments.box_label")}
        placeholder={t("chat-comments.placeholder")}
        onChange={(e) => setBody(e.target.value)}
        onKeyDown={(e) => {
          if (composing(e.nativeEvent)) return;
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
            e.preventDefault();
            onAdd(body.trim());
          } else if (e.key === "Escape") {
            // not the page's Esc (it stops a running agent)
            e.preventDefault();
            e.stopPropagation();
            onCancel();
          }
        }}
        className="w-full resize-none rounded-[8px] bg-inset px-2.5 py-1.5 text-[13px] leading-[1.5] text-ink shadow-hairline outline-none placeholder:text-ink-3 focus:shadow-[0_0_0_1.5px_var(--accent)]"
      />
      <div className="flex items-center justify-end gap-2">
        <Button tone="quiet" onClick={onCancel}>
          {t("chat-comments.box_cancel")}
        </Button>
        <Button tone="accent" onClick={() => onAdd(body.trim())}>
          {t("chat-comments.box_add")}
          <Kbd>{mod}</Kbd>
        </Button>
      </div>
    </div>
  );
}
