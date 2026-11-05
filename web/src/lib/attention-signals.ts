// The DOM side of the attention signals (#2332, logic in lib/attention.ts): the tab title, the favicon's badge, and the
// optional sounds, and the system notifications (#2971, lib/notify.ts). `useAttentionSignals` is the one call App makes.
import { useEffect, useRef } from "react";
import { stateOf } from "./format";
import { agentKey } from "./pins";
import { useCounts, useAgentSessions } from "./sessions";
import { getState, useStore } from "./store";
import { attentionSoundEnabled, badgeText, detectTransitions, pickEvent, rateLimiter, SETTLE_MS, suppressed, tabTitle, trackTurns, type Turn } from "./attention";
import { claimNotification, FRONT_BEAT_MS, loadNotifyPrefs, markFront, notificationText, notifyPrefs, otherTabInFront, postNotification, systemNotifyActive, wants } from "./notify";
import { isMuted } from "./session-actions";
import { href, navigate } from "./router";
import { t } from "../i18n";
import type { AgentState, Session } from "./types";

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
let faviconNeeds = 0;

/** The look's icon (lib/look-icon.ts, #3200) under the badge: each link's original becomes the look's picture for its
 *  scheme (index.html's light and dark links), or the files index.html names again with null. */
export function setFaviconBase(icon: { light: string; dark: string } | null): void {
  for (const link of document.querySelectorAll<HTMLLinkElement>(ICONS)) {
    link.dataset.file ??= link.dataset.original ?? link.getAttribute("href") ?? "";
    const dark = (link.getAttribute("media") ?? "").includes("dark");
    link.dataset.original = icon ? (dark ? icon.dark : icon.light) : link.dataset.file;
  }
  setFaviconBadge(faviconNeeds);
}

/** Swaps the page's icon links (index.html has a light and a dark one, chosen by the browser's media query) for the
 *  badged copy of each, or back to the originals at 0. The files themselves come through the service worker like any
 *  other icon (lib/sw-routes.ts "static": the hub's, else the cached one), so this also works offline; the drawn
 *  copies are data: URLs and never touch the network or the cache. */
export function setFaviconBadge(needs: number): void {
  faviconNeeds = needs;
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

/** The page is the one in front: its tab is showing and its window has the focus. */
function inFront(): boolean {
  return document.visibilityState === "visible" && document.hasFocus();
}

/** A system notification for the event (设置 › 通知): only while no tab of the app is in front (that one's title, badge
 *  and sounds cover it), and once for all the open tabs. Clicking it opens the agent's conversation. */
function notify(s: Session, kind: "needs" | "finished"): void {
  if (inFront() || otherTabInFront() || !systemNotifyActive()) return;
  const tag = `${agentKey(s)}#${kind}`;
  if (!claimNotification(tag)) return;
  const { title, body } = notificationText(s, kind, {
    needs: (name) => t("notify.needs_title", { name }),
    finished: (name) => t("notify.finished_title", { name }),
    needsBody: t("notify.needs_body"),
    finishedBody: t("notify.finished_body"),
  });
  const route = { name: "session", mid: s.machine_id, sid: s.session_id } as const;
  const icon = document.querySelector<HTMLLinkElement>(ICONS)?.dataset.original ?? document.querySelector<HTMLLinkElement>(ICONS)?.href;
  postNotification({ title, body, tag, url: href(route), icon }, () => navigate(route));
}

/**
 * The tab title (`label` is the page's label, as App works it out), the favicon badge, the sounds and the system
 * notifications. `pane` is the agent conversation on the page, if any: it is not announced while the window is focused.
 * Sounds and notifications follow this browser's 设置 › 通知 (lib/notify.ts): 需要你 by its switch, 做完 by its three
 * choices, a turn's length as the page saw it (trackTurns).
 */
export function useAttentionSignals(label: string, pane: { mid: string; sid: string } | undefined): void {
  const needs = useCounts().badge;
  useEffect(() => {
    document.title = tabTitle(label, needs);
  }, [label, needs]);
  useEffect(() => setFaviconBadge(needs), [needs]);

  useEffect(armAudio, []);

  // Tells the other tabs whether this one is in front (notify: none of them posts then).
  useEffect(() => {
    const mark = () => markFront(inFront());
    mark();
    const beat = setInterval(() => inFront() && markFront(true), FRONT_BEAT_MS);
    const away = () => markFront(false);
    window.addEventListener("focus", mark);
    window.addEventListener("blur", mark);
    document.addEventListener("visibilitychange", mark);
    window.addEventListener("pagehide", away);
    return () => {
      clearInterval(beat);
      away();
      window.removeEventListener("focus", mark);
      window.removeEventListener("blur", mark);
      document.removeEventListener("visibilitychange", mark);
      window.removeEventListener("pagehide", away);
    };
  }, []);

  const sessions = useAgentSessions();
  const loaded = useStore((s) => s.loaded);
  useEffect(() => {
    if (loaded) loadNotifyPrefs().catch(() => {}); // the cached copy stays when the hub can't say
  }, [loaded]);
  const key = pane ? agentKey({ machine_id: pane.mid, session_id: pane.sid }) : undefined;
  // read when an event fires, not when the effect ran
  const viewing = useRef(key);
  viewing.current = key;
  const prev = useRef<ReadonlyMap<string, AgentState>>(new Map());
  const turns = useRef<ReadonlyMap<string, Turn>>(new Map());
  const allow = useRef(rateLimiter());
  const latest = useRef(sessions);
  latest.current = sessions;
  const timers = useRef(new Set<ReturnType<typeof setTimeout>>());

  useEffect(() => {
    if (!loaded) return;
    const now = Date.now();
    const { next, events } = detectTransitions(prev.current, sessions, getState().mutes);
    prev.current = next;
    turns.current = trackTurns(turns.current, sessions, now);
    const prefs = notifyPrefs();
    const ran = (k: string) => {
      const turn = turns.current.get(k);
      return turn?.endedAt !== undefined ? turn.endedAt - turn.start : undefined;
    };
    const wanted = events.filter((e) => wants(e.kind, prefs, e.kind === "finished" ? ran(e.key) : undefined));
    const sound = attentionSoundEnabled();
    const speak = (kind: "needs" | "finished", k: string) => {
      if (!sound || suppressed(k, { key: viewing.current, focused: inFront() })) return;
      if (allow.current(Date.now())) play(kind);
    };
    const find = (k: string) => latest.current.find((x) => agentKey(x) === k);
    const urgent = wanted.filter((e) => e.kind === "needs");
    const first = pickEvent(urgent);
    if (first) speak(first.kind, first.key);
    // The sound of 需要你 does not look at mutes (detectTransitions); the notification does, as the phone's push and
    // the Mac's do (hub announce, MacAttention.transitions).
    for (const e of urgent) {
      const s = find(e.key);
      if (s && !isMuted(s, getState().mutes)) notify(s, "needs");
    }
    // A finished turn speaks once it has stayed finished (the status flaps working → done → working around a turn).
    // No sound when a 需要你 one is going out with it: that one is the more urgent.
    for (const done of wanted.filter((e) => e.kind === "finished")) {
      const t = setTimeout(() => {
        timers.current.delete(t);
        const s = find(done.key);
        if (!s || stateOf(s) !== "unread") return;
        if (!first) speak("finished", done.key);
        notify(s, "finished");
      }, SETTLE_MS);
      timers.current.add(t);
    }
  }, [sessions, loaded]);

  useEffect(() => {
    const pending = timers.current;
    return () => pending.forEach(clearTimeout);
  }, []);
}
