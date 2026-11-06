// The bar above the composer while an Agent works (cindy's RunningStatusBar, #1963): what its status line says (the
// spinner's verb, or "Thinking"), how long the turn has run and the tokens it has written so far. It breathes once each
// time the count moves (output, not just time passing); when the turn ends it stays a second, then fades out. Without
// a status line (another agent, an older hub) it says "Running" and counts from when the turn started. While the chat's live
// work line ("Working…") ticks, the bar leaves the time to it (#3534: the same clock twice; iOS shows it there only).
import { useEffect, useRef, useState } from "react";
import { duration } from "../../lib/format";
import { useSessionLive } from "../../lib/live";
import type { SessionLive } from "../../lib/types";
import { useNow } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { prefersReducedMotion } from "../../ui/stream/motion";
import "../../ui/stream/stream.css";
import { t } from "../../i18n";

const LINGER_MS = 1000;
const FADE_OUT_MS = 400;
const TWEEN_MS = 400;

/** 22.8k, 1.2k, 950: thousands with one decimal (the reference client's formatRunningTokenCount; not lib/compaction's ten-thousand units). */
export function tokenCount(n: number): string {
  return n >= 1000 ? `${(n / 1000).toFixed(1)}k` : `${Math.round(n)}`;
}

/** The subagents the agent waits on and what they do (lib/tasks.ts agentsWorking). */
export interface AgentsWorking {
  count: number;
  doing: string[];
}

/** The left side's words: while the agent waits on its subagents, how many work and what the newest does (#5602). */
export function statusText(live: SessionLive | null, agents?: AgentsWorking | null): string {
  if (agents) {
    const head = t("chat-runningstatusbar.agents_working", { count: agents.count });
    if (!agents.doing.length) return `${head}…`;
    const more = agents.doing.length > 1 ? ` · ${t("chat-runningstatusbar.agents_more", { count: agents.doing.length - 1 })}` : "";
    return `${head} — ${agents.doing[0]}…${more}`;
  }
  if (!live) return t("chat-runningstatusbar.running");
  if (live.thinking) return t("chat-runningstatusbar.thinking");
  return live.verb ? `${live.verb}…` : t("chat-runningstatusbar.running");
}

export function RunningStatusBar({ mid, sid, working, startedAt, clock, agents }: { mid: string; sid: string; working: boolean; startedAt?: string; clock?: boolean; agents?: AgentsWorking | null }) {
  const live = useSessionLive(mid, sid);
  return <RunningStatusView live={live} working={working} startedAt={startedAt} clock={clock} agents={agents} />;
}

type Phase = "on" | "linger" | "fading" | "off";

/** The bar for a given status line (RunningStatusBar subscribes; tests render this). `clock: false`: no time, the
 *  chat's live work line shows it. */
export function RunningStatusView({ live, working, startedAt, clock = true, agents }: { live: SessionLive | null; working: boolean; startedAt?: string; clock?: boolean; agents?: AgentsWorking | null }) {
  const phase = usePhase(working);
  // after the turn: what it last said, while it lingers and fades
  const last = useRef(live);
  if (working) last.current = live;
  const shown = working ? live : last.current;
  // and whether it had the time: the work line's clock goes with the turn, the bar does not take it up as it fades
  const hadClock = useRef(clock);
  if (working) hadClock.current = clock;
  const showClock = working ? clock : hadClock.current;
  const now = useNow(1000, phase === "on");
  const elapsed = useElapsed(shown, startedAt, now);
  const tokens = useTween(shown?.tokens_down ?? 0, TWEEN_MS);
  const breath = useBreath(shown?.tokens_down, phase === "on");
  if (phase === "off") return null;
  return (
    <div
      data-running-status={phase}
      className="flex select-none items-center justify-between gap-3 px-2 py-1.5 text-[13px] font-medium"
      style={{ opacity: phase === "fading" ? 0 : 1, transition: `opacity ${FADE_OUT_MS}ms ease-out` }}
    >
      <div key={breath.cycle} onAnimationEnd={breath.ended} className={`flex min-w-0 items-center gap-1.5 text-status-accent ${breath.cycle ? "status-breath" : ""}`}>
        <Icon name="sparkles" size={14} className="shrink-0 -translate-y-px" />
        <span className="truncate" data-running-verb>
          {statusText(shown, working ? agents : null)}
        </span>
      </div>
      <div className="flex shrink-0 items-center gap-1.5 whitespace-nowrap text-ink-3 tabular-nums">
        {showClock && <span data-running-elapsed>{duration(elapsed * 1000)}</span>}
        {shown?.tokens_down !== undefined && (
          <>
            {showClock && <span aria-hidden>·</span>}
            <span data-running-tokens>↓ {tokenCount(tokens)} tokens</span>
          </>
        )}
      </div>
    </div>
  );
}

/** on while working; after it, a second as it was, then the fade, then gone. */
function usePhase(working: boolean): Phase {
  const [phase, setPhase] = useState<Phase>(working ? "on" : "off");
  useEffect(() => {
    if (working) {
      setPhase("on");
      return;
    }
    if (phase === "off") return;
    const a = window.setTimeout(() => setPhase("fading"), LINGER_MS);
    const b = window.setTimeout(() => setPhase("off"), LINGER_MS + FADE_OUT_MS);
    return () => {
      clearTimeout(a);
      clearTimeout(b);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [working]);
  return working ? "on" : phase === "on" ? "linger" : phase;
}

/**
 * Seconds the turn has run: the status line's count, going on locally between its updates (about one a second);
 * without one, from `startedAt` (or from when the bar appeared).
 */
function useElapsed(live: SessionLive | null, startedAt: string | undefined, now: number): number {
  const base = useRef<{ s: number; at: number } | null>(null);
  const mounted = useRef(Date.now());
  if (live?.elapsed_s === undefined) base.current = null;
  else if (base.current?.s !== live.elapsed_s) base.current = { s: live.elapsed_s, at: Date.now() };
  if (base.current) return base.current.s + Math.max(0, Math.floor((now - base.current.at) / 1000));
  const from = startedAt ? Date.parse(startedAt) : NaN;
  return Math.max(0, Math.floor((now - (Number.isNaN(from) ? mounted.current : from)) / 1000));
}

/** A number easing to `target` over `ms` (cindy's useAnimatedNumber); a drop (a new turn) snaps. */
function useTween(target: number, ms: number): number {
  const [shown, setShown] = useState(target);
  const ref = useRef(target);
  ref.current = shown;
  useEffect(() => {
    const from = ref.current;
    if (from === target) return;
    if (target < from || prefersReducedMotion() || typeof requestAnimationFrame !== "function") {
      setShown(target);
      return;
    }
    const start = performance.now();
    let raf = 0;
    const step = (t: number) => {
      const k = Math.min(1, (t - start) / ms);
      setShown(from + (target - from) * (1 - (1 - k) ** 3));
      if (k < 1) raf = requestAnimationFrame(step);
    };
    raf = requestAnimationFrame(step);
    return () => cancelAnimationFrame(raf);
  }, [target, ms]);
  return shown;
}

/**
 * One breath (1.5 s) each time `value` moves while `on`: a new key replays the one-shot animation. A change during a
 * breath queues one more, so steady output breathes continuously and silence (a long think, a tool) stays still.
 */
function useBreath(value: number | undefined, on: boolean): { cycle: number; ended: () => void } {
  const [cycle, setCycle] = useState(0);
  const playing = useRef(false);
  const pending = useRef(false);
  const seen = useRef(value);
  useEffect(() => {
    if (!on) {
      playing.current = pending.current = false;
      seen.current = value;
      return;
    }
    if (value === seen.current) return;
    seen.current = value;
    if (value === undefined || prefersReducedMotion()) return; // reduced: the animation never ends
    if (playing.current) {
      pending.current = true;
      return;
    }
    playing.current = true;
    setCycle((n) => n + 1);
  }, [value, on]);
  const ended = () => {
    playing.current = false;
    if (!pending.current) return;
    pending.current = false;
    playing.current = true;
    setCycle((n) => n + 1);
  };
  return { cycle, ended };
}
