import { t } from "../../i18n";
import type { FeedItem } from "../../lib/feed";
import { checkCount, checklist, latestReview, reviewBadge, reviewVerdictLabel, type CheckMark, type ReviewTone } from "../../lib/supervision";
import type { Todo } from "../../lib/todos";
import { cx } from "../../ui/atoms";
import { Icon } from "../../ui/icons";

// How a supervised task's review stands (#3124, docs/unified-entry.md "Supervision"): a chip and the count of criteria met
// on its card, the checklist on its page, a line in the conversation for each review.

const TONE: Record<ReviewTone, string> = {
  accent: "bg-accent/12 text-accent",
  red: "bg-red/12 text-red",
  orange: "bg-orange/14 text-orange",
  green: "bg-green/12 text-green",
  muted: "bg-field text-ink-2",
};

/** The review's chip, with the criteria met; nothing for a task that is not supervised. */
export function ReviewBadge({ todo, className }: { todo: Todo; className?: string }) {
  const badge = reviewBadge(todo);
  if (!badge) return null;
  const count = checkCount(todo.supervision);
  return (
    <span data-testid="review-badge" className={cx("inline-flex h-5 shrink-0 items-center gap-1 whitespace-nowrap rounded-md px-1.5 text-[11.5px] font-semibold", TONE[badge.tone], className)}>
      <Icon name="target" size={11} stroke={2.2} />
      {badge.label}
      {count && <span className="font-medium tabular-nums opacity-80">· {count}</span>}
    </span>
  );
}

const MARK: Record<CheckMark, { icon: "check" | "x" | "clock"; className: string; label: () => string }> = {
  met: { icon: "check", className: "text-green", label: () => t("assistant-tasks.review_met") },
  missing: { icon: "x", className: "text-red", label: () => t("assistant-tasks.review_missing") },
  open: { icon: "clock", className: "text-ink-3", label: () => t("assistant-tasks.review_open") },
};

/** The task's criteria, each as its latest review found it, and what the supervisor said last. */
export function ReviewChecklist({ todo }: { todo: Todo }) {
  const sup = todo.supervision;
  if (!sup) return null;
  const list = checklist(sup);
  const review = latestReview(sup);
  return (
    <section data-testid="review-checklist" className="mt-4 rounded-control bg-inset p-3.5 shadow-hairline">
      <div className="flex items-center gap-2">
        <span className="text-[12.5px] font-semibold text-ink-2">{t("assistant-tasks.review_title")}</span>
        <ReviewBadge todo={todo} />
        <span className="flex-1" />
        {sup.expect_minutes ? <span className="text-[12px] text-ink-3">{t("assistant-tasks.review_expect", { minutes: sup.expect_minutes })}</span> : null}
      </div>
      {list.length === 0 ? (
        <p className="mt-2 text-[12.5px] text-ink-3">{t("assistant-tasks.review_none")}</p>
      ) : (
        <ul className="mt-2 flex flex-col gap-1.5">
          {list.map((c) => {
            const m = MARK[c.mark];
            return (
              <li key={c.criterion} className="flex items-start gap-2 text-[13px]">
                <span role="img" aria-label={m.label()} title={m.label()} className={cx("mt-0.5 shrink-0", m.className)}>
                  <Icon name={m.icon} size={13} stroke={2.4} />
                </span>
                <span className="flex min-w-0 flex-col">
                  <span className="text-ink">{c.criterion}</span>
                  {c.evidence && <span className="text-[12px] text-ink-3 select-text">{c.evidence}</span>}
                </span>
              </li>
            );
          })}
        </ul>
      )}
      {review?.note && <p className="mt-2 text-[12.5px] leading-[1.6] text-ink-2 select-text">{t("assistant-tasks.review_note", { note: review.note })}</p>}
    </section>
  );
}

/** A review in the conversation (feed kind "review"): the verdict and the supervisor's note. */
export function ReviewLine({ item, title, onOpenTask }: { item: FeedItem; title: string; onOpenTask?: () => void }) {
  const body = (item.body ?? {}) as { verdict?: string; round?: number; note?: string };
  return (
    <div data-testid="review-line" className="flex max-w-full min-w-0 items-center gap-1.5 self-start rounded-full bg-field px-3 py-1.5 text-[12px] text-ink-2">
      <Icon name="target" size={12} className="shrink-0" />
      {onOpenTask ? (
        <button type="button" onClick={onOpenTask} className="max-w-[45%] shrink-0 truncate text-left hover:text-ink hover:underline">
          {title}
        </button>
      ) : (
        <span className="max-w-[45%] shrink-0 truncate">{title}</span>
      )}
      <span className="shrink-0 whitespace-nowrap font-semibold">· {reviewVerdictLabel(body.verdict ?? "", body.round)}</span>
      {body.note && <span className="min-w-0 flex-1 truncate">{body.note}</span>}
    </div>
  );
}
