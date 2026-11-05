// The DOM side of the attention signals (#2332, logic in lib/attention.ts): the tab title, the favicon's badge, and the
// optional sounds. `useAttentionSignals` is the one call App makes.
import { useEffect, useRef } from "react";
import { stateOf } from "./format";
import { agentKey } from "./pins";
import { useCounts, useAgentSessions } from "./sessions";
import { getState, useStore } from "./store";
import { attentionSoundEnabled, badgeText, detectTransitions, pickEvent, rateLimiter, SETTLE_MS, suppressed, tabTitle } from "./attention";
import type { AgentState } from "./types";

// ── favicon ──────────────────────────────────────────────

const ICONS = 'link[rel="icon"]';
const SIZE = 64; // the icon files are 64 px; the browser scales the tab's down
const decoded = new Map<string, Promise<HTMLImageElement>>();
const drawn = new Map<string, string>();

function load(src: string): Promise<HTMLImageElement> {
  let p = decoded.get(src);
  if (!p) {
    p = new Promise((resolve, reject) => {
      const img = new Image();
      img.onload = () => resolve(img);
      img.onerror = () => reject(new Error("icon"));
      img.src = src;
    });
    decoded.set(src, p);
    p.catch(() => decoded.delete(src));
  }
  return p;
}

/** The icon with the 需要你 badge at its top right: a red disc (the sidebar's --status-needs) with the count. */
async function badged(src: string, text: string): Promise<string> {
  const key = `${src}|${text}`;
  const hit = drawn.get(key);
  if (hit) return hit;
  const img = await load(src);
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = SIZE;
  const g = canvas.getContext("2d");
  if (!g) throw new Error("canvas");
  g.drawImage(img, 0, 0, SIZE, SIZE);
  const css = getComputedStyle(document.documentElement);
  const r = SIZE * 0.34;
  const cx = SIZE - r;
  const cy = r;
  g.beginPath();
  g.arc(cx, cy, r, 0, Math.PI * 2);
  g.fillStyle = css.getPropertyValue("--status-needs").trim() || css.getPropertyValue("--red").trim() || "red";
  g.fill();
  g.lineWidth = SIZE * 0.05;
  g.strokeStyle = css.getPropertyValue("--surface").trim() || "white";
  g.stroke();
  g.fillStyle = "white";
  g.font = `700 ${text.length > 1 ? r * 1.05 : r * 1.35}px system-ui, sans-serif`;
  g.textAlign = "center";
  g.textBaseline = "middle";
  g.fillText(text, cx, cy + r * 0.06);
  const url = canvas.toDataURL("image/png");
  drawn.set(key, url);
  return url;
}

let faviconSeq = 0;

/** Swaps the page's icon links (index.html has a light and a dark one, chosen by the browser's media query) for the
 *  badged copy of each, or back to the originals at 0. The files themselves come through the service worker like any
 *  other icon (lib/sw-routes.ts "static": the hub's, else the cached one), so this also works offline; the drawn
 *  copies are data: URLs and never touch the network or the cache. */
export function setFaviconBadge(needs: number): void {
  const links = [...document.querySelectorAll<HTMLLinkElement>(ICONS)];
  const text = badgeText(needs);
  const seq = ++faviconSeq;
  for (const link of links) {
    link.dataset.original ??= link.getAttribute("href") ?? "";
    const original = link.dataset.original;
    if (!text) {
      link.setAttribute("href", original);
      continue;
    }
    badged(original, text).then(
      (url) => seq === faviconSeq && link.setAttribute("href", url),
      () => seq === faviconSeq && link.setAttribute("href", original), // the icon can't be read: leave it as it is
    );
  }
}

// ── sounds ───────────────────────────────────────────────

let ctx: AudioContext | undefined;
let gestured = false;

/** Browsers only start audio after a user gesture: until one has happened nothing plays (and nothing is queued). */
export function armAudio(): void {
  if (typeof window === "undefined") return;
  const unlock = () => {
    gestured = true;
    try {
      ctx ??= new AudioContext();
      void ctx.resume();
    } catch {
      // no WebAudio here: silent
    }
  };
  for (const ev of ["pointerdown", "keydown", "touchend"]) window.addEventListener(ev, unlock, { once: true, capture: true });
}

// [frequency Hz, start s, length s, peak gain]
type Note = [number, number, number, number];
/** 需要你: two rising notes, clear. */
const CHIME: Note[] = [[784, 0, 0.22, 0.16], [1175, 0.14, 0.34, 0.16]];
/** 完成: one low, soft note. */
const DONE: Note[] = [[523, 0, 0.4, 0.07]];

export function play(kind: "needs" | "finished"): void {
  if (!gestured || !ctx || ctx.state !== "running") return;
  const t0 = ctx.currentTime;
  for (const [freq, at, len, peak] of kind === "needs" ? CHIME : DONE) {
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.type = "sine";
    osc.frequency.value = freq;
    gain.gain.setValueAtTime(0.0001, t0 + at);
    gain.gain.exponentialRampToValueAtTime(peak, t0 + at + 0.02);
    gain.gain.exponentialRampToValueAtTime(0.0001, t0 + at + len);
    osc.connect(gain).connect(ctx.destination);
    osc.start(t0 + at);
    osc.stop(t0 + at + len + 0.05);
  }
}

// ── the hook ─────────────────────────────────────────────

/**
 * The tab title (`label` is the page's label, as App works it out), the favicon badge and the sounds. `pane` is the
 * agent conversation on the page, if any: it is not announced while the window is focused.
 */
export function useAttentionSignals(label: string, pane: { mid: string; sid: string } | undefined): void {
  const needs = useCounts().badge;
  useEffect(() => {
    document.title = tabTitle(label, needs);
  }, [label, needs]);
  useEffect(() => setFaviconBadge(needs), [needs]);

  useEffect(armAudio, []);

  const sessions = useAgentSessions();
  const loaded = useStore((s) => s.loaded);
  const key = pane ? agentKey({ machine_id: pane.mid, session_id: pane.sid }) : undefined;
  // read when an event fires, not when the effect ran
  const viewing = useRef(key);
  viewing.current = key;
  const prev = useRef<ReadonlyMap<string, AgentState>>(new Map());
  const allow = useRef(rateLimiter());
  const latest = useRef(sessions);
  latest.current = sessions;
  const timers = useRef(new Set<ReturnType<typeof setTimeout>>());

  useEffect(() => {
    if (!loaded) return;
    const { next, events } = detectTransitions(prev.current, sessions, getState().mutes);
    prev.current = next;
    if (!attentionSoundEnabled()) return;
    const speak = (kind: "needs" | "finished", k: string) => {
      const focused = document.visibilityState === "visible" && document.hasFocus();
      if (suppressed(k, { key: viewing.current, focused })) return;
      if (allow.current(Date.now())) play(kind);
    };
    const now = events.filter((e) => e.kind === "needs");
    const first = pickEvent(now);
    if (first) speak(first.kind, first.key);
    // A finished turn sounds once it has stayed finished (the status flaps working → done → working around a turn).
    // Not when a 需要你 sound is going out with it: that one is the more urgent.
    const done = first ? undefined : pickEvent(events.filter((e) => e.kind === "finished"));
    if (done) {
      const t = setTimeout(() => {
        timers.current.delete(t);
        const s = latest.current.find((x) => agentKey(x) === done.key);
        if (s && stateOf(s) === "unread") speak("finished", done.key);
      }, SETTLE_MS);
      timers.current.add(t);
    }
  }, [sessions, loaded]);

  useEffect(() => {
    const pending = timers.current;
    return () => pending.forEach(clearTimeout);
  }, []);
}
