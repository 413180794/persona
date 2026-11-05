// 设置 › 通知 of this browser (#2971, the app's NotificationSettingsView): what the hub keeps per device (GET / PUT
// /devices/{id}/prefs, hub/notify.go NotifyPrefs), and the browser's system notifications that follow it. The hub does
// not push to a browser: while the page is open it watches its own session list (lib/attention-signals.ts) and posts a
// notification when it is not in front, as the Mac app does (MacAttention.swift). The prefs also decide the sounds.
import { useSyncExternalStore } from "react";
import { api, enc } from "./api";
import { sessionTitle } from "./format";
import type { Session } from "./types";

export type DonePref = "off" | "long" | "always";

/** The part of a device's prefs this page reads and writes; the rest (the phone's Live Activity, translation) stays as
 *  the hub has it: a PUT decodes over the device's stored prefs. */
export interface NotifyPrefs {
  input: boolean;
  done: DonePref;
  /** What 长任务 means, from the hub (notify.go longTurn). */
  long_turn_seconds?: number;
  /** How long a finished turn waits before the phone's push (notify.go doneDelay). */
  done_delay_seconds?: number;
}

/** The hub's defaults (notify.go defaultPrefs, longTurn). */
export const DEFAULT_PREFS: NotifyPrefs = { input: true, done: "long" };
export const DEFAULT_LONG_TURN_SECONDS = 60;

// ── which events speak ───────────────────────────────────

/**
 * Does this browser want a signal (sound, notification) for the event? 需要你: the 需要你处理时 switch. 做完: 不推送 never,
 * 每次都推 always, 只推长任务 for a turn of at least long_turn_seconds (`ranMs`; unknown is not long, as on the Mac).
 */
export function wants(kind: "needs" | "finished", prefs: NotifyPrefs, ranMs?: number): boolean {
  if (kind === "needs") return prefs.input;
  switch (prefs.done) {
    case "always":
      return true;
    case "long":
      return ranMs !== undefined && ranMs >= (prefs.long_turn_seconds ?? DEFAULT_LONG_TURN_SECONDS) * 1000;
    default:
      return false;
  }
}

/** A duration as the app says it (NotifyPrefs.duration): whole minutes in minutes, else seconds; 60 s when unknown. */
export function durationText(seconds: number | undefined, units: { s: (n: number) => string; m: (n: number) => string }): string {
  const s = seconds ?? DEFAULT_LONG_TURN_SECONDS;
  return s >= 60 && s % 60 === 0 ? units.m(s / 60) : units.s(s);
}

// ── this browser's device id and its prefs ───────────────

const DEVICE_KEY = "persona.notifyDevice";
const PREFS_KEY = "persona.notifyPrefs";

const randomId = () => crypto.randomUUID?.() ?? Math.random().toString(36).slice(2);

/**
 * The id this browser's prefs go by on the hub: made once, kept in this browser (`kept`). The hub ties the row to the
 * device token that wrote it, so revoking this browser in 设置 › 设备 removes it. Without storage the id would be new
 * on every load: it is this page's own and nothing is sent (`kept: false`), so no browser leaves rows behind or shares
 * one id with another.
 */
let pageId: string | undefined;
export function deviceId(): { id: string; kept: boolean } {
  try {
    let id = localStorage.getItem(DEVICE_KEY);
    if (!id) {
      id = `web-${randomId()}`;
      localStorage.setItem(DEVICE_KEY, id);
    }
    return { id, kept: true };
  } catch {
    pageId ??= `web-${randomId()}`;
    return { id: pageId, kept: false };
  }
}

function readCached(): NotifyPrefs {
  try {
    const raw = localStorage.getItem(PREFS_KEY);
    return raw ? normalize(JSON.parse(raw)) : DEFAULT_PREFS;
  } catch {
    return DEFAULT_PREFS;
  }
}

/** The hub's answer, defaults filled in and an unknown 做完 taken as 只推长任务 (as the hub does). */
export function normalize(raw: Partial<NotifyPrefs> | null | undefined): NotifyPrefs {
  const done: DonePref = raw?.done === "off" || raw?.done === "always" ? raw.done : "long";
  return {
    input: raw?.input ?? true,
    done,
    long_turn_seconds: raw?.long_turn_seconds,
    done_delay_seconds: raw?.done_delay_seconds,
  };
}

let prefs: NotifyPrefs = typeof localStorage === "undefined" ? DEFAULT_PREFS : readCached();
const listeners = new Set<() => void>();

// Another tab of the app changed them (its own save or load): this one follows at once, sounds and notifications too.
if (typeof window !== "undefined") {
  window.addEventListener("storage", (e) => {
    if (e.key !== PREFS_KEY) return;
    prefs = readCached();
    listeners.forEach((l) => l());
  });
}

function set(next: NotifyPrefs) {
  prefs = next;
  try {
    localStorage.setItem(PREFS_KEY, JSON.stringify(next));
  } catch {
    // storage unavailable: kept for this page only
  }
  listeners.forEach((l) => l());
}

export function notifyPrefs(): NotifyPrefs {
  return prefs;
}

// Each save takes a number; only the latest one's answer (or failure) is applied, so two quick changes whose answers
// land out of order never show a state without the second.
let saveSeq = 0;

/** The hub's prefs for this browser; the cached copy until it answers (and if it can't). */
export async function loadNotifyPrefs(): Promise<NotifyPrefs> {
  const { id, kept } = deviceId();
  if (!kept) return prefs;
  const seq = saveSeq;
  const p = normalize(await api<Partial<NotifyPrefs>>(`/devices/${enc(id)}/prefs`));
  if (seq === saveSeq) set(p); // a save went out meanwhile: its answer is newer
  return prefs;
}

/** Optimistic: the control moves at once and goes back if the hub says no (the error is the caller's to show). */
export async function saveNotifyPrefs(patch: Partial<Pick<NotifyPrefs, "input" | "done">>): Promise<void> {
  const before = prefs;
  const mine = ++saveSeq;
  set({ ...prefs, ...patch });
  const { id, kept } = deviceId();
  if (!kept) return; // this page's own: nothing to keep them on the hub by
  try {
    const answer = normalize(await api<Partial<NotifyPrefs>>(`/devices/${enc(id)}/prefs`, { method: "PUT", json: { input: prefs.input, done: prefs.done } }));
    if (mine === saveSeq) set(answer);
  } catch (e) {
    if (mine === saveSeq) set(before);
    throw e;
  }
}

function subscribe(l: () => void) {
  listeners.add(l);
  return () => listeners.delete(l);
}

export function useNotifyPrefs(): NotifyPrefs {
  return useSyncExternalStore(subscribe, notifyPrefs, () => DEFAULT_PREFS);
}

// ── the browser's system notifications ───────────────────

/** unsupported: this browser has no Notification API (an iPhone's Safari outside a home-screen app, an old browser). */
export type Permission = "unsupported" | "default" | "granted" | "denied";

export function permission(): Permission {
  if (typeof Notification === "undefined") return "unsupported";
  return Notification.permission;
}

const SYSTEM_KEY = "persona.systemNotify";
const systemListeners = new Set<() => void>();

/** The page's own switch, off until turned on here (turning it on asks the browser). */
export function systemNotifyOn(): boolean {
  try {
    return localStorage.getItem(SYSTEM_KEY) === "1";
  } catch {
    return false;
  }
}

function setSystemFlag(on: boolean) {
  try {
    if (on) localStorage.setItem(SYSTEM_KEY, "1");
    else localStorage.removeItem(SYSTEM_KEY);
  } catch {
    // storage unavailable
  }
  systemListeners.forEach((l) => l());
}

/** Turns the switch on (asking the browser first when it has not been asked) or off. Says the permission it ended with. */
export async function setSystemNotify(on: boolean): Promise<Permission> {
  if (!on) {
    setSystemFlag(false);
    return permission();
  }
  let p = permission();
  if (p === "default") p = await Notification.requestPermission();
  setSystemFlag(p === "granted");
  return p;
}

/** Notifications go out: the switch is on and the browser allows them. */
export function systemNotifyActive(): boolean {
  return systemNotifyOn() && permission() === "granted";
}

function subscribeSystem(l: () => void) {
  systemListeners.add(l);
  window.addEventListener("storage", l);
  // the permission changes in the browser's own settings: look again when the page comes back
  window.addEventListener("focus", l);
  return () => {
    systemListeners.delete(l);
    window.removeEventListener("storage", l);
    window.removeEventListener("focus", l);
  };
}

const snapshot = () => `${systemNotifyOn() ? 1 : 0}|${permission()}`;

export function useSystemNotify(): { on: boolean; permission: Permission } {
  const s = useSyncExternalStore(subscribeSystem, snapshot, () => "0|unsupported");
  const [on, p] = s.split("|");
  return { on: on === "1", permission: p as Permission };
}

// ── between the app's tabs ───────────────────────────────
// Every open tab watches the same sessions. A notification is for when the user is not looking at the app at all, so
// none goes out while any tab of it is in front, and one event makes one notification, not one per tab.

const FRONT_KEY = "persona.frontTab";
const NOTIFIED_KEY = "persona.notified";
/** The tab in front says so again this often; a mark older than FRONT_FRESH_MS is a tab that went away without a word. */
export const FRONT_BEAT_MS = 20_000;
export const FRONT_FRESH_MS = 3 * FRONT_BEAT_MS;
/** The tabs see an event within moments of each other: the first to post it within this long is the one that does. */
export const NOTIFIED_WINDOW_MS = 5_000;

let tabId = `tab-${randomId()}`;

/** For tests: this module as another tab of the same browser. */
export function _setTabId(id: string): void {
  tabId = id;
}

function readJSON<T>(key: string): T | undefined {
  try {
    const raw = localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : undefined;
  } catch {
    return undefined;
  }
}

/** Records whether this tab is the one in front (on focus, blur, visibility changes and every FRONT_BEAT_MS). */
export function markFront(front: boolean, now = Date.now()): void {
  try {
    if (front) localStorage.setItem(FRONT_KEY, JSON.stringify({ tab: tabId, at: now }));
    else if (readJSON<{ tab?: string }>(FRONT_KEY)?.tab === tabId) localStorage.removeItem(FRONT_KEY);
  } catch {
    // storage unavailable: each tab decides on its own
  }
}

/** Some other tab of the app is the one in front. */
export function otherTabInFront(now = Date.now()): boolean {
  const mark = readJSON<{ tab?: string; at?: number }>(FRONT_KEY);
  return !!mark && mark.tab !== tabId && typeof mark.at === "number" && now - mark.at < FRONT_FRESH_MS;
}

/** Takes the event (`tag`) for this tab: false when another tab already posted it just now. */
export function claimNotification(tag: string, now = Date.now()): boolean {
  const seen = readJSON<Record<string, number>>(NOTIFIED_KEY) ?? {};
  if (typeof seen[tag] === "number" && now - seen[tag] < NOTIFIED_WINDOW_MS) return false;
  const next: Record<string, number> = { [tag]: now };
  for (const [k, at] of Object.entries(seen)) if (k !== tag && now - at < NOTIFIED_WINDOW_MS) next[k] = at;
  try {
    localStorage.setItem(NOTIFIED_KEY, JSON.stringify(next));
  } catch {
    // storage unavailable: this tab posts
  }
  return true;
}

/** What a notification says (MacAttention.text): the agent and what happened, its last words as the body. */
export function notificationText(s: Pick<Session, "title" | "cwd" | "last_text">, kind: "needs" | "finished", words: { needs: (title: string) => string; finished: (title: string) => string; needsBody: string; finishedBody: string }): { title: string; body: string } {
  const name = sessionTitle(s);
  const title = kind === "needs" ? words.needs(name) : words.finished(name);
  const last = s.last_text?.replace(/\s+/g, " ").trim();
  return { title, body: (last || (kind === "needs" ? words.needsBody : words.finishedBody)).slice(0, 200) };
}

/**
 * Posts one notification; a later one for the same agent and kind replaces it (`tag`). Clicking it brings the page
 * forward and runs `open`. Where the page can't make one itself (Chrome on Android wants the service worker's), the
 * service worker shows it and opens `url` when clicked (sw.ts notificationclick).
 */
export function postNotification(n: { title: string; body: string; tag: string; url: string; icon?: string }, open: () => void): void {
  if (!systemNotifyActive()) return;
  try {
    const note = new Notification(n.title, { body: n.body, tag: n.tag, icon: n.icon });
    note.onclick = () => {
      window.focus();
      open();
      note.close();
    };
  } catch {
    navigator.serviceWorker
      ?.getRegistration()
      .then((reg) => reg?.showNotification(n.title, { body: n.body, tag: n.tag, icon: n.icon, data: { url: n.url } }))
      .catch(() => {});
  }
}
