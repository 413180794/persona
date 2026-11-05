// The full herdr surface's feed (docs/terminal.md「完整 herdr 界面」): herdr's own client, run in a PTY on the computer,
// drawn by a plain xterm. Not TerminalFeed's job — that one mirrors a pane's grid frame by frame — so this is a feed of
// its own with the same face for what the page shares (the key bar, the find bar, the right-click menu): the hub relays
// bytes both ways over the app WebSocket (herdr_open / herdr_data / herdr_input / herdr_resize / herdr_ack / herdr_close,
// server/internal/hub/herdrclient.go) and this keeps xterm in step, acknowledging what it has parsed so the computer
// never runs more than a window ahead.
import type { Terminal as XTerm } from "@xterm/xterm";
import type { FitAddon as XFit } from "@xterm/addon-fit";
import type { SearchAddon as XSearch } from "@xterm/addon-search";
import { t as tr } from "../../../i18n";
import { openLink } from "../../../lib/links";
import { getState, isOpen, onHubEvent, send } from "../../../lib/store";
import { passesToApp } from "../../../lib/shortcuts";
import {
  NO_MODIFIERS,
  encodeKey,
  keyDecision,
  keyNameForBytes,
  modifierHeld,
  modifierReleased,
  modifierTapped,
  modifiersActive,
  modifiersPressed,
  terminalPalette,
  typedWithModifiers,
  withModifiers,
  type ModifierKey,
  type Modifiers,
  type TerminalPalette,
} from "../../../lib/terminal";
import { copyNotice, copyWith, osc52Text } from "../../../lib/terminal-clipboard";
import { hex6, lineEditBytes, parseCursorStyle, parseFlag, webLink, type CursorStyle } from "../../../lib/terminal-details";
import { FIND_LIMIT } from "../../../lib/terminal-search";
import {
  AckTracker,
  FULL_SCROLLBACK,
  HINT_KEY,
  colourReports,
  dimLine,
  endStatus,
  endText,
  fullBytes,
  fullGrid,
  schemeModeReport,
  schemeReport,
  type FullStatus,
} from "../../../lib/terminal-mode";
import { toast } from "../../../ui/atoms";
import { cjkFontStack, terminalFontReady } from "./cjk";
import { browserClipboard } from "./clipboard";

export interface FullView {
  /** xterm is loaded and open. */
  ready: boolean;
  status: FullStatus;
  /** Why the view is not live (the reason the computer or the hub gave, in words). */
  note: string;
  cols: number;
  rows: number;
  /** "active" while keys can go in (the key bar's switch for being enabled). */
  control: "active" | "off";
  mods: Modifiers;
  selection: boolean;
  find: { index: number; count: number } | null;
  palette: TerminalPalette | null;
  /** The view ended before it was ever live and the reason is the computer's or the hub's, not a lost connection: this machine cannot show it. */
  failedToStart: boolean;
}

const RESIZE_MS = 120;
/** One herdr_input carries at most this many UTF-16 units: a big paste goes in several (the hub caps one at 1 MiB). */
const INPUT_CHUNK = 32 << 10;
const FONT_KEY = "jarvis.terminal.fontSize";
const CURSOR_KEY = "jarvis.terminal.cursorStyle";
const BLINK_KEY = "jarvis.terminal.cursorBlink";
const FONT_RANGE = { min: 9, max: 20, default: 13 };

function savedFontSize(): number {
  const n = Number(localStorage.getItem(FONT_KEY));
  return Number.isFinite(n) && n >= FONT_RANGE.min && n <= FONT_RANGE.max ? n : FONT_RANGE.default;
}

let views = 0;

export class FullFeed {
  private view: FullView;
  private listeners = new Set<() => void>();
  private term: XTerm | null = null;
  private fit: XFit | null = null;
  private search: XSearch | null = null;
  private cleanup: (() => void)[] = [];
  private disposed = false;
  /** The id of the view open on the hub ("" when none): messages about other ids are an earlier view's. */
  private id = "";
  private ack: AckTracker | null = null;
  /** Let go on purpose (the surface is leaving or hidden): nothing is opened until `resume`. */
  private released = false;
  private resumeAt = 0;
  private grid: { cols: number; rows: number } | null = null;
  private sent: { cols: number; rows: number } | null = null;
  private resizeTimer = 0;
  private openTimer = 0;
  /** The TUI turned on colour-scheme notifications (DEC 2031). */
  private schemeNotify = false;
  private found = { query: "", cs: false, busy: false };
  private everLive = false;

  constructor(
    readonly mid: string,
    readonly sid: string,
  ) {
    this.view = {
      ready: false,
      status: "connecting",
      note: "",
      cols: 0,
      rows: 0,
      control: "off",
      mods: NO_MODIFIERS,
      selection: false,
      find: null,
      palette: null,
      failedToStart: false,
    };
  }

  // ── React ─────────────────────────────────────────────

  subscribe = (l: () => void) => {
    this.listeners.add(l);
    return () => {
      this.listeners.delete(l);
    };
  };

  snapshot = () => this.view;

  private patch(p: Partial<FullView>) {
    const v = this.view as unknown as Record<string, unknown>;
    if (!Object.entries(p).some(([k, x]) => v[k] !== x)) return;
    this.view = { ...this.view, ...p };
    this.listeners.forEach((l) => l());
  }

  // ── lifecycle ─────────────────────────────────────────

  /** Builds xterm in `host`, then opens the view once there is a grid and a connection. `delay`: wait this long first. */
  async mount(host: HTMLElement, delay = 0) {
    let mod: typeof import("./xterm");
    try {
      mod = await import("./xterm");
    } catch {
      if (!this.disposed) this.patch({ status: "error", note: tr("terminal-full.load_failed") });
      return;
    }
    if (this.disposed) return;
    const { Terminal, FitAddon, ClipboardAddon, WebLinksAddon, SearchAddon } = mod;
    const size = savedFontSize();
    const family = getComputedStyle(host).fontFamily;
    await terminalFontReady(size, family);
    if (this.disposed) return;
    const palette = this.theme();
    const term = new Terminal({
      fontFamily: family,
      fontSize: size,
      lineHeight: 1, // herdr draws its borders and bars with box characters: with any gap between rows they come out dashed
      scrollback: FULL_SCROLLBACK,
      cursorBlink: parseFlag(localStorage.getItem(BLINK_KEY)),
      cursorStyle: parseCursorStyle(localStorage.getItem(CURSOR_KEY)) satisfies CursorStyle,
      cursorInactiveStyle: "outline",
      allowProposedApi: true, // the search addon marks its matches with decorations
      macOptionIsMeta: true,
      theme: palette,
      linkHandler: { activate: (_e, uri) => this.openWebLink(uri) }, // OSC 8 links
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.loadAddon(new WebLinksAddon((_e, uri) => this.openWebLink(uri)));
    const search = new SearchAddon({ highlightLimit: FIND_LIMIT });
    term.loadAddon(search);
    // herdr's own client copies with OSC 52 (a selection, or a pane's program): a write goes to the clipboard, a read is
    // refused — nothing running on the computer gets to read what the user copied.
    term.loadAddon(new ClipboardAddon(undefined, { readText: () => "", writeText: (_selection: string, text: string) => this.programCopied(text) }));
    term.open(host);
    this.term = term;
    this.fit = fit;
    this.search = search;

    const parser = term.parser;
    const handlers = [
      // what herdr asks of the terminal it draws in: the colour scheme (CSI ? 996 n), and to be told when it changes
      parser.registerCsiHandler({ prefix: "?", final: "n" }, (p) => (p.length === 1 && p[0] === 996 ? (this.raw(schemeReport(this.dark())), true) : false)),
      ...(["h", "l"] as const).map((final) =>
        parser.registerCsiHandler({ prefix: "?", final }, (p) => {
          if (!p.includes(2031)) return false;
          this.schemeNotify = final === "h";
          return p.length === 1; // other modes in the same sequence go on to xterm
        }),
      ),
      parser.registerCsiHandler({ prefix: "?", intermediates: "$", final: "p" }, (p) =>
        p.length === 1 && p[0] === 2031 ? (this.raw(schemeModeReport(this.schemeNotify)), true) : false,
      ),
    ];
    const data = term.onData((d) => this.type(d));
    const selected = term.onSelectionChange(() => this.patch({ selection: term.hasSelection() }));
    const results = search.onDidChangeResults((r) => this.found.query && this.patch({ find: { index: r.resultIndex, count: r.resultCount } }));
    term.attachCustomKeyEventHandler(this.key);
    const colors = new MutationObserver(() => {
      const next = this.theme();
      this.patch({ palette: next });
      if (this.term) this.term.options.theme = next;
      if (this.schemeNotify) this.raw(colourReports(next.foreground, next.background) + schemeReport(this.dark()));
    });
    colors.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
    const sizes = new ResizeObserver(() => this.measure());
    sizes.observe(host);
    const offs = [onHubEvent("herdr_state", this.onState), onHubEvent("herdr_data", this.onData), onHubEvent("herdr_end", this.onEnd), onHubEvent("hello", this.onHello)];
    this.cleanup.push(
      ...handlers.map((h) => () => h.dispose()),
      () => data.dispose(),
      () => selected.dispose(),
      () => results.dispose(),
      () => colors.disconnect(),
      () => sizes.disconnect(),
      ...offs,
    );

    this.patch({ ready: true, palette });
    void cjkFontStack(family).then((stack) => {
      if (stack && !this.disposed && this.term) this.term.options.fontFamily = stack; // the cell is the same width: nothing resizes
    });
    this.measure(true);
    this.resumeAt = Date.now() + delay;
    this.begin();
  }

  /** Leaves: the hub stops the client on the computer, xterm goes. */
  dispose() {
    if (this.disposed) return;
    this.disposed = true;
    this.close();
    clearTimeout(this.resizeTimer);
    clearTimeout(this.openTimer);
    this.cleanup.forEach((f) => f());
    this.cleanup = [];
    this.term?.dispose();
    this.term = null;
    this.listeners.clear();
  }

  /** Let go on purpose and not opened again yet (`reconnect` does). */
  get isReleased() {
    return this.released;
  }

  /** Lets go of the client on the computer but keeps what xterm shows (the surface fading out, a hidden terminal). */
  release() {
    this.released = true;
    clearTimeout(this.openTimer);
    this.close();
    this.patch({ status: "closed", note: "", control: "off" });
  }

  /** Opens a view again after `release`, or after it ended (重新连接): a clean screen, the client draws it all. */
  reconnect() {
    if (this.disposed) return;
    this.released = false;
    this.close();
    this.term?.reset();
    this.patch({ status: "connecting", note: "", control: "off", failedToStart: false });
    this.resumeAt = 0;
    this.begin();
  }

  private close() {
    if (this.id) send({ type: "herdr_close", id: this.id });
    this.id = "";
    this.ack?.dispose();
    this.ack = null;
  }

  private dark() {
    return document.documentElement.classList.contains("dark");
  }

  private theme(): TerminalPalette {
    const css = getComputedStyle(document.documentElement);
    return terminalPalette((n) => css.getPropertyValue(n), this.dark());
  }

  // ── the view on the hub ───────────────────────────────

  private linkUp() {
    return isOpen() && getState().conn === "online";
  }

  /** Opens the view when everything is there for it: xterm sized, the hub reachable, and not let go. */
  private begin() {
    if (this.disposed || this.released || this.id || !this.term) return;
    const wait = this.resumeAt - Date.now();
    if (wait > 0) {
      clearTimeout(this.openTimer);
      this.openTimer = window.setTimeout(() => this.begin(), wait);
      return;
    }
    if (!getState().features.includes("herdr_client")) return void this.patch({ status: "unsupported", note: tr("terminal-full.hub_unsupported") });
    if (!this.grid || !this.linkUp()) return; // the first measure or the hub's hello calls again
    this.id = `v${(++views).toString(36)}${Math.random().toString(36).slice(2, 8)}`;
    const id = this.id;
    this.sent = this.grid;
    this.ack = new AckTracker((bytes) => send({ type: "herdr_ack", id, bytes }));
    this.patch({ status: "connecting", note: "" });
    send({ type: "herdr_open", id, machine_id: this.mid, session_id: this.sid, cols: this.grid.cols, rows: this.grid.rows, focus: true });
  }

  private onHello = () => {
    if (this.disposed || this.released) return;
    // the connection dropped and came back: the hub let go of the view with it; open another on a clean screen
    if (this.id || this.view.status === "live") {
      this.id = "";
      this.ack?.dispose();
      this.ack = null;
      this.term?.reset();
      this.patch({ status: "connecting", note: "", control: "off" });
    }
    this.begin();
  };

  private onState = (msg: Record<string, unknown>) => {
    if (msg.id !== this.id || !this.id) return;
    this.goLive(Number(msg.cols), Number(msg.rows));
  };

  private goLive(cols: number, rows: number) {
    if (this.view.status === "live") return;
    this.everLive = true;
    this.patch({ status: "live", note: "", control: "active", cols: cols || this.view.cols, rows: rows || this.view.rows });
    this.term?.focus();
    this.hint();
    if (this.grid && this.sent && (this.grid.cols !== this.sent.cols || this.grid.rows !== this.sent.rows)) this.measure(true); // it changed while the client started
  }

  private onData = (msg: Record<string, unknown>) => {
    if (msg.id !== this.id || !this.id || !this.term) return;
    const bytes = fullBytes(msg.data);
    if (!bytes) return;
    if (this.view.status === "connecting") this.goLive(this.grid?.cols ?? 0, this.grid?.rows ?? 0); // data can come ahead of the state message
    const ack = this.ack;
    this.term.write(bytes, () => ack?.noteParsed(bytes.length));
  };

  private onEnd = (msg: Record<string, unknown>) => {
    if (msg.id !== this.id || !this.id) return;
    const end = { error: typeof msg.error === "string" ? msg.error : undefined, code: typeof msg.code === "string" ? msg.code : undefined, unsupported: msg.unsupported === true };
    const status = endStatus(end);
    const note = endText(end);
    this.id = "";
    this.ack?.dispose();
    this.ack = null;
    this.term?.write(dimLine(note));
    this.patch({ status, note, control: "off", failedToStart: !this.everLive && end.code !== "offline" });
  };

  /** Said once in this browser: this surface shares herdr's layout with every other client. */
  private hint() {
    if (localStorage.getItem(HINT_KEY)) return;
    try {
      localStorage.setItem(HINT_KEY, "1");
    } catch {
      // not kept: it is said again next time
    }
    toast(tr("terminal-full.hint_toast"), "info");
  }

  // ── size ──────────────────────────────────────────────

  private measure(now = false) {
    clearTimeout(this.resizeTimer);
    const run = () => {
      if (this.disposed || !this.fit || !this.term) return;
      const g = fullGrid(this.fit.proposeDimensions());
      if (!g) return;
      this.grid = g;
      if (this.term.cols !== g.cols || this.term.rows !== g.rows) this.term.resize(g.cols, g.rows);
      this.patch({ cols: g.cols, rows: g.rows });
      if (this.id && (!this.sent || this.sent.cols !== g.cols || this.sent.rows !== g.rows) && this.view.status === "live") {
        this.sent = g;
        send({ type: "herdr_resize", id: this.id, cols: g.cols, rows: g.rows });
      }
      this.begin();
    };
    if (now || !this.grid) run();
    else this.resizeTimer = window.setTimeout(run, RESIZE_MS);
  }

  // ── input ─────────────────────────────────────────────

  /** Bytes for the client, as they are (the grid it draws in is the page's; keys, mouse reports and pastes all come here). */
  private raw(text: string) {
    if (!text || !this.id || this.view.status !== "live") return;
    for (let i = 0; i < text.length; ) {
      let end = Math.min(text.length, i + INPUT_CHUNK);
      const last = text.charCodeAt(end - 1);
      if (end < text.length && last >= 0xd800 && last <= 0xdbff) end--; // not between the halves of a surrogate pair
      send({ type: "herdr_input", id: this.id, data: text.slice(i, end) });
      i = end;
    }
  }

  /** What xterm makes of the keys typed on it; with the key bar's ctrl / alt / shift down, that key. */
  private type(data: string) {
    const m = this.view.mods;
    if (!modifiersActive(m)) return this.raw(data);
    const name = keyNameForBytes(data);
    if (name) {
      this.sendKey(withModifiers(m, name));
    } else if ([...data].length === 1) {
      const t = typedWithModifiers(m, data);
      if (t.key) this.sendKey(t.key);
      else if (t.text) this.raw(t.text);
    } else {
      this.raw(data);
    }
    this.patch({ mods: modifiersPressed(m) });
  }

  private sendKey(name: string) {
    const b = encodeKey(name, this.term?.modes.applicationCursorKeysMode ?? false);
    if (b) this.raw(b);
  }

  get driving() {
    return this.view.status === "live";
  }

  pressKey(name: string) {
    if (!this.driving) return;
    const m = this.view.mods;
    this.sendKey(withModifiers(m, name));
    this.patch({ mods: modifiersPressed(m) });
  }

  pressBare(name: string) {
    if (this.driving) this.sendKey(name);
  }

  tapModifier(k: ModifierKey, quick: boolean) {
    this.patch({ mods: modifierTapped(this.view.mods, k, quick) });
  }

  holdModifier(k: ModifierKey) {
    this.patch({ mods: modifierHeld(this.view.mods, k) });
  }

  releaseModifier(k: ModifierKey) {
    this.patch({ mods: modifierReleased(this.view.mods, k) });
  }

  /** A saved command: pasted into the client (bracketed when the program asked for that), and run with return when `run`. */
  typeText(text: string, run: boolean) {
    if (!this.driving || !text) return;
    this.term?.paste(text);
    if (run) this.raw("\r");
    this.term?.focus();
  }

  /** The key bar's paste: what the clipboard has, as ⌘V does. */
  async pasteClipboard() {
    if (!this.driving) return;
    if (!navigator.clipboard?.readText) return void toast(tr("terminal-full.clipboard_https"));
    let text = "";
    try {
      text = (await navigator.clipboard.readText()) ?? "";
    } catch {
      return void toast(tr("terminal-full.clipboard_denied"));
    }
    if (!text) return void toast(tr("terminal-full.clipboard_empty"));
    this.term?.paste(text);
  }

  focus() {
    this.term?.focus();
  }

  /** Whether the program under it takes the mouse (herdr does): then a right click is its own, not the page's menu. */
  get mouseTracked() {
    return (this.term?.modes.mouseTrackingMode ?? "none") !== "none";
  }

  /**
   * Keys the page takes before xterm: one of the app's shortcuts (the app takes it), a copy with text selected, shift+enter
   * as a new line, Mac line editing; an IME composition is left alone (keyDecision).
   */
  private key = (e: KeyboardEvent) => {
    if (passesToApp(e)) return false;
    if (this.driving) {
      const bytes = lineEditBytes(e);
      if (bytes) {
        e.preventDefault();
        e.stopPropagation();
        this.raw(bytes);
        return false;
      }
    }
    const d = keyDecision(e, this.term?.hasSelection() ?? false);
    if (d === "xterm") return true;
    if (e.type === "keydown") {
      if (d === "copy") this.copySelection();
      else if (d === "newline") this.raw("\x1b\r");
    }
    e.preventDefault();
    e.stopPropagation();
    return false;
  };

  private openWebLink(uri: string) {
    const url = webLink(uri);
    if (url) openLink(url);
  }

  // ── the clipboard ─────────────────────────────────────

  copySelection() {
    const text = this.term?.getSelection() ?? "";
    if (!text) return void toast(tr("terminal-full.select_first_herdr"));
    void copyWith(text, browserClipboard()).then((outcome) => {
      const n = copyNotice(outcome, window.isSecureContext);
      toast(n.text, n.tone);
      if (outcome !== "failed") this.term?.clearSelection();
    });
  }

  copyText(text: string) {
    if (!text) return void toast(tr("terminal-full.select_first"));
    void copyWith(text, browserClipboard()).then((outcome) => {
      const n = copyNotice(outcome, window.isSecureContext);
      toast(n.text, n.tone);
    });
  }

  hasSelection() {
    return this.term?.hasSelection() ?? false;
  }

  selectedText() {
    return this.term?.getSelection() ?? "";
  }

  selectAll() {
    this.term?.selectAll();
  }

  clearSelection() {
    this.term?.clearSelection();
  }

  /** herdr copied something (OSC 52): onto the clipboard at once where the browser allows it, else the user taps 复制 (it wants a gesture). */
  private programCopied(text: string) {
    const t = osc52Text(text);
    if (!t) return;
    void copyWith(t, browserClipboard()).then((outcome) => {
      if (outcome !== "failed") return toast(tr("terminal-full.copied"), "ok");
      toast(tr("terminal-full.copy_prompt"), "info", {
        label: tr("terminal-full.copy"),
        run: () =>
          void copyWith(t, browserClipboard()).then((o) => {
            const n = copyNotice(o, window.isSecureContext);
            toast(n.text, n.tone);
          }),
      });
    });
  }

  // ── find ──────────────────────────────────────────────

  private searchOptions() {
    const css = getComputedStyle(document.documentElement);
    const c = (name: string) => hex6(css.getPropertyValue(name));
    const border = c("--orange"), tint = c("--accent-tint"), accent = c("--accent");
    const decorations =
      border && tint && accent
        ? { matchBorder: border, matchOverviewRuler: border, activeMatchBackground: tint, activeMatchBorder: accent, activeMatchColorOverviewRuler: accent }
        : undefined;
    return { caseSensitive: this.found.cs, decorations };
  }

  /** Looks for `query` in xterm's screen and scrollback. dir 0 starts afresh from the newest; 1 / -1 step, wrapping around. */
  find(query: string, caseSensitive: boolean, dir: 0 | 1 | -1) {
    const search = this.search;
    if (!query || !search || !this.term) return this.clearFind();
    const fresh = dir === 0 || query !== this.found.query || caseSensitive !== this.found.cs;
    this.found = { query, cs: caseSensitive, busy: true };
    try {
      if (fresh) {
        this.term.clearSelection();
        search.clearDecorations(); // also forgets the last term: the addon does not notice a changed 区分大小写 on its own
        search.findPrevious(query, this.searchOptions());
      } else if (dir === -1) search.findPrevious(query, this.searchOptions());
      else search.findNext(query, this.searchOptions());
    } finally {
      this.found.busy = false;
    }
  }

  clearFind() {
    this.found = { query: "", cs: false, busy: false };
    this.search?.clearDecorations();
    this.patch({ find: null });
  }
}
