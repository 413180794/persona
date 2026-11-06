// @vitest-environment jsdom
// The side panel's terminal while the hub link drops: the hub forgets who drives, so the page must not go on saying it
// does (keys would be dropped) until the hub grants it again.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const hub = vi.hoisted(() => ({
  open: true,
  conn: "online",
  sent: [] as Record<string, unknown>[],
  listeners: new Map<string, Set<(msg: Record<string, unknown>) => void>>(),
}));

vi.mock("../../../lib/store", () => ({
  getState: () => ({ features: ["terminal_stream", "terminal_control"], conn: hub.conn }),
  isOpen: () => hub.open,
  send: (msg: Record<string, unknown>) => {
    if (hub.open) hub.sent.push(msg); // the real store drops what it cannot send
  },
  onHubEvent: (type: string, l: (msg: Record<string, unknown>) => void) => {
    const set = hub.listeners.get(type) ?? new Set();
    set.add(l);
    hub.listeners.set(type, set);
    return () => set.delete(l);
  },
}));

const typed = vi.hoisted(() => ({ onData: null as ((d: string) => void) | null }));

vi.mock("./xterm", () => {
  const disposable = { dispose() {} };
  class Terminal {
    options: Record<string, unknown>;
    cols = 80;
    rows = 24;
    element: HTMLElement | undefined;
    modes = { applicationCursorKeysMode: false };
    constructor(options: Record<string, unknown>) {
      this.options = { ...options };
    }
    loadAddon() {}
    open(host: HTMLElement) {
      this.element = host;
    }
    onData(cb: (d: string) => void) {
      typed.onData = cb;
      return disposable;
    }
    onResize() {
      return disposable;
    }
    onWriteParsed() {
      return disposable;
    }
    onSelectionChange() {
      return disposable;
    }
    attachCustomKeyEventHandler() {}
    attachCustomWheelEventHandler() {}
    resize(cols: number, rows: number) {
      this.cols = cols;
      this.rows = rows;
    }
    reset() {}
    write() {}
    focus() {}
    blur() {}
    hasSelection() {
      return false;
    }
    clearSelection() {}
    dispose() {}
  }
  class FitAddon {
    proposeDimensions() {
      return { cols: 100, rows: 30 };
    }
  }
  class SearchAddon {
    onDidChangeResults() {
      return disposable;
    }
  }
  return { Terminal, FitAddon, SearchAddon, ClipboardAddon: class {}, WebLinksAddon: class {}, matchHerdrWidths: () => {} };
});

vi.mock("./cjk", () => ({ cjkFontStack: () => Promise.resolve(null), terminalFontReady: () => Promise.resolve() }));

import { TerminalFeed } from "./feed";

const TICK_MS = 250;

function emit(type: string, msg: Record<string, unknown>) {
  hub.listeners.get(type)?.forEach((l) => l(msg));
}

function inputs() {
  return hub.sent.filter((m) => m.type === "terminal_input" && "text" in m);
}

describe("the side panel's terminal when the hub link drops", () => {
  let feed: TerminalFeed;

  beforeEach(() => {
    vi.useFakeTimers();
    hub.open = true;
    hub.conn = "online";
    hub.sent = [];
    hub.listeners.clear();
    typed.onData = null;
    localStorage.clear();
    globalThis.ResizeObserver ??= class {
      observe() {}
      unobserve() {}
      disconnect() {}
    } as unknown as typeof ResizeObserver;
  });

  afterEach(() => {
    feed?.dispose();
    vi.useRealTimers();
  });

  it("stops driving until the hub grants control again", async () => {
    feed = new TerminalFeed("m1", "s1");
    const host = document.createElement("div");
    await feed.mount(host, document.createElement("div"));
    expect(feed.snapshot().mode).toBe("stream"); // mount ticks once: it subscribes asking to drive
    expect(hub.sent.some((m) => m.type === "terminal_subscribe" && m.control)).toBe(true);

    emit("terminal_control", { machine_id: "m1", session_id: "s1", state: "active" });
    expect(feed.snapshot().control).toBe("active");
    expect(feed.driving).toBe(true);

    hub.open = false;
    hub.conn = "offline";
    vi.advanceTimersByTime(TICK_MS);
    expect(feed.snapshot().failure).not.toBeNull();
    expect(feed.snapshot().control).not.toBe("active");
    expect(feed.driving).toBe(false);

    hub.sent = [];
    hub.open = true; // the socket is back before the hub's hello: still nothing goes in
    typed.onData!("ls\r");
    feed.pressBare("ctrl+c");
    expect(inputs()).toEqual([]);

    hub.conn = "online";
    emit("hello", {});
    const sub = hub.sent.find((m) => m.type === "terminal_subscribe");
    expect(sub?.control).toEqual({ cols: 100, rows: 30 }); // it still asks to drive
    expect(feed.driving).toBe(false);

    emit("terminal_control", { machine_id: "m1", session_id: "s1", state: "active" });
    expect(feed.snapshot().control).toBe("active");
    expect(feed.driving).toBe(true);
    typed.onData!("ls\r");
    expect(inputs()).toEqual([{ type: "terminal_input", machine_id: "m1", session_id: "s1", text: "ls\r" }]);
  });
  it("stops driving when the hub says hello again before a tick sees the link drop", async () => {
    feed = new TerminalFeed("m1", "s1");
    await feed.mount(document.createElement("div"), document.createElement("div"));
    emit("terminal_control", { machine_id: "m1", session_id: "s1", state: "active" });
    expect(feed.driving).toBe(true);

    // The socket dropped and came back within one tick: no tick saw it down, only the hub's hello tells.
    hub.sent = [];
    emit("hello", {});
    expect(feed.snapshot().control).toBe("starting");
    expect(feed.driving).toBe(false);
    const sub = hub.sent.find((m) => m.type === "terminal_subscribe");
    expect(sub?.control).toEqual({ cols: 100, rows: 30 }); // it still asks to drive
    typed.onData!("ls\r");
    feed.pressBare("ctrl+c");
    expect(inputs()).toEqual([]);

    emit("terminal_control", { machine_id: "m1", session_id: "s1", state: "active" });
    expect(feed.driving).toBe(true);
    typed.onData!("ls\r");
    expect(inputs()).toEqual([{ type: "terminal_input", machine_id: "m1", session_id: "s1", text: "ls\r" }]);
  });
});
