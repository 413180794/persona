// Details of the web terminal that decide things without touching the page (so they are tested): the Mac line-editing
// keys, which links are opened, the right-click menu, when the terminal takes the focus back, and the cursor options.
// The pieces that need xterm or the DOM are in views/session/terminal/.
import { t } from "../i18n";

// ── Mac line-editing keys ───────────────────────────────

/** The fields of a KeyboardEvent the key mapping reads. */
export interface MacKeyEvent {
  type: string;
  key: string;
  ctrlKey: boolean;
  metaKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
  isComposing?: boolean;
  keyCode?: number;
}

/**
 * The bytes a Mac line-editing chord stands for, as Terminal.app and iTerm2 send them: ⌘⌫ kills the line before the
 * cursor (^U), ⌘← goes to its start (^A), ⌘→ to its end (^E). Only a bare ⌘ chord on keydown; null for any other key.
 * Browsers would otherwise take ⌘← / ⌘→ as Back / Forward, and xterm sends nothing for ⌘⌫.
 */
export function lineEditBytes(e: MacKeyEvent): string | null {
  if (e.type !== "keydown" || e.isComposing || e.keyCode === 229) return null;
  if (!e.metaKey || e.ctrlKey || e.altKey || e.shiftKey) return null;
  switch (e.key) {
    case "Backspace":
      return "\x15";
    case "ArrowLeft":
      return "\x01";
    case "ArrowRight":
      return "\x05";
    default:
      return null;
  }
}

// ── links ───────────────────────────────────────────────

/** The address of a link the terminal may open (web pages only), or null: anything else a program prints stays text. */
export function webLink(uri: string): string | null {
  if (!/^https?:\/\//i.test(uri)) return null;
  let u: URL;
  try {
    u = new URL(uri);
  } catch {
    return null;
  }
  if ((u.protocol !== "http:" && u.protocol !== "https:") || !u.hostname) return null;
  return u.href;
}

// ── the right-click menu ────────────────────────────────

/**
 * Whether a right click gets Persona's menu: not with Shift held (the browser's own menu stays one chord away, as in
 * other web terminals), and not from a touch long press (which selects text on a phone).
 */
export function wantsOwnMenu(e: { shiftKey: boolean; pointerType?: string }): boolean {
  return !e.shiftKey && e.pointerType !== "touch";
}

export type TerminalMenuKey = "copy" | "paste" | "selectAll" | "search" | "clearSelection";

export interface TerminalMenuEntry {
  key: TerminalMenuKey;
  label: string;
  disabled: boolean;
  divider?: boolean;
}

/** The menu's rows: copy and clear need a selection, paste needs the terminal to be driven. */
export function terminalMenu(s: { selection: boolean; driving: boolean }): TerminalMenuEntry[] {
  return [
    { key: "copy", label: t("lib-terminal-details.copy"), disabled: !s.selection },
    { key: "paste", label: t("lib-terminal-details.paste"), disabled: !s.driving },
    { key: "selectAll", label: t("lib-terminal-details.select_all"), disabled: false, divider: true },
    { key: "search", label: t("lib-terminal-details.search"), disabled: false },
    { key: "clearSelection", label: t("lib-terminal-details.clear_selection"), disabled: !s.selection },
  ];
}

// ── focus ───────────────────────────────────────────────

/** What is true of the page when the terminal might take the keyboard back. */
export interface FocusFacts {
  /** This browser drives the pane (keys go in). */
  driving: boolean;
  /** The terminal is in front: shown, not under a kept-but-hidden page. */
  visible: boolean;
  /** Text is selected in the terminal or the panel (a click that took the focus would not lose it, but the next key would). */
  selection: boolean;
  /** A dialog, menu or popover is open. */
  overlayOpen: boolean;
  /** The focus is in an input, textarea, select or editable element that is not the terminal's own. */
  otherInput: boolean;
  /** The focus is on a control outside the panel (the sidebar, the page): the user went there on purpose. */
  outside: boolean;
}

/** Driving, a click in the panel or the window coming back gives the keyboard to the terminal unless it would take it from something. */
export function shouldRefocus(f: FocusFacts): boolean {
  return f.driving && f.visible && !f.selection && !f.overlayOpen && !f.otherInput && !f.outside;
}

// ── options ─────────────────────────────────────────────

export type CursorStyle = "block" | "bar" | "underline";

export const CURSOR_STYLES: { value: CursorStyle; label: string }[] = [
  { value: "block", label: t("lib-terminal-details.cursor_block") },
  { value: "bar", label: t("lib-terminal-details.cursor_bar") },
  { value: "underline", label: t("lib-terminal-details.cursor_underline") },
];

export const DEFAULT_CURSOR: CursorStyle = "block";

/** The saved cursor style; anything unknown is the default. */
export function parseCursorStyle(raw: string | null): CursorStyle {
  return CURSOR_STYLES.some((c) => c.value === raw) ? (raw as CursorStyle) : DEFAULT_CURSOR;
}

/** A saved on / off option: only "1" is on. */
export function parseFlag(raw: string | null): boolean {
  return raw === "1";
}

/** `#rrggbb` for an xterm search decoration (it takes no alpha), from a page token like `#3d9aff29`, `#fa0` or `#ef720c`; null when it is not hex. */
export function hex6(color: string): string | null {
  const m = /^#([0-9a-f]{3}|[0-9a-f]{6}|[0-9a-f]{8})$/i.exec(color.trim());
  if (!m) return null;
  const h = m[1].toLowerCase();
  if (h.length === 3) return `#${h[0]}${h[0]}${h[1]}${h[1]}${h[2]}${h[2]}`;
  return `#${h.slice(0, 6)}`;
}
