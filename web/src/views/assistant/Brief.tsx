// Actions shared by the live owner review rows and the on-demand briefing card.
import { openTask } from "../../lib/handoff";
import { type ReactNode, useRef, useState } from "react";
import { errorText } from "../../lib/api";
import { putTodo, refreshTodos } from "../../lib/store";
import { updateTodo } from "../../lib/todos";
import { acceptBrief, type BriefRow } from "../../lib/feed";
import { shortDuration } from "../../lib/format";
import { t } from "../../i18n";
import { Button, cx, toast } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";

/** A section's rows shown at once (the hub cuts the lists as its card does; the totals say how many there were). */
export function Section({ icon, tint, title, children }: { icon: IconName; tint: string; title: string; children: ReactNode }) {
  return (
    <section className="border-t border-line py-2 first:border-t-0" data-brief-section>
      <div role="heading" aria-level={3} className="mb-1 flex items-center gap-2 text-[12.5px] font-medium text-ink-2">
        <span className={cx("flex size-6 items-center justify-center rounded-control", tint)}>
          <Icon name={icon} size={13} />
        </span>
        {title}
      </div>
      <div className="flex flex-col">{children}</div>
    </section>
  );
}

/** What a row's meta line is made of: numbers and text only. A row typed as BriefRow cannot carry an object here: a
 *  to-do's `ask.from` is one, and spreading the ask into a row is a type error (#11794); rows off the wire go through
 *  lib/feed `briefRow`, which drops a field that is not text. */
export function meta(row: Pick<BriefRow, "waited_s" | "nudged" | "from">): string {
  const parts: string[] = [t("assistant-brief.waited", { wait: shortDuration(row.waited_s * 1000) })];
  if (row.nudged) parts.push(t("assistant-brief.nudged"));
  if (row.from) parts.push(row.from);
  return parts.join(" · ");
}

export const briefRowMeta = meta;
/** What the Accept button of a task says: it asked, the hub took it, or the round is already over. */
type Accepting = "idle" | "busy" | "finishing" | "finished" | "accepted" | "recorded" | "handled";

/** What the button of a row to verify says: Accept a deployed task, Got it a conclusion, Take recommendation a decision. */
function acceptLabel(row: BriefRow): { label: string; title: string } {
  if (row.outcome === "decide") return { label: t("assistant-brief.take"), title: t("assistant-brief.take_row", { title: row.title }) };
  if (row.outcome) return { label: t("assistant-brief.got_it"), title: t("assistant-brief.accept_row", { title: row.title }) };
  return { label: t("assistant-brief.accept"), title: t("assistant-brief.accept_row", { title: row.title }) };
}

export function AcceptRow({ row, stacked = false, admission, onBusyChange, blocked = false, secondary, hideTitle = false }: { row: BriefRow; stacked?: boolean; admission?: { current: boolean }; onBusyChange?: (busy: boolean) => void; blocked?: boolean; secondary?: ReactNode; hideTitle?: boolean }) {
  const [state, setState] = useState<Accepting>("idle");
  // Admit either action synchronously, before React disables the other button.
  const ownAdmission = useRef(false);
  const admitted = admission ?? ownAdmission;
  const button = acceptLabel(row);
  // the primary button (and the settled word) on its own full-width line while the row is narrow
  const full = stacked ? "w-full" : "@max-md:w-full";
  const deciding = row.outcome === "decide";
  const settled = state === "accepted" || state === "recorded" || state === "handled" || state === "finished";
  // A decision with nothing recommended can still be finished, but has nothing to take.
  const takes = !deciding || !!row.recommend;
  const accept = () => {
    if (!row.asked_at || admitted.current || state !== "idle") return;
    admitted.current = true;
    onBusyChange?.(true);
    setState("busy");
    acceptBrief(row.id, row.asked_at).then(
      (res) => setState(res),
      (e) => {
        admitted.current = false;
        onBusyChange?.(false);
        setState("idle");
        toast(t("assistant-brief.accept_failed", { error: errorText(e) }), "error");
      },
    );
  };
  const finish = () => {
    if (!stacked || !row.asked_at || admitted.current || state !== "idle") return;
    admitted.current = true;
    onBusyChange?.(true);
    setState("finishing");
    // User completion only: unlike /accept, PATCH never routes the recommendation to the Agent.
    updateTodo(row.id, { done: true }).then(
      (todo) => {
        setState("finished");
        // putTodo merges optional fields; explicitly clear the ask omitted by a completed todo.
        putTodo({ ...todo, ask: todo.ask });
        void refreshTodos();
        toast(t("assistant-brief.finished"), "ok");
      },
      (e) => {
        admitted.current = false;
        onBusyChange?.(false);
        setState("idle");
        toast(t("assistant-brief.finish_failed", { error: errorText(e) }), "error");
      },
    );
  };
  return (
    <div className="@container" data-brief-row="accept">
    {/* Narrow (the tray, a phone's chat column; `stacked` always): the words take the whole width, the secondary buttons
        one row under them and the primary its own full-width line, so the words are never a four-letter column (#5569). */}
    <div className={cx("flex items-center", stacked ? "flex-wrap gap-x-2 gap-y-3 py-0.5" : "gap-3 py-1.5 @max-md:flex-wrap @max-md:gap-y-2")}>
      {/* stacked, top to bottom: the task, what is asked, what is recommended, how long it has waited; the buttons
          follow after a larger gap, so the words and the actions read as two groups */}
      <div className={cx("min-w-0", stacked ? "flex w-full flex-col gap-1.5" : "flex-1 @max-md:w-full @max-md:flex-none")}>
        {!hideTitle && <div className={cx("font-medium [overflow-wrap:anywhere]", stacked ? "text-sm font-semibold" : "text-[13.5px]")}>{row.title}</div>}
        {row.verdict && (
          <div className={cx("[overflow-wrap:anywhere]", stacked ? "text-sm leading-relaxed" : "text-[12.5px]")} data-brief-verdict={row.outcome ?? "deployed"}>
            {row.verdict}
          </div>
        )}
        {row.detail && <div className="text-label text-ink-2 [overflow-wrap:anywhere]">{row.detail}</div>}
        {row.recommend && <div className="text-label text-ink-2 [overflow-wrap:anywhere]" data-brief-recommend>{t("assistant-brief.recommend", { option: row.recommend })}</div>}
        <div className={cx("text-label", stacked ? "text-ink-3" : "text-ink-2")} data-brief-meta>{briefRowMeta(row)}</div>
      </div>
      <Button title={t("assistant-brief.open_row", { title: row.title })} onClick={() => openTask(row.id)}>
        {t("assistant-brief.open")}
      </Button>
      {/* Only the live Pending row can finish a task; historical brief snapshots must not end a newer run. */}
      {stacked && deciding && !settled && (
        <Button busy={state === "finishing"} disabled={blocked || !row.asked_at || state === "busy"} title={t("assistant-brief.finish_row", { title: row.title })} onClick={finish}>
          {t("assistant-brief.finish")}
        </Button>
      )}
      {!settled && secondary}
      {settled ? (
        <span className={cx("shrink-0 text-[12.5px] text-ink-2", full)} data-brief-accepted={state}>
          {t(state === "finished" ? "assistant-brief.finished" : state === "handled" ? "assistant-brief.handled" : state === "recorded" ? "assistant-brief.recorded_not_delivered" : deciding ? "assistant-brief.taken" : row.outcome ? "assistant-brief.noted" : "assistant-brief.accepted")}
        </span>
      ) : takes ? (
        <Button tone="primary" className={full} busy={state === "busy"} disabled={blocked || !row.asked_at || state === "finishing"} title={button.title} onClick={accept}>
          {button.label}
        </Button>
      ) : null}
    </div>
    </div>
  );
}
