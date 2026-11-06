// The agent's plan (TodoWrite) as a card in the chat (cindy's InlinePlanCard), open by default: done / total and the
// step it is on in its header, a 2 px progress bar under it when folded. While the card is out of view, a small
// "Step N / M" pill above the composer (PlanPill) brings it back.
import { useEffect, useState, type RefObject } from "react";
import type { Segment } from "../../lib/turns";
import { isPlanStep, planProgress, planSteps, planTodos, type Todo } from "../../lib/work-groups";
import { cx, Spinner } from "../../ui/atoms";
import { Icon } from "../../ui/icons";

/** The chat's newest plan: the latest TodoWrite of two or more items in its newest exchange that has one. */
export function latestPlan(segs: Segment[]): { id: string; todos: Todo[] } | undefined {
  for (let i = segs.length - 1; i >= 0; i--) {
    const s = planSteps(segs[i].steps).findLast(isPlanStep);
    if (s?.kind === "tool") return { id: s.id, todos: planTodos(s) };
  }
  return undefined;
}

/** Whether the plan card `id` is drawn but out of the scroll view (scrolled past, or not reached). */
export function usePlanOutOfView(scroller: RefObject<HTMLDivElement | null>, id?: string): boolean {
  const [out, setOut] = useState(false);
  useEffect(() => {
    const sc = scroller.current;
    const body = sc?.firstElementChild;
    if (!sc || !body || !id) return setOut(false);
    const check = () => {
      const el = sc.querySelector(`[data-plan-id="${CSS.escape(id)}"]`);
      if (!el) return setOut(false);
      const r = el.getBoundingClientRect();
      const v = sc.getBoundingClientRect();
      setOut(r.bottom < v.top || r.top > v.bottom);
    };
    check();
    sc.addEventListener("scroll", check, { passive: true });
    const ro = new ResizeObserver(check);
    ro.observe(body);
    return () => {
      sc.removeEventListener("scroll", check);
      ro.disconnect();
    };
  }, [scroller, id]);
  return out;
}

export function PlanCard({ id, todos, live }: { id?: string; todos: Todo[]; live?: boolean }) {
  const [open, setOpen] = useState(true);
  if (!todos.length) return null;
  const p = planProgress(todos);
  return (
    <div className="flex w-full" data-plan-card data-plan-id={id}>
      <div className="max-w-full min-w-0 overflow-hidden rounded-[12px] border border-line bg-surface">
        <button type="button" aria-expanded={open} onClick={() => setOpen((o) => !o)} className="flex w-full min-w-0 cursor-pointer items-center gap-2 px-[14px] py-[10px] transition-opacity select-none hover:opacity-80 [@media(pointer:coarse)]:min-h-10">
          <Icon name={open ? "chevron" : "chevronRight"} size={14} stroke={2.2} className="shrink-0 text-tool-ink" />
          <Icon name="todo" size={16} className="shrink-0 text-ink" />
          <span className="shrink-0 font-mono text-[13px] leading-none font-medium text-ink tabular-nums">
            {p.done}/{p.total}
          </span>
          <span className="shrink-0 text-[13px] leading-none text-tool-ink">·</span>
          <span className="mt-px min-w-0 truncate text-[13px] leading-none text-ink">{p.current}</span>
        </button>
        {!open && (
          <div className="h-[2px] w-full bg-line" data-plan-progress>
            <div className="h-full bg-ink transition-[width] duration-200 motion-reduce:transition-none" style={{ width: `${p.total ? (p.done / p.total) * 100 : 0}%` }} />
          </div>
        )}
        {open && (
          <div className="border-t border-line px-[14px] pt-[10px] pb-[12px]">
            <div className="flex flex-col gap-[2px]">
              {todos.map((t, i) => (
                <div key={`${t.content}:${i}`} className="flex min-h-[30px] items-center gap-[10px]" data-todo={t.status}>
                  {t.status === "completed" ? (
                    <span className="flex size-[18px] shrink-0 items-center justify-center rounded-full border-[1.5px] border-tool-ink text-tool-ink">
                      <Icon name="check" size={11} stroke={2.6} />
                    </span>
                  ) : t.status === "in_progress" ? (
                    <span className="flex size-[18px] shrink-0 items-center justify-center">{live ? <Spinner size={16} /> : <span className="size-[16px] rounded-full border-[1.5px] border-ink border-r-transparent" />}</span>
                  ) : (
                    <span className="size-[18px] shrink-0 rounded-full border-[1.5px] border-ink" />
                  )}
                  <span className={cx("min-w-0 text-[13px] leading-[1.45] break-words", t.status === "completed" ? "text-tool-ink" : t.status === "in_progress" ? "font-medium text-ink" : "text-ink")}>{t.content}</span>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

/** Above the composer while the latest plan is out of view: a ring with how far along it is, Step N / M. */
export function PlanPill({ todos, onClick }: { todos: Todo[]; onClick: () => void }) {
  const p = planProgress(todos);
  const r = 7;
  const c = 2 * Math.PI * r;
  return (
    <div className="flex justify-center">
      <button type="button" onClick={onClick} className="flex items-center gap-2 rounded-full border border-line bg-surface px-[14px] py-[6px] whitespace-nowrap transition-opacity select-none hover:opacity-80" data-plan-pill style={{ animation: "fade-in 150ms ease-out both" }}>
        {p.allDone ? (
          <Icon name="check" size={14} stroke={2.4} className="shrink-0 text-ink" />
        ) : (
          <svg width={16} height={16} viewBox="0 0 16 16" aria-hidden className="shrink-0">
            <circle cx={8} cy={8} r={r} fill="none" stroke="var(--tool-ink)" strokeWidth={2} opacity={0.45} />
            <circle cx={8} cy={8} r={r} fill="none" stroke="var(--ink)" strokeWidth={2} strokeLinecap="round" strokeDasharray={c} strokeDashoffset={c * (1 - (p.total ? p.step / p.total : 0))} transform="rotate(-90 8 8)" />
          </svg>
        )}
        <span className="text-[13px] leading-none text-ink tabular-nums">
          Step {p.step} / {p.total}
        </span>
      </button>
    </div>
  );
}
