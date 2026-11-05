// A session's terminal in the side panel, as the iOS TerminalFeed (docs/terminal.md, docs/protocol.md「终端流」): the
// hub's live stream of the pane written into xterm and continued after a reconnect, or — with a hub, node or herdr
// that cannot stream — the colored screen read once a second.
//
// Driving it (the default where the hub can, `terminal_control`): the pane takes the panel's columns × rows, so the
// program lays itself out for the panel; keys, the wheel and clicks go to it. Watching: xterm is the pane's own size
// and the panel scrolls over it; nothing goes in.
import type { Terminal as XTerm } from "@xterm/xterm";
import type { FitAddon as XFit } from "@xterm/addon-fit";
import { api, errorText, isAbort, sessionPath } from "../../../lib/api";
import { openLink } from "../../../lib/links";
import { getState, isOpen, onHubEvent, send } from "../../../lib/store";
import {
  FrameOrder,
  HISTORY_PAGE,
  NO_MODIFIERS,
  WheelLines,
  backoff,
  cellAt,
  decodeBase64,
  encodeKey,
  historyAnchor,
  isStale,
  keyDecision,
  keyNameForBytes,
  lineCount,
  modifierHeld,
  modifierReleased,
  modifierTapped,
  modifiersActive,
  modifiersPressed,
  parseTerminalPush,
  pasteBytes,
  terminalPalette,
  typedWithModifiers,
  usableGrid,
  withModifiers,
  type ModifierKey,
  type Modifiers,
  type TerminalFrame,
  type TerminalPalette,
} from "../../../lib/terminal";
import { gridRow, parseAnsiRows, plainRow, type CellRow } from "../../../lib/terminal-cells";
import { toast } from "../../../ui/atoms";
import {
  copyNotice,
  copyWith,
  dragHasFiles,
  osc52Text,
  pastedPaths,
  pasteVerdict,
  refuseUpload,
  transferFiles,
  uploadName,
  uploadingText,
} from "../../../lib/terminal-clipboard";
import { cjkFontStack, terminalFontReady } from "./cjk";
import { browserClipboard, clipboardFiles, uploadFailure, uploadToMachine } from "./clipboard";

/** Driving: not asked (off), asked (starting), granted (active), another program holds it and the user is to say
 * whether to take it (busy), or watching although it wanted to drive (declined, `why`). */
export type Control = "off" | "starting" | "active" | "busy" | "declined";
export type Why = "" | "taken" | "lost" | "unsupported" | "chosen";

export interface FeedView {
  /** xterm is loaded and open. */
  ready: boolean;
  mode: "stream" | "poll";
  control: Control;
  why: Why;
  note: string;
  /** The user wants to drive it (接管) rather than watch (只看); remembered across terminals. */
  wants: boolean;
  /** xterm is the pane's size (watching): the panel scrolls over it instead of fitting it. */
  pinned: boolean;
  cols: number;
  rows: number;
  /** The first screen arrived (or the first read failed). */
  loaded: boolean;
  offline: boolean;
  /** What is shown may be old: the status bar is up and the screen dimmed. */
  stale: boolean;
  failure: string | null;
  /** A paste of several lines waiting for 粘贴. */
  pasting: string | null;
  /** Files being sent to the computer to be pasted by path ("正在上传 …"), null when none are. */
  uploading: string | null;
  /** Files are being dragged over the panel. */
  dropping: boolean;
  /** Text is selected in xterm (the key bar's copy). */
  selection: boolean;
  /** The key bar's ctrl / alt / shift: they apply to the next key, from the bar or typed. */
  mods: Modifiers;
  /** Pages of herdr's history above the screen, oldest first (watching only, iOS TerminalFeed.older). */
  older: CellRow[];
  /** Reading history: the screen stays as it was until 回到最新. */
  paused: boolean;
  loadingOlder: boolean;
  hasOlder: boolean;
  /** The page reached the start of what herdr keeps (about 1000 lines). */
  atLimit: boolean;
  /** 换行: long rows fold at the panel's width while watching (remembered). */
  wrap: boolean;
  fontSize: number;
  /** The terminal's font stack (with Chinese scaled to two cells once that font loads). */
  fontFamily: string;
  /** The screen as rows the page draws (换行 while watching); null while xterm draws it. */
  screen: ScreenRows | null;
  /** xterm's colors, for the rows the page draws (follows light / dark). */
  palette: TerminalPalette | null;
}

export interface ScreenRows {
  rows: CellRow[];
  /** Where the cursor is, when the program shows it. */
  cursor: { row: number; col: number } | null;
}

const TICK_MS = 250;
const RESIZE_MS = 250;
const POLL_MS = 1000;
const WHEEL_MS = 50;
const LINK_DOWN = "连不上 hub";
const WATCH_KEY = "jarvis.terminal.watch";
const WRAP_KEY = "jarvis.terminal.wrap";
const FONT_KEY = "jarvis.terminal.fontSize";
export const FONT_SIZES = { min: 9, max: 20, default: 13 };
/** Frames held while history is read; past this the screen starts over from a whole frame instead. */
const HELD_MAX = 4 << 20;

function savedFontSize(): number {
  const n = Number(localStorage.getItem(FONT_KEY));
  return Number.isFinite(n) && n >= FONT_SIZES.min && n <= FONT_SIZES.max ? n : FONT_SIZES.default;
}

export class TerminalFeed {
  private view: FeedView;
  private listeners = new Set<() => void>();
  private term: XTerm | null = null;
  private fit: XFit | null = null;
  private cleanup: (() => void)[] = [];
  private disposed = false;
  private order = new FrameOrder();
  private subscribed = false;
  private everSubscribed = false;
  /** This session cannot stream (old node / herdr): stay on polling. */
  private unsupported = false;
  private takeover = false;
  private failures = 0;
  private lastOK: number | null = null;
  private nextTry = 0;
  /** When the wait for the first screen started. */
  private since: number | null = Date.now();
  /** The grid that fits the panel: what driving asks for. */
  private grid: { cols: number; rows: number } | null = null;
  private gridTimer = 0;
  private polling: AbortController | null = null;
  private lastPoll: string | null = null;
  private wheel = new WheelLines();
  private wheelPending = 0;
  private wheelAt: [number, number] | null = null;
  private wheelTimer = 0;
  private down: { x: number; y: number } | null = null;
  private focusedOnce = false;
  /** The last screen read while polling (its rows, when the page draws them). */
  private pollText = "";
  /** The plain lines of `older` (what the next page is located by). */
  private olderPlain: string[] = [];
  /** Frames that arrived while history is read, applied on 回到最新. */
  private held: { frame: TerminalFrame; bytes: Uint8Array }[] = [];
  private heldBytes = 0;
  /** Too much arrived meanwhile: start over from a whole frame. */
  private heldOverflow = false;
  private screenFrame = 0;
  /** Names of the files on their way to the computer (pasted or dropped), and how deep the drag is over the panel. */
  private uploads: string[] = [];
  private dragDepth = 0;

  constructor(
    readonly mid: string,
    readonly sid: string,
  ) {
    this.view = {
      ready: false,
      mode: "poll",
      control: "off",
      why: "",
      note: "",
      wants: localStorage.getItem(WATCH_KEY) !== "1",
      pinned: false,
      cols: 0,
      rows: 0,
      loaded: false,
      offline: false,
      stale: false,
      failure: null,
      pasting: null,
      uploading: null,
      dropping: false,
      selection: false,
      mods: NO_MODIFIERS,
      older: [],
      paused: false,
      loadingOlder: false,
      hasOlder: true,
      atLimit: false,
      wrap: localStorage.getItem(WRAP_KEY) === "1",
      fontSize: savedFontSize(),
      fontFamily: "",
      screen: null,
      palette: null,
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

  /** The link's health right now, for the status bar's countdown. */
  link() {
    return { lastOK: this.lastOK, failure: this.view.failure, nextTry: this.nextTry };
  }

  private patch(p: Partial<FeedView>) {
    const v = this.view as unknown as Record<string, unknown>;
    if (!Object.entries(p).some(([k, x]) => v[k] !== x)) return;
    this.view = { ...this.view, ...p };
    this.listeners.forEach((l) => l());
  }

  // ── lifecycle ─────────────────────────────────────────

  async mount(host: HTMLElement, viewport: HTMLElement) {
    let mod: typeof import("./xterm");
    try {
      mod = await import("./xterm");
    } catch {
      if (!this.disposed) this.patch({ loaded: true, failure: "终端组件没有加载出来，刷新页面再试", stale: true });
      return;
    }
    if (this.disposed) return;
    const { Terminal, FitAddon, ClipboardAddon } = mod;
    const family = getComputedStyle(host).fontFamily;
    await terminalFontReady(this.view.fontSize, family); // xterm measures a cell when it opens: not before the font is there
    if (this.disposed) return;
    this.patch({ palette: this.theme() });
    const term = new Terminal({
      fontFamily: family,
      fontSize: this.view.fontSize,
      lineHeight: 1.15,
      scrollback: 0, // herdr redraws the grid in place: nothing ever scrolls into xterm's history
      cursorBlink: false,
      cursorStyle: "block",
      cursorInactiveStyle: "outline",
      disableStdin: true,
      macOptionIsMeta: true,
      theme: this.view.palette ?? undefined,
      linkHandler: {
        activate: (_e, uri) => {
          if (/^https?:\/\//i.test(uri)) openLink(uri); // OSC 8 links from the program: web pages only
        },
      },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    // OSC 52 from the program (herdr's streams do not carry it, docs/terminal.md): a write goes to the clipboard, a read
    // is refused — a program must not read what the user copied, so it is told the clipboard is empty.
    term.loadAddon(new ClipboardAddon(undefined, { readText: () => "", writeText: (_selection: string, text: string) => this.programCopied(text) }));
    term.open(host);
    this.term = term;
    this.fit = fit;

    const data = term.onData((d) => this.type(d));
    const resized = term.onResize(() => this.sizeElement());
    const parsed = term.onWriteParsed(() => this.screenChanged());
    const selected = term.onSelectionChange(() => this.patch({ selection: term.hasSelection() }));
    term.attachCustomKeyEventHandler(this.key);
    term.attachCustomWheelEventHandler(this.wheelEvent);
    host.addEventListener("paste", this.paste, true);
    viewport.addEventListener("dragenter", this.dragEnter, true);
    viewport.addEventListener("dragover", this.dragOver, true);
    viewport.addEventListener("dragleave", this.dragLeave, true);
    viewport.addEventListener("drop", this.drop, true);
    host.addEventListener("pointerdown", this.pointerDown, true);
    host.addEventListener("click", this.click, true);
    const colors = new MutationObserver(() => {
      const palette = this.theme();
      this.patch({ palette });
      if (this.term) this.term.options.theme = palette;
    });
    colors.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] });
    const sizes = new ResizeObserver(() => this.measure());
    sizes.observe(viewport);
    const offs = [onHubEvent("terminal", this.onTerminal), onHubEvent("terminal_control", this.onControl), onHubEvent("hello", this.onHello)];
    const tick = window.setInterval(this.tick, TICK_MS);
    this.cleanup.push(
      () => data.dispose(),
      () => resized.dispose(),
      () => parsed.dispose(),
      () => selected.dispose(),
      () => cancelAnimationFrame(this.screenFrame),
      () => host.removeEventListener("paste", this.paste, true),
      () => viewport.removeEventListener("dragenter", this.dragEnter, true),
      () => viewport.removeEventListener("dragover", this.dragOver, true),
      () => viewport.removeEventListener("dragleave", this.dragLeave, true),
      () => viewport.removeEventListener("drop", this.drop, true),
      () => host.removeEventListener("pointerdown", this.pointerDown, true),
      () => host.removeEventListener("click", this.click, true),
      () => colors.disconnect(),
      () => sizes.disconnect(),
      ...offs,
      () => clearInterval(tick),
    );

    this.measure(true); // the first grid sizes the empty terminal and is what driving asks for
    this.patch({ ready: true, fontFamily: family });
    this.tick();
    void cjkFontStack(family).then((stack) => {
      if (!stack || this.disposed || !this.term) return;
      this.term.options.fontFamily = stack; // the cell is the same width: nothing resizes
      this.patch({ fontFamily: stack });
    });
  }

  /** Leaves the terminal: the hub lets go of it (the pane goes back to the computer's size). */
  dispose() {
    if (this.disposed) return;
    this.disposed = true;
    if (this.everSubscribed && this.view.mode === "stream") send({ type: "terminal_unsubscribe", ...this.ids() });
    this.polling?.abort();
    clearTimeout(this.gridTimer);
    clearTimeout(this.wheelTimer);
    this.cleanup.forEach((f) => f());
    this.cleanup = [];
    this.term?.dispose();
    this.term = null;
    this.listeners.clear();
  }

  private ids() {
    return { machine_id: this.mid, session_id: this.sid };
  }

  private has(feature: string) {
    return getState().features.includes(feature);
  }

  /** The socket is up and the hub said hello. */
  private live() {
    return isOpen() && getState().conn === "online";
  }

  private theme(): TerminalPalette {
    const css = getComputedStyle(document.documentElement);
    return terminalPalette((n) => css.getPropertyValue(n), document.documentElement.classList.contains("dark"));
  }

  // ── the loop (four times a second, as iOS) ─────────────

  private tick = () => {
    if (!this.term || this.disposed) return;
    const now = Date.now();
    const v = this.view;
    if (v.mode === "poll" && !this.unsupported && this.has("terminal_stream") && this.live()) {
      if (v.wants && this.has("terminal_control") && v.control === "off" && !this.grid) return; // the first layout tells the size
      this.polling?.abort();
      this.polling = null;
      this.patch({ mode: "stream" });
      this.subscribeNow(false);
    } else if (v.mode === "stream") {
      if (!this.live()) {
        this.subscribed = false; // the hub's hello subscribes again
        if (!v.failure) this.patch({ failure: LINK_DOWN });
      } else {
        if (v.failure === LINK_DOWN) this.patch({ failure: null }); // back: an idle pane sends nothing, that is not a failure
        if (!this.subscribed && now >= this.nextTry) this.subscribeNow(true);
        else if (this.subscribed && this.view.loaded && !this.view.failure) this.lastOK = now;
      }
    } else if (now >= this.nextTry && !this.polling && !v.paused) {
      void this.poll();
    }
    this.patch({ stale: isStale(this.lastOK, this.view.failure, now, this.since) });
  };

  private willDrive() {
    const v = this.view;
    return v.wants && this.has("terminal_control") && usableGrid(this.grid) && v.control !== "declined" && v.control !== "busy";
  }

  private subscribeNow(continuing: boolean) {
    this.subscribed = this.everSubscribed = true;
    const msg: Record<string, unknown> = { type: "terminal_subscribe", ...this.ids() };
    if (continuing && this.order.epoch) {
      msg.epoch = this.order.epoch;
      msg.after_seq = this.order.seq;
    } else {
      this.order.restart(Date.now());
    }
    if (this.willDrive() && this.grid) {
      msg.control = { cols: this.grid.cols, rows: this.grid.rows };
      if (this.view.control === "off") this.patch({ control: "starting" });
    }
    if (this.takeover) msg.takeover = true;
    this.takeover = false;
    send(msg);
    this.inputMode();
  }

  private onHello = () => {
    if (this.view.mode !== "stream" || this.disposed) return;
    this.subscribed = false;
    if (this.live()) this.subscribeNow(true);
  };

  // ── frames ────────────────────────────────────────────

  private onTerminal = (msg: Record<string, unknown>) => {
    if (msg.machine_id !== this.mid || msg.session_id !== this.sid || !this.term) return;
    const p = parseTerminalPush(msg);
    if (!p) return;
    if (p.kind === "ended") return this.ended(p.error, p.unsupported);
    if (this.view.mode !== "stream") return;
    const bytes = decodeBase64(p.frame.data);
    if (!bytes) return; // the next diff will not follow: a gap
    const now = Date.now();
    switch (this.order.accept(p.frame, now)) {
      case "apply":
        if (this.view.paused) this.hold(p.frame, bytes);
        else this.draw(p.frame, bytes);
        this.healthy(now);
        this.patch({ offline: false, loaded: true });
        break;
      case "gap":
        this.subscribeNow(false);
        break;
    }
  };

  private draw(f: TerminalFrame, bytes: Uint8Array) {
    const t = this.term!;
    if (f.full) t.reset(); // a whole screen: nothing of the last one stays (nor a resize's leftovers)
    if (f.cols && f.rows) this.resize(f.cols, f.rows);
    t.write(bytes);
  }

  /** Reading history: the frame waits for 回到最新 (a whole one makes the ones before it moot). */
  private hold(frame: TerminalFrame, bytes: Uint8Array) {
    if (frame.full) {
      this.held = [];
      this.heldBytes = 0;
      this.heldOverflow = false;
    }
    if (this.heldOverflow) return;
    this.held.push({ frame, bytes });
    this.heldBytes += bytes.length;
    if (this.heldBytes > HELD_MAX) {
      this.held = [];
      this.heldBytes = 0;
      this.heldOverflow = true;
    }
  }

  private ended(error: string, unsupported: boolean) {
    this.subscribed = false;
    this.order.reset();
    const c = this.view.control;
    const control: Control = c === "active" || c === "starting" ? "off" : c; // it asks again when it subscribes again
    if (unsupported) {
      this.unsupported = true;
      this.nextTry = 0;
      this.patch({ mode: "poll", control });
    } else {
      const offline = error.includes("not running");
      this.patch({ offline, loaded: true, control });
      this.failed(offline ? "会话没有在 herdr 里运行" : error);
    }
    this.inputMode();
  }

  private onControl = (msg: Record<string, unknown>) => {
    if (msg.machine_id !== this.mid || msg.session_id !== this.sid || !this.term) return;
    const why = typeof msg.error === "string" ? msg.error : "";
    switch (msg.state) {
      case "active":
        this.leaveHistory();
        this.patch({ control: "active", why: "", note: "" });
        if (!this.focusedOnce) {
          this.focusedOnce = true;
          this.term.focus();
        }
        break;
      case "busy":
        this.term.blur(); // the question's buttons take the keys, not the terminal (Esc would reach the program)
        this.patch({ control: "busy" });
        break;
      case "unsupported":
        this.patch({ control: "declined", why: "unsupported", note: why || "这台电脑只能看，不能接管" });
        toast(this.view.note);
        break;
      case "taken":
        this.patch({ control: "declined", why: "taken", note: why || "另一台设备接管了这个终端" });
        break;
      default: // lost: the node let go on its own
        this.patch({ control: "declined", why: "lost", note: why || "终端已回到电脑上的尺寸" });
    }
    this.inputMode();
  };

  // ── sizes ─────────────────────────────────────────────

  /** The panel changed size (dragged, the window): the first grid at once, later ones once they settle. */
  private measure(now = false) {
    clearTimeout(this.gridTimer);
    const run = () => {
      const g = this.fit?.proposeDimensions();
      if (g && Number.isFinite(g.cols) && Number.isFinite(g.rows)) this.setGrid({ cols: g.cols, rows: g.rows });
    };
    if (now || !this.grid) run();
    else this.gridTimer = window.setTimeout(run, RESIZE_MS);
  }

  private setGrid(g: { cols: number; rows: number }) {
    if (!usableGrid(g) || (this.grid && this.grid.cols === g.cols && this.grid.rows === g.rows)) return;
    this.grid = g;
    const c = this.view.control;
    if (this.subscribed && (c === "active" || c === "starting")) send({ type: "terminal_input", ...this.ids(), control: g });
    if (this.view.mode === "poll" || !this.view.loaded) {
      this.resize(g.cols, g.rows);
      this.lastPoll = null; // read again at the new size
      if (this.view.mode === "poll") this.nextTry = 0;
    }
  }

  private resize(cols: number, rows: number) {
    const t = this.term;
    if (!t || cols < 1 || rows < 1) return;
    if (t.cols !== cols || t.rows !== rows) t.resize(cols, rows); // onResize sizes the element
    this.patch({ cols, rows });
  }

  /** Watching, xterm keeps the pane's size and the panel scrolls over it; otherwise it fills the panel. */
  private sizeElement() {
    const v = this.view;
    const pinned = v.mode === "stream" && v.control !== "active" && v.control !== "starting";
    const el = this.term?.element;
    const screen = el?.querySelector<HTMLElement>(".xterm-screen");
    if (el) {
      el.style.width = pinned && screen ? screen.style.width : "";
      el.style.height = pinned && screen ? screen.style.height : "";
    }
    this.patch({ pinned });
  }

  // ── input ─────────────────────────────────────────────

  private inputMode() {
    const t = this.term;
    if (!t) return;
    t.options.disableStdin = this.view.control !== "active";
    this.sizeElement();
    this.screenChanged();
  }

  private send(text: string) {
    if (text && this.view.control === "active") send({ type: "terminal_input", ...this.ids(), text });
  }

  /** What xterm makes of the keys typed on it: the terminal's bytes; with the key bar's ctrl / alt / shift down, that key. */
  private type(data: string) {
    const m = this.view.mods;
    if (!modifiersActive(m)) return this.send(data);
    const name = keyNameForBytes(data);
    if (name) {
      this.sendKey(withModifiers(m, name));
    } else if ([...data].length === 1) {
      const t = typedWithModifiers(m, data);
      if (t.key) this.sendKey(t.key);
      else if (t.text) this.send(t.text);
    } else {
      this.send(data);
    }
    this.patch({ mods: modifiersPressed(m) });
  }

  /** A key by herdr's name ("ctrl+c", "up"), as the program asked for its arrows. */
  private sendKey(name: string) {
    const b = encodeKey(name, this.term?.modes.applicationCursorKeysMode ?? false);
    if (b) this.send(b);
  }

  // ── the key bar ───────────────────────────────────────

  /** Driving it: keys go in. */
  get driving() {
    return this.view.control === "active";
  }

  /** A key of the bar, with the modifiers that are down (then the armed-once ones let go). */
  pressKey(name: string) {
    if (!this.driving) return;
    const m = this.view.mods;
    this.sendKey(withModifiers(m, name));
    this.patch({ mods: modifiersPressed(m) });
  }

  /** A key that goes out as it is, whatever is down (⌃C). */
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

  /** A saved command: typed into the terminal, and run (with return) when `run`. */
  typeText(text: string, run: boolean) {
    if (!this.driving || !text) return;
    this.send(pasteBytes(text) + (run ? "\r" : ""));
    this.term?.focus();
  }

  /** The key bar's paste: what the clipboard has, as ⌘V does (several lines ask first). */
  async pasteClipboard() {
    if (!this.driving) return;
    const files = await clipboardFiles();
    if (files.length) return void this.uploadFiles(files);
    let text = "";
    try {
      text = (await navigator.clipboard?.readText?.()) ?? "";
    } catch {
      return void toast("浏览器没有允许读取剪贴板，用 ⌘V / Ctrl+V 粘贴");
    }
    if (!navigator.clipboard?.readText) return void toast("这个页面不能读取剪贴板（需要 HTTPS），用 ⌘V / Ctrl+V 粘贴");
    if (!text) return void toast("剪贴板里没有文字");
    if (lineCount(text) > 1) {
      this.term?.blur();
      this.patch({ pasting: text });
    } else {
      this.send(pasteBytes(text));
    }
  }

  focus() {
    this.term?.focus();
  }

  /**
   * Keys the page takes before xterm (keyDecision): ⌃C / ⌘C with text selected copies it (nothing selected: ^C goes to the
   * program as ever), shift+enter is a new line in Claude Code's box (ESC CR, as iOS), an IME composition is left alone.
   */
  private key = (e: KeyboardEvent) => {
    const d = keyDecision(e, this.term?.hasSelection() ?? false);
    if (d === "xterm") return true;
    if (e.type === "keydown") {
      if (d === "copy") this.copySelection();
      else if (d === "newline") this.send("\x1b\r");
    }
    e.preventDefault();
    e.stopPropagation();
    return false;
  };

  /** Copies what is selected in xterm (the key bar's copy, ⌃C / ⌘C); the selection goes once it is on the clipboard. */
  copySelection() {
    const text = this.term?.getSelection() ?? "";
    if (!text) return void toast("先用鼠标选中要复制的文字");
    void copyWith(text, browserClipboard()).then((outcome) => {
      const n = copyNotice(outcome, window.isSecureContext);
      toast(n.text, n.tone);
      if (outcome !== "failed") this.term?.clearSelection();
    });
  }

  /** A program asked for the clipboard (OSC 52): written at once where the browser allows it, else the user taps 复制 (it wants a gesture). */
  private programCopied(text: string) {
    const t = osc52Text(text);
    if (!t) return;
    void copyWith(t, browserClipboard()).then((outcome) => {
      if (outcome !== "failed") return toast("终端里的程序已复制到剪贴板", "ok");
      toast("终端里的程序要复制一段文字", "info", {
        label: "复制",
        run: () =>
          void copyWith(t, browserClipboard()).then((o) => {
            const n = copyNotice(o, window.isSecureContext);
            toast(n.text, n.tone);
          }),
      });
    });
  }

  /** ⌘V: one line goes in as typed; several ask first, then go as one bracketed paste. */
  private paste = (e: ClipboardEvent) => {
    e.preventDefault();
    e.stopPropagation();
    const files = transferFiles(e.clipboardData);
    if (files.length) return void this.uploadFiles(files);
    if (this.view.control !== "active") return;
    const text = e.clipboardData?.getData("text/plain") ?? "";
    if (!text) return;
    if (lineCount(text) > 1) {
      this.term?.blur();
      this.patch({ pasting: text });
    } else {
      this.send(pasteBytes(text));
    }
  };

  // ── files: pasted or dropped, sent to the computer, their paths typed in (docs/terminal.md「粘贴图片 / 文件」) ──

  /**
   * Sends the files to the computer the pane is on, then types their escaped paths into it as one paste: Claude Code
   * and Codex take a path to an image as the image. Only while this browser drives the pane — before, and again when
   * the uploads end (taken over, 只看 or another terminal meanwhile: nothing is pasted).
   */
  async uploadFiles(files: File[]) {
    if (!this.driving) return void toast("接管终端后才能粘贴文件");
    const sending = files.filter((f) => {
      const why = refuseUpload(f);
      if (why) toast(why, "error");
      return !why;
    });
    if (!sending.length) return;
    const names = sending.map(uploadName);
    this.uploads.push(...names);
    this.patch({ uploading: uploadingText(this.uploads) });
    const results = await Promise.allSettled(sending.map((f) => uploadToMachine(this.mid, f)));
    for (const n of names) this.uploads.splice(this.uploads.indexOf(n), 1);
    this.patch({ uploading: this.uploads.length ? uploadingText(this.uploads) : null });
    const paths: string[] = [];
    results.forEach((r, i) => {
      if (r.status === "fulfilled") paths.push(r.value);
      else if (!this.disposed) toast(uploadFailure(names[i], r.reason), "error");
    });
    if (!paths.length) return;
    const verdict = pasteVerdict({ disposed: this.disposed, driving: this.driving });
    if (verdict === "changed") return void toast("上传期间终端已切换，没有粘贴", "error");
    if (verdict === "watching") return void toast("上传期间不再接管终端，没有粘贴", "error");
    this.term?.paste(pastedPaths(paths)); // bracketed when the program asked for it; the keys go through type()
    this.term?.focus();
  }

  private dragEnter = (e: DragEvent) => {
    if (!dragHasFiles(e.dataTransfer)) return;
    this.dragDepth++;
    if (this.driving) this.patch({ dropping: true });
  };

  private dragOver = (e: DragEvent) => {
    if (!dragHasFiles(e.dataTransfer)) return;
    e.preventDefault(); // without it the browser does not offer the drop (and opens the file instead)
    if (e.dataTransfer) e.dataTransfer.dropEffect = this.driving ? "copy" : "none";
  };

  private dragLeave = (e: DragEvent) => {
    if (!dragHasFiles(e.dataTransfer)) return;
    this.dragDepth = Math.max(0, this.dragDepth - 1);
    if (!this.dragDepth) this.patch({ dropping: false });
  };

  private drop = (e: DragEvent) => {
    const files = transferFiles(e.dataTransfer);
    this.dragDepth = 0;
    this.patch({ dropping: false });
    if (!files.length) return;
    e.preventDefault();
    e.stopPropagation();
    void this.uploadFiles(files);
  };

  confirmPaste(ok: boolean) {
    const text = this.view.pasting;
    this.patch({ pasting: null });
    if (ok && text) this.send(pasteBytes(text));
    this.term?.focus();
  }

  private box() {
    const t = this.term;
    const s = t?.element?.querySelector<HTMLElement>(".xterm-screen");
    if (!t || !s) return null;
    const r = s.getBoundingClientRect();
    return { left: r.left, top: r.top, width: r.width, height: r.height, cols: t.cols, rows: t.rows };
  }

  /**
   * Driving, the wheel is the terminal's (`terminal_scroll`): herdr gives it to a program that takes the mouse
   * (Claude Code, Codex scroll their conversation) or scrolls its own history — in lines, over the cell under the
   * pointer, one message per burst. Watching, the panel scrolls (xterm has no history; it would send arrow keys).
   */
  private wheelEvent = (e: WheelEvent) => {
    if (this.view.control !== "active" || !this.has("terminal_scroll")) return false;
    e.preventDefault();
    const b = this.box();
    if (!b) return false;
    const n = this.wheel.add(e.deltaY, e.deltaMode, b.height / b.rows, b.rows);
    this.wheelAt ??= cellAt(e.clientX - b.left, e.clientY - b.top, b.width, b.height, b.cols, b.rows);
    if (n) {
      this.wheelPending += n;
      if (!this.wheelTimer) this.wheelTimer = window.setTimeout(this.flushWheel, WHEEL_MS);
    }
    return false;
  };

  private flushWheel = () => {
    this.wheelTimer = 0;
    const n = this.wheelPending, at = this.wheelAt;
    this.wheelPending = 0;
    this.wheelAt = null;
    if (n && this.view.control === "active") send({ type: "terminal_input", ...this.ids(), scroll: n, ...(at ? { at } : {}) });
  };

  private pointerDown = (e: PointerEvent) => {
    this.down = e.button === 0 ? { x: e.clientX, y: e.clientY } : null;
  };

  /** Driving, a plain click is a click in the terminal at that cell (`terminal_click`); a drag selects instead. */
  private click = (e: MouseEvent) => {
    const d = this.down;
    this.down = null;
    if (!d || e.detail !== 1 || Math.hypot(e.clientX - d.x, e.clientY - d.y) > 4) return;
    if (this.view.control !== "active" || !this.has("terminal_click") || this.term?.hasSelection()) return;
    const b = this.box();
    if (!b) return;
    const x = e.clientX - b.left, y = e.clientY - b.top;
    if (x < 0 || y < 0 || x > b.width || y > b.height) return;
    send({ type: "terminal_input", ...this.ids(), click: cellAt(x, y, b.width, b.height, b.cols, b.rows) });
  };

  // ── what the user chose ───────────────────────────────

  /** 接管 / 只看 (remembered): subscribe again the other way. */
  setWants(on: boolean) {
    if (on === this.view.wants) return;
    if (on) localStorage.removeItem(WATCH_KEY);
    else localStorage.setItem(WATCH_KEY, "1");
    const control: Control = !on || this.view.control === "declined" ? "off" : this.view.control;
    if (on) this.leaveHistory();
    this.patch({ wants: on, control, why: "", note: "" });
    if (this.subscribed && this.live()) this.subscribeNow(false);
    this.inputMode();
  }

  /** Take it from the program or device that holds it (after busy, taken, lost). */
  takeOver() {
    this.leaveHistory();
    this.takeover = true;
    this.patch({ control: "starting", why: "", note: "" });
    if (this.view.mode === "stream" && this.live()) this.subscribeNow(true);
    this.inputMode();
  }

  /** Only watch what another program on the computer drives (the hub already mirrors it). */
  watchOnly() {
    this.patch({ control: "declined", why: "chosen", note: "只看：电脑上另一个程序在用这个终端" });
    this.inputMode();
    this.term?.focus();
  }

  /** 重新连接: a fresh subscription (a whole screen to start from), or a read now. */
  retry() {
    this.nextTry = 0;
    if (this.view.mode === "stream") {
      if (this.live()) this.subscribeNow(false);
    } else {
      this.polling?.abort();
      this.polling = null;
      this.lastPoll = null;
      void this.poll();
    }
  }

  // ── health ────────────────────────────────────────────

  private healthy(now: number) {
    this.lastOK = now;
    this.since = null;
    this.failures = 0;
    this.patch({ failure: null, stale: false });
  }

  private failed(why: string) {
    this.failures++;
    const now = Date.now();
    this.nextTry = now + backoff(this.failures) * 1000;
    this.patch({ failure: why, stale: true });
  }

  // ── history (iOS TerminalFeed.loadOlder) ───────────────

  /** The plain lines on screen now: xterm's grid, or the last read while polling. */
  private screenPlain(): string[] {
    const t = this.term;
    if (this.view.mode === "poll") return parseAnsiRows(this.pollText).map(plainRow);
    if (!t) return [];
    const b = t.buffer.active;
    const out: string[] = [];
    for (let y = 0; y < t.rows; y++) out.push(b.getLine(b.viewportY + y)?.translateToString(true) ?? "");
    return out;
  }

  /**
   * The page of herdr's history before the first lines shown (watching only: driving, the program scrolls its own).
   * The screen holds still meanwhile; with nothing above (a full-screen program) it keeps following.
   */
  async loadOlder() {
    const v = this.view;
    if (v.loadingOlder || !v.hasOlder || !v.loaded || !this.term || v.control === "active" || v.control === "starting") return;
    const wasPaused = v.paused;
    this.patch({ loadingOlder: true, paused: true });
    const { anchor, loaded } = historyAnchor([...this.olderPlain, ...this.screenPlain()]);
    try {
      const r = await api<{ text?: string; has_more?: boolean; at_limit?: boolean }>(`${sessionPath(this.mid, this.sid)}/screen/before`, {
        method: "POST",
        json: { anchor, lines: HISTORY_PAGE, loaded, format: "ansi" },
      });
      if (this.disposed || !this.view.paused) return;
      const rows = r.text ? parseAnsiRows(r.text) : [];
      if (rows.length) this.olderPlain = [...rows.map(plainRow), ...this.olderPlain];
      this.patch({ older: rows.length ? [...rows, ...this.view.older] : this.view.older, hasOlder: Boolean(r.has_more), atLimit: Boolean(r.at_limit) });
    } catch (e) {
      if (!this.disposed && !isAbort(e)) toast(`没有读到更早的输出：${errorText(e)}`, "error");
    } finally {
      if (!this.disposed) {
        this.patch({ loadingOlder: false });
        if (!this.view.older.length && !wasPaused) this.resume();
      }
    }
  }

  /** 回到最新: history goes, the screen follows the pane again (what arrived meanwhile is drawn now). */
  resume() {
    this.olderPlain = [];
    const wasPaused = this.view.paused;
    this.patch({ older: [], hasOlder: true, atLimit: false, paused: false, loadingOlder: false });
    if (!wasPaused) return;
    if (this.view.mode === "stream") {
      const held = this.held, overflow = this.heldOverflow;
      this.held = [];
      this.heldBytes = 0;
      this.heldOverflow = false;
      if (overflow) {
        this.order.reset(); // what was skipped is not on screen: continue from a whole frame
        if (this.live()) this.subscribeNow(false);
      } else {
        for (const h of held) this.draw(h.frame, h.bytes);
      }
    } else {
      this.nextTry = 0;
    }
    this.screenChanged();
  }

  /** Driving now: what was read of the pane's history at the computer's size goes. */
  private leaveHistory() {
    if (this.view.paused || this.view.older.length) this.resume();
  }

  // ── 换行 and the font size ──────────────────────────────

  setWrap(on: boolean) {
    if (on) localStorage.setItem(WRAP_KEY, "1");
    else localStorage.removeItem(WRAP_KEY);
    this.patch({ wrap: on });
    this.screenChanged();
  }

  setFontSize(size: number) {
    const n = Math.max(FONT_SIZES.min, Math.min(FONT_SIZES.max, Math.round(size)));
    if (n === this.view.fontSize) return;
    if (n === FONT_SIZES.default) localStorage.removeItem(FONT_KEY);
    else localStorage.setItem(FONT_KEY, String(n));
    this.patch({ fontSize: n });
    if (this.term) {
      this.term.options.fontSize = n;
      this.measure(true); // a new grid: driving, the pane takes it
      this.sizeElement();
      requestAnimationFrame(() => this.sizeElement()); // xterm sizes its screen for the new cells as it renders
    }
  }

  // ── the screen the page draws (换行 while watching) ─────

  /** The page draws the screen (rather than xterm): 换行 is on and the user only watches. */
  private drawn() {
    const c = this.view.control;
    return this.view.wrap && c !== "active" && c !== "starting";
  }

  /** Something was written (or what draws the screen changed): read the rows again, once per frame. */
  private screenChanged() {
    if (!this.drawn()) {
      if (this.view.screen) this.patch({ screen: null });
      return;
    }
    if (this.screenFrame) return;
    this.screenFrame = requestAnimationFrame(() => {
      this.screenFrame = 0;
      if (this.disposed || !this.drawn()) return;
      this.patch({ screen: this.readScreen() });
    });
  }

  private readScreen(): ScreenRows {
    if (this.view.mode === "poll") {
      const rows = parseAnsiRows(this.pollText);
      while (rows.length && !plainRow(rows[rows.length - 1])) rows.pop();
      return { rows, cursor: null };
    }
    const t = this.term!;
    const b = t.buffer.active;
    const rows: CellRow[] = [];
    for (let y = 0; y < t.rows; y++) {
      const line = b.getLine(b.viewportY + y);
      rows.push(line ? gridRow(t.cols, (x) => line.getCell(x)) : { runs: [] });
    }
    // the program hid it (Claude Code draws its own): xterm keeps that in its core, not its API
    const hidden = (t as unknown as { _core?: { coreService?: { isCursorHidden?: boolean } } })._core?.coreService?.isCursorHidden;
    const cursor = hidden === false ? { row: b.cursorY, col: b.cursorX } : null;
    // a shell after `clear` has its prompt at the top and empty rows under it: the screen ends at the cursor or the last text
    let end = rows.length;
    while (end > 0 && end - 1 > (cursor?.row ?? -1) && !plainRow(rows[end - 1]) && !rows[end - 1].runs.some((r) => r.style.bg !== undefined)) end--;
    return { rows: rows.slice(0, end), cursor };
  }

  // ── polling (no stream) ───────────────────────────────

  private async poll() {
    const ctrl = new AbortController();
    this.polling = ctrl;
    const rows = this.grid?.rows ?? this.term?.rows ?? 40;
    try {
      const r = await api<{ text?: string; status?: string }>(`${sessionPath(this.mid, this.sid)}/screen?format=ansi&lines=${rows}`, { signal: ctrl.signal });
      if (this.disposed || ctrl.signal.aborted || this.view.mode !== "poll" || !this.term) return;
      if (this.grid) this.resize(this.grid.cols, this.grid.rows);
      const text = r.text ?? "";
      if (text !== this.lastPoll) {
        this.lastPoll = text;
        this.pollText = text;
        this.term.reset();
        // no cursor in a read; rows end in \r\n (plain text from an old node in \n)
        this.term.write("\x1b[?25l" + text.replace(/\r?\n/g, "\r\n").replace(/(\r\n)+$/, ""));
        this.screenChanged();
      }
      const now = Date.now();
      this.healthy(now);
      this.nextTry = now + POLL_MS;
      this.patch({ offline: r.status === "offline", loaded: true });
    } catch (e) {
      if (isAbort(e) || this.disposed) return;
      this.failed(errorText(e));
      this.patch({ loaded: true });
    } finally {
      if (this.polling === ctrl) this.polling = null;
    }
  }
}
