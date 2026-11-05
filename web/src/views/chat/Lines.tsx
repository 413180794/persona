// The chat's lines other than an exchange (iOS ChatView.lineView): the wake that opens new work, the chip under an
// answer for background tasks that reported back (and its drawer), recaps (and their drawer, and the strip above the
// composer), model API errors as cards, and the header of a split exchange.
import { memo, useState } from "react";
import { ago, clock } from "../../lib/format";
import { retryable, type APIErrorInfo } from "../../lib/api-errors";
import { wakeSummary } from "../../lib/tasks";
import { wakeDrawer, workSegment, type ChatLine, type OpenRecap, type Turn } from "../../lib/turns";
import type { Item, SessionLive } from "../../lib/types";
import { Button, Collapse, copyText, cx, Dialog, Spinner } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Markdown } from "../../ui/Markdown";
import { Answer, SegmentView } from "./SegmentView";
import { CompactDivider, NoticeRow } from "./Steps";
import { t } from "../../i18n";
import { ScrollFold } from "./fold-scroll";

export interface LineProps {
  line: ChatLine;
  /** The screen's status line (contract §3), handed to the live line only. */
  status?: SessionLive | null;
  mid: string;
  sid: string;
  streaming?: string;
  expandFold: (f: Item) => Promise<void>;
  /** 重试 on an API error card (only on the last line, while the agent is not working). */
  onRetry?: () => void;
}

export const ChatLineView = memo(function ChatLineView({ line, status, mid, sid, streaming, expandFold, onRetry }: LineProps) {
  switch (line.kind) {
    case "segment":
      return (
        <SegmentView
          seg={line.seg}
          live={line.live}
          status={status}
          mid={mid}
          sid={sid}
          streaming={line.live ? streaming : undefined}
          expandFold={expandFold}
        />
      );
    case "notice":
      return <LineNotice item={line.item} />;
    case "wakes":
      return <WakeChip turn={line.turn} live={line.live} mid={mid} sid={sid} expandFold={expandFold} />;
    case "compact":
      return <CompactDivider item={line.item} />;
    case "recaps":
      return <RecapChip items={line.items} />;
    case "apiError":
      return <APIErrorCard info={line.info} count={line.count} at={line.item.ts} onRetry={onRetry && retryable(line.info) ? onRetry : undefined} />;
  }
});

/** A notice on a line of its own: a wake opening new work (a centered capsule), "已中断", a long local command's output. */
function LineNotice({ item }: { item: Item }) {
  const [open, setOpen] = useState(false);
  if (item.wake) {
    const w = wakeSummary(item);
    return (
      <div className="flex justify-center">
        <span className="flex max-w-full min-w-0 items-center gap-1.5 rounded-full bg-inset px-3 py-1 text-[12px] text-ink-2 shadow-hairline" title={clock(item.ts)}>
          <Icon name="bell" size={12} className={cx("shrink-0", w.failed ? "text-red" : "text-accent")} />
          <span className="min-w-0 truncate">{w.text}</span>
        </span>
      </div>
    );
  }
  const text = item.text ?? "";
  if (!text.includes("\n") && text.length <= 60) {
    return (
      <div className="flex justify-center">
        <span className="rounded-full bg-inset px-3 py-1 text-[12px] text-ink-2 shadow-hairline" title={clock(item.ts)}>
          {text}
        </span>
      </div>
    );
  }
  return (
    <button type="button" onClick={() => setOpen((o) => !o)} aria-expanded={open} className="flex w-full items-start gap-2 rounded-card bg-inset px-3 py-2 text-left shadow-hairline">
      <Icon name="info" size={13} className="mt-[3px] shrink-0 text-ink-3" />
      <span className={cx("min-w-0 text-[12.5px] leading-relaxed whitespace-pre-wrap text-ink-2", !open && "line-clamp-4")}>{text}</span>
    </button>
  );
}

// ── background tasks that reported back after the answer ──

function WakeChip({ turn, live, mid, sid, expandFold }: { turn: Turn; live: boolean; mid: string; sid: string; expandFold: (f: Item) => Promise<void> }) {
  const [open, setOpen] = useState(false);
  const notices = turn.wakes;
  const failed = notices.some((n) => wakeSummary(n).failed);
  const text = live ? t("chat-lines.wake_processing") : notices.length === 1 ? wakeSummary(notices[0]).text : t("chat-lines.wake_count", { count: notices.length });
  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label={t("chat-lines.wake_open")}
        className="-mt-2 flex w-fit max-w-full min-w-0 items-center gap-1.5 rounded-full bg-inset px-3 py-1 text-[12px] text-ink-2 shadow-hairline transition-colors hover:bg-hover-2"
      >
        {live ? <Spinner size={11} /> : <Icon name="bell" size={12} className={cx("shrink-0", failed ? "text-red" : "text-accent")} />}
        <span className="min-w-0 truncate">{text}</span>
        <Icon name="chevronRight" size={12} stroke={2.2} className="shrink-0 text-ink-3" />
      </button>
      <Dialog open={open} onClose={() => setOpen(false)} title={notices.length > 1 ? t("chat-lines.wake_count", { count: notices.length }) : t("chat-lines.wake_single")} width={640}>
        {open && <WakeDrawerBody turn={turn} live={live} mid={mid} sid={sid} expandFold={expandFold} />}
      </Dialog>
    </>
  );
}

/** Each wake (its reports, then what the agent answered to them), newest first; the steps in between fold into one
 *  work row at the end. */
function WakeDrawerBody({ turn, live, mid, sid, expandFold }: { turn: Turn; live: boolean; mid: string; sid: string; expandFold: (f: Item) => Promise<void> }) {
  const d = wakeDrawer(turn);
  const work = workSegment(`dw-${turn.id}`, d.process);
  return (
    <div className="flex max-h-[65vh] flex-col gap-5 overflow-y-auto pr-1">
      {d.groups.map((g) => (
        <section key={g.id} className="flex flex-col gap-2">
          <div className="flex flex-col gap-0.5">
            {g.notices.map((n) => (
              <NoticeRow key={n.id} item={n} mid={mid} sid={sid} />
            ))}
          </div>
          {g.answer && <Answer item={g.answer} mid={mid} sid={sid} />}
        </section>
      ))}
      {(work.steps.length > 0 || live) && <SegmentView seg={work} live={live} mid={mid} sid={sid} expandFold={expandFold} />}
    </div>
  );
}

// ── recaps ──

/** Claude Code's "while you were away" recap: one short line; the recap itself opens in a drawer. Several in a row are
 *  one line, the newest first in the drawer. */
function RecapChip({ items }: { items: Item[] }) {
  const [open, setOpen] = useState(false);
  const newest = items[items.length - 1];
  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label={items.length > 1 ? t("chat-lines.recap_open_many", { count: items.length }) : t("chat-lines.recap_open")}
        className="flex w-fit max-w-full items-center gap-1.5 rounded-full bg-inset px-3 py-1 text-[12px] whitespace-nowrap text-ink-2 shadow-hairline transition-colors hover:bg-hover-2"
      >
        <Icon name="retry" size={12} className="shrink-0 text-accent" />
        {t("chat-lines.recap")}
        {items.length > 1 && <span>{t("chat-lines.recap_times", { count: items.length })}</span>}
        {newest.ts && <span>· {ago(newest.ts)}</span>}
        <Icon name="chevronRight" size={12} stroke={2.2} className="shrink-0 text-ink-3" />
      </button>
      <RecapDrawer items={items} open={open} onClose={() => setOpen(false)} />
    </>
  );
}

function RecapDrawer({ items, open, onClose }: { items: Item[]; open: boolean; onClose: () => void }) {
  return (
    <Dialog open={open} onClose={onClose} title={items.length > 1 ? t("chat-lines.recap_title_many", { count: items.length }) : t("chat-lines.recap")} width={600}>
      <div className="flex max-h-[65vh] flex-col gap-4 overflow-y-auto pr-1">
        {[...items].reverse().map((it, n) => (
          <section key={it.id} className={cx("flex flex-col gap-2", n > 0 && "border-t border-line pt-4")}>
            {it.ts && (
              <span className="text-[11.5px] text-ink-3">
                {t(n === 0 && items.length > 1 ? "chat-lines.recap_newest_written" : "chat-lines.recap_written", { ago: ago(it.ts) })}
              </span>
            )}
            <Markdown text={it.text ?? ""} />
          </section>
        ))}
      </div>
    </Dialog>
  );
}

/** The open recap's line, the chat's last (#1360): the older recaps folded with it as their chip, then the newest in
 *  full. In the flow with the agent's words: it scrolls with them, and the chat's end includes it. */
export function OpenRecapLine({ open, onClose }: { open: OpenRecap; onClose: () => void }) {
  return (
    <div className="flex flex-col gap-2" data-open-recap>
      {open.earlier.length > 0 && <RecapChip items={open.earlier} />}
      <RecapStrip item={open.newest} onClose={onClose} />
    </div>
  );
}

/** The newest recap in full when you come back to a chat; a click (or typing) closes it back to its chip. */
function RecapStrip({ item, onClose }: { item: Item; onClose: () => void }) {
  return (
    <div className="group/recap relative flex gap-2.5 rounded-card bg-accent-tint px-3 py-2.5" style={{ animation: "fade-up 250ms ease-out both" }}>
      <span aria-hidden className="w-[3px] shrink-0 rounded-full bg-accent" />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <span className="flex items-center gap-1.5 text-[12px] font-semibold whitespace-nowrap text-ink">
          <Icon name="retry" size={12} className="text-accent" />
          {t("chat-lines.recap")}
          {item.ts && <span className="font-normal text-ink-3">· {ago(item.ts)}</span>}
        </span>
        <ScrollFold className="max-h-[6lh] text-[13px] leading-relaxed text-ink-2">
          <Markdown text={item.text ?? ""} small />
        </ScrollFold>
      </div>
      <button type="button" onClick={onClose} aria-label={t("chat-lines.recap_close")} title={t("chat-lines.close")} className="flex size-6 shrink-0 items-center justify-center rounded-[6px] text-ink-3 transition-colors hover:bg-hover-2 hover:text-ink">
        <Icon name="x" size={13} />
      </button>
    </div>
  );
}

const CLOSED_RECAPS = "jarvis.closedRecaps";

/** The recap closed per conversation (its id), kept in this browser. */
export function closedRecap(key: string): string | undefined {
  try {
    return (JSON.parse(localStorage.getItem(CLOSED_RECAPS) ?? "{}") as Record<string, string>)[key];
  } catch {
    return undefined;
  }
}

export function closeRecap(key: string, id: string) {
  let all: Record<string, string> = {};
  try {
    all = JSON.parse(localStorage.getItem(CLOSED_RECAPS) ?? "{}");
  } catch {
    /* start over */
  }
  delete all[key];
  all[key] = id;
  const keys = Object.keys(all);
  if (keys.length > 300) for (const k of keys.slice(0, keys.length - 200)) delete all[k];
  localStorage.setItem(CLOSED_RECAPS, JSON.stringify(all));
}

// ── model API errors ──

function APIErrorCard({ info, count, at, onRetry }: { info: APIErrorInfo; count: number; at?: string; onRetry?: () => void }) {
  const [open, setOpen] = useState(false);
  const sub = info.transient === true ? t("chat-lines.api_transient") : info.transient === false ? t("chat-lines.api_permanent") : "";
  return (
    <div className="flex flex-col gap-2.5 rounded-card bg-red-tint px-3.5 py-3 shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--red)_22%,transparent)]">
      <button type="button" onClick={() => setOpen((o) => !o)} aria-expanded={open} aria-label={t("chat-lines.api_error", { reason: info.reason })} className="flex min-w-0 items-start gap-2 text-left">
        <Icon name="info" size={15} stroke={2.2} className="mt-[2px] shrink-0 text-red" />
        <span className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="text-[14px] font-semibold text-ink">{info.reason}</span>
          {sub && <span className="text-[12px] text-ink-2">{sub}</span>}
        </span>
        <span className="flex shrink-0 items-center gap-1">
          {info.status !== undefined && <span className="rounded-[4px] bg-red-tint px-1 font-mono text-[11px] font-medium text-red">{info.status}</span>}
          {count > 1 && <span className="rounded-[4px] bg-field px-1 text-[11px] font-medium whitespace-nowrap text-ink-2">{t("chat-lines.api_repeated", { count })}</span>}
          <Icon name="chevron" size={13} stroke={2.2} className="text-ink-3 transition-transform duration-200" style={{ transform: open ? "rotate(180deg)" : "none" }} />
        </span>
      </button>
      <Collapse open={open}>
        <div className="flex flex-col gap-1.5 pt-0.5">
          {info.requestId && <Field label={t("chat-lines.request_id")} value={info.requestId} />}
          {info.session && <Field label="Session" value={info.session} />}
          {at && <Field label={t("chat-lines.time")} value={clock(at)} copy={false} />}
          <pre className="max-h-56 overflow-auto rounded-[8px] bg-surface px-2.5 py-2 font-mono text-[11.5px] leading-[1.6] whitespace-pre-wrap text-ink-2 shadow-hairline [overflow-wrap:anywhere]">{info.raw}</pre>
          <button type="button" onClick={() => copyText(info.raw)} className="flex w-fit items-center gap-1 text-[12px] font-medium whitespace-nowrap text-accent-ink hover:underline">
            <Icon name="copy" size={12} />
            {t("chat-lines.copy_raw")}
          </button>
        </div>
      </Collapse>
      {onRetry && (
        <div>
          <Button icon="retry" onClick={onRetry}>
            {t("chat-lines.retry")}
          </Button>
        </div>
      )}
    </div>
  );
}

function Field({ label, value, copy = true }: { label: string; value: string; copy?: boolean }) {
  return (
    <div className="flex min-w-0 items-center gap-2 text-[12px]">
      <span className="w-14 shrink-0 text-ink-3">{label}</span>
      <span className="min-w-0 truncate font-mono text-ink-2">{value}</span>
      {copy && (
        <button type="button" onClick={() => copyText(value)} aria-label={t("chat-lines.copy_field", { label })} className="flex size-5 shrink-0 items-center justify-center rounded-[5px] text-ink-3 hover:bg-hover-2 hover:text-ink">
          <Icon name="copy" size={11} />
        </button>
      )}
    </div>
  );
}
