// The pill over the page once the socket has dropped (after the app's ConnectionTitle): which phase ("No network" /
// "Connecting…" / "Hub is updating…" / "Can't reach the hub, retrying…"), and on a click the details (why, the next try, since when) with
// "Retry now". The page that never reached the hub has its own empty state (App.tsx Offline) instead.
import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { BREATHING, connFacts, connPhase, phaseExplanation, phaseTitle, phaseTone, type ConnLink, type ConnPhase } from "../lib/connection";
import { reconnectNow, useStore } from "../lib/store";
import type { Conn } from "../lib/store";
import { Button, cx, EASE, useNow } from "../ui/atoms";

/** The dot's color and whether it breathes: still trying vs given up for now. */
function Dot({ phase }: { phase: ConnPhase }) {
  const tone = phaseTone(phase);
  return <span aria-hidden className={cx("size-1.5 shrink-0 rounded-full", tone.color)} style={tone.breathing ? BREATHING : undefined} />;
}

export function ConnectionPill() {
  const conn = useStore((s) => s.conn);
  const link = useStore((s) => s.link);
  const phase = connPhase(conn, link);
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const down = (e: PointerEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const key = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    addEventListener("pointerdown", down, true);
    addEventListener("keydown", key);
    return () => {
      removeEventListener("pointerdown", down, true);
      removeEventListener("keydown", key);
    };
  }, [open]);

  const title = phaseTitle(phase);
  return (
    <div ref={ref} className="pointer-events-auto relative flex flex-col items-center">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={t("lib-connection.show_details", { state: title })}
        data-connection-pill
        className="flex items-center gap-2 rounded-full bg-tooltip-bg px-3 py-1.5 text-[12px] whitespace-nowrap text-tooltip-fg shadow-overlay transition-[opacity,transform] duration-150 hover:opacity-90 active:scale-[0.97]"
      >
        <Dot phase={phase} />
        {title}
      </button>
      {open && <ConnectionDetails conn={conn} link={link} />}
    </div>
  );
}

/** Why the pill says what it says, the next try and since when, and "Retry now". `now` fixed: tests and screenshots. */
export function ConnectionDetails({ conn, link, now: fixed }: { conn: Conn; link: ConnLink; now?: number }) {
  const tick = useNow(1000, fixed === undefined);
  const now = fixed ?? tick;
  const phase = connPhase(conn, link);
  const facts = connFacts(phase, link, now);
  // a click shows that it is trying for a moment (the socket answers within that, or the next try's line says when)
  const [trying, setTrying] = useState(false);
  useEffect(() => {
    if (!trying) return;
    const id = setTimeout(() => setTrying(false), 900);
    return () => clearTimeout(id);
  }, [trying]);
  return (
    <div
      role="dialog"
      aria-label={phaseTitle(phase)}
      data-connection-details
      className="absolute top-full z-20 mt-2 w-[300px] max-w-[calc(100vw-32px)] rounded-card bg-surface p-4 text-left shadow-overlay"
      style={{ animation: `pop-in 160ms ${EASE} both`, transformOrigin: "top center" }}
    >
      <div className="flex flex-col gap-3">
        <div className="flex items-center gap-2">
          <Dot phase={phase} />
          <span className="min-w-0 truncate text-[13.5px] font-medium text-ink">{phaseTitle(phase)}</span>
        </div>
        <p className="text-[12.5px] leading-relaxed text-ink-2">{phaseExplanation(phase)}</p>
        {facts.length > 0 && (
          <ul className="flex flex-col gap-1 text-[11.5px] text-ink-3 tabular-nums">
            {facts.map((f) => <li key={f}>{f}</li>)}
          </ul>
        )}
        {phase !== "online" && (
          <Button tone="primary" icon="retry" busy={trying} onClick={() => { setTrying(true); reconnectNow(); }} className="w-full">
            {t("lib-connection.retry_now")}
          </Button>
        )}
      </div>
    </div>
  );
}
