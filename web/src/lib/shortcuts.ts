// The keyboard shortcuts of the web client (docs/web.md「快捷键」): the registry of commands (id, label, default chord,
// scope), chord parsing / formatting per platform, the user's rebindings (localStorage), the reserved chords, and the
// dispatcher that App.tsx's keydown listener and the terminal's key handler call.
//
// Every chord carries Mod (⌘ on a Mac, Ctrl elsewhere): a bare key is never a shortcut, so typing and the terminal's own
// keys are never swallowed. Pure apart from the small store of rebindings, so it is tested without a DOM.
import { useSyncExternalStore } from "react";
import type { IconName } from "../ui/icons";

// ── platform ─────────────────────────────────────────────

export type Platform = "mac" | "other";

/** ⌘ is Mod on Apple platforms (Mac, iPad Safari asking for the desktop site), Ctrl everywhere else. */
export function platformOf(nav?: { platform?: string; userAgent?: string; userAgentData?: { platform?: string } }): Platform {
  const hint = nav?.userAgentData?.platform || nav?.platform || nav?.userAgent || "";
  return /mac|iphone|ipad|ipod/i.test(hint) ? "mac" : "other";
}

let detected: Platform | null = null;
export function currentPlatform(): Platform {
  detected ??= platformOf(typeof navigator === "undefined" ? undefined : (navigator as unknown as Parameters<typeof platformOf>[0]));
  return detected;
}

/** What the modifier is called on this platform, for messages: 需要带 Ctrl / 需要带 ⌘. */
export const modName = (p: Platform) => (p === "mac" ? "⌘" : "Ctrl");

// ── chords ───────────────────────────────────────────────

/**
 * A chord is written platform-independently, modifiers in a fixed order and then the key: "Mod+Alt+Shift+Up".
 * Mod is required. A key is a letter, a digit, one of `,./;'[]\-=\``, Up / Down / Left / Right, Enter, Space, Home / End /
 * PageUp / PageDown or F1–F12. "1-9" stands for the digits 1 to 9 together (the one command that takes a number).
 */
export interface Chord {
  alt: boolean;
  shift: boolean;
  key: string;
}

export const DIGITS = "1-9";

const PUNCTUATION = ",./;'[]\\-=`";
const NAMED = ["Up", "Down", "Left", "Right", "Enter", "Space", "Home", "End", "PageUp", "PageDown"];

function normalKey(raw: string): string | null {
  if (raw === DIGITS) return DIGITS;
  if (raw.length === 1) {
    if (/[a-z]/i.test(raw)) return raw.toUpperCase();
    if (/[0-9]/.test(raw) || PUNCTUATION.includes(raw)) return raw;
    return null;
  }
  const named = NAMED.find((n) => n.toLowerCase() === raw.toLowerCase());
  if (named) return named;
  const f = raw.match(/^f([1-9]|1[0-2])$/i);
  return f ? `F${f[1]}` : null;
}

/** "Mod+Shift+P" → its parts; null when it is not a chord (no Mod, no key, an unknown key or modifier). */
export function parseChord(text: string): Chord | null {
  const parts = text.split("+").map((s) => s.trim());
  const key = normalKey(parts.pop() ?? "");
  if (!key || !parts.length) return null;
  let mod = false;
  const c: Chord = { alt: false, shift: false, key };
  for (const p of parts) {
    const m = p.toLowerCase();
    if (m === "mod" || m === "ctrl" || m === "control" || m === "cmd" || m === "command" || m === "meta") mod = true;
    else if (m === "alt" || m === "option" || m === "opt") c.alt = true;
    else if (m === "shift") c.shift = true;
    else return null;
  }
  return mod ? c : null;
}

export const chordText = (c: Chord) => ["Mod", c.alt && "Alt", c.shift && "Shift", c.key].filter(Boolean).join("+");

/** The one spelling of a chord ("ctrl+shift+p" → "Mod+Shift+P"); null when it is not one. */
export function canonChord(text: string): string | null {
  const c = parseChord(text);
  return c ? chordText(c) : null;
}

/** The chords a binding stands for: itself, or the nine of "Mod+1-9". */
export function expandChord(text: string): string[] {
  const c = parseChord(text);
  if (!c) return [];
  return c.key === DIGITS ? Array.from({ length: 9 }, (_, i) => chordText({ ...c, key: String(i + 1) })) : [chordText(c)];
}

const ARROWS: Record<string, string> = { Up: "↑", Down: "↓", Left: "←", Right: "→" };

/** A chord as the keycaps it is drawn with: ["Ctrl", "Alt", "B"] on Windows / Linux, ["⌥", "⌘", "B"] on a Mac. */
export function chordParts(text: string, platform: Platform = currentPlatform()): string[] {
  const c = parseChord(text);
  if (!c) return [];
  const key = c.key === DIGITS ? "1–9" : (ARROWS[c.key] ?? c.key);
  return platform === "mac" ? [c.alt && "⌥", c.shift && "⇧", "⌘", key].filter((s): s is string => Boolean(s)) : ["Ctrl", c.alt && "Alt", c.shift && "Shift", key].filter((s): s is string => Boolean(s));
}

/** One line of text for a chord: "⌥⌘B" or "Ctrl+Alt+B". */
export function formatChord(text: string, platform: Platform = currentPlatform()): string {
  return chordParts(text, platform).join(platform === "mac" ? "" : "+");
}

// ── keyboard events → chords ─────────────────────────────

/** The parts of a KeyboardEvent the shortcuts read (so they are tested with plain objects). */
export interface KeyEventLike {
  type?: string;
  key: string;
  code?: string;
  keyCode?: number;
  isComposing?: boolean;
  repeat?: boolean;
  ctrlKey: boolean;
  metaKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
  getModifierState?: (key: string) => boolean;
}

// event.key spellings of the keys chords use (when there is no code to go by)
const KEY_NAMES: Record<string, string> = { ArrowUp: "Up", ArrowDown: "Down", ArrowLeft: "Left", ArrowRight: "Right", " ": "Space" };

const CODE_KEYS: Record<string, string> = {
  Comma: ",", Period: ".", Slash: "/", Semicolon: ";", Quote: "'", BracketLeft: "[", BracketRight: "]", Backslash: "\\", Minus: "-", Equal: "=", Backquote: "`",
  ArrowUp: "Up", ArrowDown: "Down", ArrowLeft: "Left", ArrowRight: "Right", Enter: "Enter", NumpadEnter: "Enter", Space: "Space",
  Home: "Home", End: "End", PageUp: "PageUp", PageDown: "PageDown",
};

/** The key an event is for, in the chord's spelling; null for a modifier on its own or a key chords do not use (Esc, Tab…). */
export function eventKey(e: Pick<KeyEventLike, "key" | "code">): string | null {
  const code = e.code ?? "";
  // digits and punctuation by position: Shift (or a layout) changes what they type, not which key they are
  const digit = code.match(/^(?:Digit|Numpad)([0-9])$/);
  if (digit) return digit[1]!;
  // a letter by what it types (a Dvorak or AZERTY keyboard follows its labels), or by its key when Option / Alt composed
  // it into another character (⌥B types ∫)
  if (e.key.length === 1 && /[a-z]/i.test(e.key)) return e.key.toUpperCase();
  const letter = code.match(/^Key([A-Z])$/);
  if (letter) return letter[1]!;
  if (CODE_KEYS[code]) return CODE_KEYS[code]!;
  return normalKey(KEY_NAMES[e.key] ?? e.key);
}

/**
 * The chord an event presses, with whatever modifiers it holds: `mod` says whether Mod (and nothing that stands in for
 * it: Ctrl on a Mac is the terminal's, the Windows key is the system's) is down. null while only modifiers are down.
 */
export function eventChord(e: KeyEventLike, platform: Platform = currentPlatform()): (Chord & { mod: boolean }) | null {
  const key = eventKey(e);
  if (!key) return null;
  // AltGr (Ctrl+Alt on some Windows layouts) types characters: it is not Mod+Alt
  if (e.getModifierState?.("AltGraph")) return { alt: e.altKey, shift: e.shiftKey, key, mod: false };
  const mod = platform === "mac" ? e.metaKey && !e.ctrlKey : e.ctrlKey && !e.metaKey;
  return { alt: e.altKey, shift: e.shiftKey, key, mod };
}

/** The canonical chord of a keydown that has Mod; null for anything else (a bare key is never a shortcut). */
export function chordOfEvent(e: KeyEventLike, platform: Platform = currentPlatform()): string | null {
  const c = eventChord(e, platform);
  return c?.mod ? chordText(c) : null;
}

// ── the reserved chords ──────────────────────────────────

export interface Reserved {
  chord: string;
  /** Why it is not for rebinding, shown in the settings. */
  why: string;
  /** Only on this platform. */
  only?: Platform;
}

const BROWSER = "浏览器要用";
const EDITING = "文字编辑要用";
const TERMINAL = "终端要用";
const SYSTEM = "系统要用";
const FIND = "终端里的查找要用";

const mod = (keys: string[], why: string, only?: Platform): Reserved[] => keys.map((k) => ({ chord: `Mod+${k}`, why, only }));

/**
 * Chords that cannot be bound, and that no default uses: the ones the browser keeps for itself (a page cannot take them,
 * or should not), the editing keys of any text field, the dialogs' Mod+Enter, and the control keys of a terminal on
 * Windows / Linux, where Mod is the terminal's Ctrl.
 */
export const RESERVED: readonly Reserved[] = [
  ...mod(["T", "W", "N", "R", "L", "D", "P", "S", "O", "U", "F", "G", "H", "0", "=", "-", "[", "]", "Shift+T", "Shift+N", "Shift+W", "Shift+R", "Shift+G", "Shift+I", "Shift+J", "Shift+C", "Shift+B"], BROWSER),
  ...mod(["Q", "M", "Alt+I"], BROWSER, "mac"),
  ...mod(["A", "C", "V", "X", "Z", "Y", "Shift+Z", "Shift+V", "Up", "Down", "Left", "Right", "Home", "End", "Shift+Left", "Shift+Right", "Shift+Up", "Shift+Down"], EDITING),
  ...mod(["Enter"], "对话框的提交要用"),
  ...mod(["Space"], SYSTEM),
  ...mod(["W", "R", "L", "U", "D", "Z"], TERMINAL, "other"),
  // the terminal's find bar takes Ctrl+Shift+F off the Mac (⌘F on a Mac is reserved above, with the browser's)
  ...mod(["Shift+F"], FIND, "other"),
];

/** Why a chord cannot be bound on this platform; null when it can. */
export function reservedWhy(chord: string, platform: Platform = currentPlatform()): string | null {
  const keys = new Set(expandChord(chord));
  return RESERVED.find((r) => (!r.only || r.only === platform) && keys.has(r.chord))?.why ?? null;
}

// ── the commands ─────────────────────────────────────────

/** Where a command works: everywhere, on a conversation / terminal page, or on the sidebar. */
export type ShortcutScope = "global" | "session" | "sidebar";

export const SCOPE_LABEL: Record<ShortcutScope, string> = { global: "全局", session: "会话页", sidebar: "侧边栏" };

export interface ShortcutDef {
  id: string;
  /** What the settings, the help overlay and the command palette call it. */
  label: string;
  /** The default chord (platform-independent, see Chord). */
  chord: string;
  scope: ShortcutScope;
  /** Still works with a dialog or the palette open (they are the ones that open those). */
  overlay?: boolean;
  /** Fires again while held (walking the sidebar's rows). */
  repeat?: boolean;
  icon?: IconName;
}

/**
 * Moshi's keymap, each mapped to the closest Persona action (docs/web.md「快捷键」):
 *   Mod+K 跳转 · Mod+Shift+P 命令面板 · Mod+J 终端 ⇄ 对话 · Mod+B 右侧面板 · Mod+Alt+B 侧边栏 · Mod+E 侧边栏分组 ·
 *   Mod+Shift+Enter 最大化中间区域 · Mod+I 输入框 · Mod+Alt+Shift+↑/↓ 折叠 / 展开 · Mod+Alt+↑/↓ 上 / 下一行 · Mod+, 设置 ·
 *   Mod+1–9 电脑 · Mod+Alt+←/→ 本机上 / 下一个标签页.
 * Not taken over: Moshi's Mod+Alt+1–3 (choose the right panel's tab), which Persona has no single place for.
 */
export const SHORTCUTS: readonly ShortcutDef[] = [
  { id: "jump", label: "跳转到会话或终端窗格", chord: "Mod+K", scope: "global", overlay: true, icon: "search" },
  { id: "commands", label: "命令面板", chord: "Mod+Shift+P", scope: "global", overlay: true, icon: "run" },
  { id: "help", label: "快捷键帮助", chord: "Mod+/", scope: "global", overlay: true, icon: "keyboard" },
  { id: "settings", label: "打开设置", chord: "Mod+,", scope: "global", icon: "gear" },
  { id: "machine", label: "切到第 1–9 台电脑", chord: `Mod+${DIGITS}`, scope: "global", icon: "machine" },
  { id: "left-sidebar", label: "显示 / 隐藏侧边栏", chord: "Mod+Alt+B", scope: "global", icon: "sidebar" },
  { id: "maximize", label: "最大化中间区域", chord: "Mod+Shift+Enter", scope: "global", icon: "layers" },
  { id: "terminal-chat", label: "终端 ⇄ 对话", chord: "Mod+J", scope: "session", icon: "terminal" },
  { id: "right-panel", label: "显示 / 隐藏右侧面板", chord: "Mod+B", scope: "session", icon: "sidebar" },
  { id: "focus-input", label: "聚焦输入框", chord: "Mod+I", scope: "session", icon: "keyboard" },
  { id: "prev-tab", label: "本机上一个标签页", chord: "Mod+Alt+Left", scope: "session", icon: "arrowLeft" },
  { id: "next-tab", label: "本机下一个标签页", chord: "Mod+Alt+Right", scope: "session", icon: "arrowRight" },
  { id: "sidebar-grouping", label: "切换侧边栏的分组方式", chord: "Mod+E", scope: "sidebar", icon: "filter" },
  { id: "prev-row", label: "侧边栏上一行", chord: "Mod+Alt+Up", scope: "sidebar", repeat: true, icon: "up" },
  { id: "next-row", label: "侧边栏下一行", chord: "Mod+Alt+Down", scope: "sidebar", repeat: true, icon: "arrowDown" },
  { id: "collapse-tree", label: "折叠侧边栏的全部分组", chord: "Mod+Alt+Shift+Up", scope: "sidebar", icon: "chevron" },
  { id: "expand-tree", label: "展开侧边栏的全部分组", chord: "Mod+Alt+Shift+Down", scope: "sidebar", icon: "chevron" },
];

export const shortcutDef = (id: string) => SHORTCUTS.find((s) => s.id === id);

/**
 * Keys a component handles itself, where it is the one that knows if they apply (the terminal's find bar defers to the
 * browser's own find when nothing is being driven). They are not in the registry, so they are not rebound and the
 * dispatcher never sees them; they are reserved (so no command is bound to them) and listed in the help and the settings.
 */
export interface LocalShortcut {
  id: string;
  label: string;
  /** The chord on each platform. */
  chord: Record<Platform, string>;
  /** What else to know, one line. */
  note?: string;
}

export const LOCAL_SHORTCUTS: readonly LocalShortcut[] = [
  { id: "terminal-find", label: "终端里查找", chord: { mac: "Mod+F", other: "Mod+Shift+F" }, note: "Windows / Linux 上没有接管终端时，Ctrl+F 也是" },
];

// ── the user's bindings ──────────────────────────────────

/** Only what differs from the defaults: command id → chord. */
export type Overrides = Record<string, string>;
export type Bindings = Record<string, string>;

export const STORAGE_KEY = "persona.shortcuts";

/** The effective chord of every command: the default unless the user rebound it. */
export function bindingsOf(overrides: Overrides): Bindings {
  const out: Bindings = {};
  for (const s of SHORTCUTS) out[s.id] = overrides[s.id] ?? s.chord;
  return out;
}

export type Check = { ok: true; chord: string } | { ok: false; reason: "mod" | "key" | "reserved" | "conflict" | "digits"; message: string; with?: string };

/**
 * Whether `pressed` can be the chord of command `id`. `pressed` is what the user pressed (a Chord with `mod`); the
 * answer is the canonical chord, or why not: no Mod ("需要带 Ctrl"), a key chords do not use, a reserved chord, or
 * another command's ("已被「X」占用").
 */
export function checkBinding(id: string, pressed: (Chord & { mod: boolean }) | null, bindings: Bindings, platform: Platform = currentPlatform()): Check {
  if (!pressed) return { ok: false, reason: "key", message: "这个键不能用作快捷键" };
  if (!pressed.mod) return { ok: false, reason: "mod", message: `需要带 ${modName(platform)}` };
  const def = shortcutDef(id);
  let chord = chordText(pressed);
  if (def && parseChord(def.chord)?.key === DIGITS) {
    if (!/^[1-9]$/.test(pressed.key)) return { ok: false, reason: "digits", message: "这一项要按数字键 1–9" };
    chord = chordText({ ...pressed, key: DIGITS });
  }
  const why = reservedWhy(chord, platform);
  if (why) return { ok: false, reason: "reserved", message: `${formatChord(chord, platform)} ${why}，换一个` };
  const mine = new Set(expandChord(chord));
  for (const other of SHORTCUTS) {
    if (other.id === id) continue;
    if (expandChord(bindings[other.id] ?? other.chord).some((c) => mine.has(c))) return { ok: false, reason: "conflict", message: `已被「${other.label}」占用`, with: other.id };
  }
  return { ok: true, chord };
}

/** What localStorage held, kept to what still makes sense: known commands, valid chords that are not reserved. */
export function sanitizeOverrides(raw: unknown, platform: Platform = currentPlatform()): Overrides {
  const out: Overrides = {};
  if (!raw || typeof raw !== "object") return out;
  for (const [id, v] of Object.entries(raw as Record<string, unknown>)) {
    const def = shortcutDef(id);
    const chord = typeof v === "string" ? canonChord(v) : null;
    if (!def || !chord || chord === canonChord(def.chord) || reservedWhy(chord, platform)) continue;
    out[id] = chord;
  }
  // two commands on one chord (a default moved since): the first keeps it, the other goes back to its default
  const seen = new Set<string>();
  for (const s of SHORTCUTS) {
    const chords = expandChord(out[s.id] ?? s.chord);
    if (chords.some((c) => seen.has(c))) delete out[s.id];
    for (const c of expandChord(out[s.id] ?? s.chord)) seen.add(c);
  }
  return out;
}

function load(): Overrides {
  try {
    return sanitizeOverrides(JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "null"));
  } catch {
    return {};
  }
}

let overrides: Overrides = typeof localStorage === "undefined" ? {} : load();
let snapshot: Bindings = bindingsOf(overrides);
const subscribers = new Set<() => void>();

function commit(next: Overrides) {
  overrides = next;
  snapshot = bindingsOf(next);
  try {
    if (Object.keys(next).length) localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
    else localStorage.removeItem(STORAGE_KEY);
  } catch {
    // storage full or blocked: the bindings last until the page is closed
  }
  subscribers.forEach((f) => f());
}

export const getBindings = (): Bindings => snapshot;
export const getOverrides = (): Overrides => overrides;

/** Binds a command to a chord (the caller has checked it); the default chord clears the override. */
export function setBinding(id: string, chord: string) {
  const def = shortcutDef(id);
  const canon = canonChord(chord);
  if (!def || !canon) return;
  const next = { ...overrides };
  if (canon === canonChord(def.chord)) delete next[id];
  else next[id] = canon;
  commit(next);
}

export function resetBinding(id: string) {
  if (!(id in overrides)) return;
  const next = { ...overrides };
  delete next[id];
  commit(next);
}

export function resetAllBindings() {
  if (Object.keys(overrides).length) commit({});
}

function subscribe(f: () => void) {
  subscribers.add(f);
  return () => void subscribers.delete(f);
}

// another tab rebinding
if (typeof addEventListener !== "undefined") {
  addEventListener("storage", (e) => {
    if (e.key === STORAGE_KEY || e.key === null) commit(load());
  });
}

/** The effective chords, live. */
export function useBindings(): Bindings {
  return useSyncExternalStore(subscribe, getBindings, getBindings);
}

/** The hint on the sidebar's search box: the ⌘K it has always shown while 跳转 has its default chord, else the chord it has now. */
export function jumpHint(bindings: Bindings, platform: Platform = currentPlatform()): string {
  const def = shortcutDef("jump")!;
  return bindings.jump === def.chord || !bindings.jump ? "⌘K" : formatChord(bindings.jump, platform);
}

// ── handlers and the dispatcher ──────────────────────────

/** Where the key was pressed: in a terminal (xterm's helper textarea), in a text field, or on the page. */
export type FocusKind = "terminal" | "input" | "page";

interface ElementLike {
  closest?: (selector: string) => unknown;
  tagName?: string;
  isContentEditable?: boolean;
}

export function focusKind(target: unknown): FocusKind {
  const el = target as ElementLike | null;
  if (!el || typeof el.closest !== "function") return "page";
  if (el.closest(".xterm")) return "terminal";
  if (el.closest("input, textarea, select, [contenteditable=''], [contenteditable='true']") || el.isContentEditable) return "input";
  return "page";
}

export interface ShortcutCall {
  /** Where the key was pressed. */
  focus: FocusKind;
  /** The digit of the chord, for the command that takes 1–9. */
  digit?: number;
  /** Set when the key started it; undefined when the command palette runs it. */
  event?: KeyEventLike;
}

/** Returns false when the command does not apply here (no conversation on the page…): the key is then left alone. */
export type ShortcutHandler = (call: ShortcutCall) => boolean | void;

const handlers = new Map<string, ShortcutHandler>();

/** Registers what a command does (the shell does, while it is mounted); returns the way to take it back. */
export function registerShortcut(id: string, run: ShortcutHandler): () => void {
  handlers.set(id, run);
  return () => {
    if (handlers.get(id) === run) handlers.delete(id);
  };
}

export const handlerOf = (id: string): ShortcutHandler | undefined => handlers.get(id);

/** Runs a command by its id (the command palette): false when it has no handler here or the handler says it does not apply. */
export function runShortcut(id: string, call: Partial<ShortcutCall> = {}): boolean {
  const run = handlers.get(id);
  if (!run) return false;
  return run({ focus: "page", ...call }) !== false;
}

/** Keys being recorded in the settings: the dispatcher stands aside. */
let recording = 0;
export function holdShortcuts(): () => void {
  recording++;
  let released = false;
  return () => {
    if (!released) recording--;
    released = true;
  };
}

/** The command a key press is for: the one bound to its chord, and the digit for 1–9. */
export function matchShortcut(e: KeyEventLike, bindings: Bindings = snapshot, platform: Platform = currentPlatform()): { def: ShortcutDef; digit?: number } | undefined {
  if (e.isComposing || e.keyCode === 229) return undefined;
  const chord = chordOfEvent(e, platform);
  if (!chord) return undefined;
  for (const def of SHORTCUTS) {
    const bound = bindings[def.id] ?? def.chord;
    if (!expandChord(bound).includes(chord)) continue;
    const digit = parseChord(bound)?.key === DIGITS ? Number(chord.split("+").pop()) : undefined;
    return { def, digit };
  }
  return undefined;
}

/**
 * The terminal's key handler asks this first: true when the key is one of the app's shortcuts (the app takes it, xterm
 * must not send it to the program). One call is all the terminal needs: `if (passesToApp(e)) return false`.
 */
export function passesToApp(e: KeyEventLike, bindings: Bindings = snapshot, platform: Platform = currentPlatform()): boolean {
  if (recording > 0) return false;
  const hit = matchShortcut(e, bindings, platform);
  return Boolean(hit && handlers.has(hit.def.id));
}

export interface Dispatchable extends KeyEventLike {
  target?: unknown;
  defaultPrevented?: boolean;
  preventDefault(): void;
  stopPropagation(): void;
}

export interface DispatchEnv {
  platform?: Platform;
  bindings?: Bindings;
  /** A dialog or the palette is open: only the commands that work over them run. */
  overlay?: boolean;
}

/** Runs the command a keydown is for. true: it did, and the event is consumed (no browser action, no one else sees it). */
export function dispatchKey(e: Dispatchable, env: DispatchEnv = {}): boolean {
  if (recording > 0 || e.defaultPrevented) return false;
  const hit = matchShortcut(e, env.bindings, env.platform);
  if (!hit) return false;
  if (env.overlay && !hit.def.overlay) return false;
  const run = handlers.get(hit.def.id);
  if (!run) return false;
  // held down: one toggle, not a flicker (the browser's own action for the chord is still held off)
  if (e.repeat && !hit.def.repeat) {
    e.preventDefault();
    e.stopPropagation();
    return true;
  }
  if (run({ focus: focusKind(e.target), digit: hit.digit, event: e }) === false) return false;
  e.preventDefault();
  e.stopPropagation();
  return true;
}
