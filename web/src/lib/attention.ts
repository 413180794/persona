// Attention signals of the web client (#2332): the tab title's count, the favicon's badge and the optional sounds. This
// file is the logic without a DOM (tested in attention.test.ts); lib/attention-signals.ts draws and plays.
//
// The count is the sidebar's own red 需要你 count (stateCounts().badge in lib/sessions.ts: quiet tasks' agents are told
// about in the main conversation, not counted), so the tab and the sidebar never disagree.
import { useSyncExternalStore } from "react";
import { stateOf } from "./format";
import { agentKey } from "./pins";
import { isMuted } from "./session-actions";
import type { AgentMutes } from "./agent-groups";
import type { AgentState, Session } from "./types";

const SITE = "Persona";

/** The tab title: the page's label, `(N) ` in front while N sessions need you. */
export function tabTitle(label: string, needs: number): string {
  const page = !label || label === SITE ? SITE : `${label} · ${SITE}`;
  return needs > 0 ? `(${needs}) ${page}` : page;
}

/** What the favicon's badge says: the count up to 9, "9+" beyond (it is drawn in 16–32 px); nothing for 0. */
export function badgeText(needs: number): string {
  if (needs <= 0) return "";
  return needs > 9 ? "9+" : String(needs);
}

export type AttentionEvent = { kind: "needs" | "finished"; key: string };

/**
 * What changed between two snapshots of the sessions' states (`prev`: the states of the last one, by agentKey).
 * - needs: a session became 需要你 (not a quiet task's: the sidebar does not count it either).
 * - finished: a session went 工作中 → 完成未看. This mirrors the hub's "finished" push (hub/machine.go notifyFinished,
 *   notify.go settle): the push is for a turn that ended and has not been seen (unread, not idle), is dropped for a
 *   muted or quiet session, and a session with background work still running stays 工作中 (backgroundState), so it
 *   never ends here. Differences: the hub waits `done_delay` and re-checks; here the caller re-checks after its own
 *   short wait (SETTLE_MS). The hub also drops a turn started by a background task's report (`WokeByTask`) and one
 *   the assistant already pushed about; the web client does not see those and chimes for them.
 * A session with no earlier state (the first snapshot, a new session) produces nothing, so opening the page does not chime.
 */
export function detectTransitions(prev: ReadonlyMap<string, AgentState>, sessions: Session[], mutes: AgentMutes): { next: Map<string, AgentState>; events: AttentionEvent[] } {
  const next = new Map<string, AgentState>();
  const events: AttentionEvent[] = [];
  for (const s of sessions) {
    const key = agentKey(s);
    const now = stateOf(s);
    next.set(key, now);
    const was = prev.get(key);
    if (was === undefined || was === now || s.quiet) continue;
    if (now === "needs_you") events.push({ kind: "needs", key });
    else if (was === "working" && now === "unread" && !isMuted(s, mutes)) events.push({ kind: "finished", key });
  }
  return { next, events };
}

/**
 * The notification tags (`<agentKey>#needs` / `#finished`, as attention-signals posts them) whose event is over between
 * two snapshots: a session no longer 需要你, or no longer 完成未看 (seen here or elsewhere), or gone from the list. Their
 * system notifications are taken back, so one answered on the phone does not wait in the notification centre (#3458).
 */
export function settledTags(prev: ReadonlyMap<string, AgentState>, next: ReadonlyMap<string, AgentState>): string[] {
  const out: string[] = [];
  for (const [key, was] of prev) {
    const now = next.get(key);
    if (was === now) continue;
    if (was === "needs_you") out.push(`${key}#needs`);
    else if (was === "unread") out.push(`${key}#finished`);
  }
  return out;
}

/** A session's current turn as the page saw it: when it started working, and when it last stopped (ms). */
export interface Turn {
  start: number;
  endedAt?: number;
}

/**
 * Follows each session's turns between snapshots, for 只推长任务 (hub/notify.go turnTracker): a turn starts when the
 * session starts working (the hub's state_since, else now), and one that resumes within SETTLE_MS of stopping is the
 * same turn (the status flaps around a turn), so its start is kept. A session not working keeps its last turn, so the
 * length of the turn that just finished can be read.
 */
export function trackTurns(prev: ReadonlyMap<string, Turn>, sessions: Session[], now: number): Map<string, Turn> {
  const next = new Map<string, Turn>();
  for (const s of sessions) {
    const key = agentKey(s);
    const was = prev.get(key);
    if (stateOf(s) === "working") {
      if (was && (was.endedAt === undefined || now - was.endedAt < SETTLE_MS)) next.set(key, { start: was.start });
      else {
        const since = s.state_since ? Date.parse(s.state_since) : NaN;
        next.set(key, { start: Number.isNaN(since) ? now : Math.min(since, now) });
      }
    } else if (was) next.set(key, was.endedAt === undefined ? { start: was.start, endedAt: now } : was);
  }
  return next;
}

/** How long a finished turn must stay finished before it sounds (the status flaps working → done → working around a turn). */
export const SETTLE_MS = 1500;

/** Where the user is looking: the session on the page, and whether the window is the one they are using. */
export interface Viewing {
  /** agentKey of the conversation on the page, if any. */
  key?: string;
  /** The window is focused and visible. */
  focused: boolean;
}

/** No sound for the session on screen while its window is focused: the user is looking at it. */
export function suppressed(key: string, viewing: Viewing): boolean {
  return viewing.focused && viewing.key === key;
}

/** Of a batch the more urgent one speaks: 需要你 over finished. */
export function pickEvent(events: AttentionEvent[]): AttentionEvent | undefined {
  return events.find((e) => e.kind === "needs") ?? events[0];
}

export const MIN_GAP_MS = 3000;

/** At most one sound per `gap` ms. `now` is a clock in ms (injected for tests). */
export function rateLimiter(gap = MIN_GAP_MS) {
  let last = -Infinity;
  return (now: number): boolean => {
    if (now - last < gap) return false;
    last = now;
    return true;
  };
}

// ── 提示音 setting (设置 › 通用), kept in this browser, off by default ──────────────

const KEY = "persona.attentionSound";
const listeners = new Set<() => void>();

export function attentionSoundEnabled(): boolean {
  try {
    return localStorage.getItem(KEY) === "1";
  } catch {
    return false;
  }
}

export function setAttentionSoundEnabled(on: boolean): void {
  try {
    if (on) localStorage.setItem(KEY, "1");
    else localStorage.removeItem(KEY);
  } catch {
    // storage unavailable: nothing is kept, the default (off) stays
  }
  listeners.forEach((l) => l());
}

function subscribe(l: () => void) {
  listeners.add(l);
  window.addEventListener("storage", l);
  return () => {
    listeners.delete(l);
    window.removeEventListener("storage", l);
  };
}

export function useAttentionSoundSetting(): boolean {
  return useSyncExternalStore(subscribe, attentionSoundEnabled, () => false);
}
