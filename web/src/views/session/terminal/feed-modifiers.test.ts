// @vitest-environment jsdom
// The key bar's ctrl / alt / shift do not outlive driving the pane: a disabled key cap gets no pointerup, so a modifier
// pressed (or tapped once, or locked) when control goes would otherwise turn the first key typed after it comes back
// into a chord — a plain "c" sent as Ctrl+C.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NO_MODIFIERS } from "../../../lib/terminal";

const hub = vi.hoisted(() => ({
  sent: [] as Record<string, unknown>[],
  listeners: new Map<string, Set<(m: Record<string, unknown>) => void>>(),
  typed: null as ((d: string) => void) | null,
}));

vi.mock("../../../lib/store", async (importOriginal) => {
  const real = await importOriginal<typeof import("../../../lib/store")>();
  return {
    ...real,
    getState: () => ({ ...real.getState(), conn: "online", features: ["terminal_stream", "terminal_control"] }),
    isOpen: () => true,
    send: (m: Record<string, unknown>) => void hub.sent.push(m),
    onHubEvent: (type: string, l: (m: Record<string, unknown>) => void) => {
      if (!hub.listeners.has(type)) hub.listeners.set(type, new Set());
      hub.listeners.get(type)!.add(l);
      return () => hub.listeners.get(type)?.delete(l);
    },
  };
});

vi.mock("./cjk", () => ({ cjkFontStack: async () => null, terminalFontReady: async () => {} }));

vi.mock("./xterm", () => {
  const disposable = () => ({ dispose() {} });
  class Terminal {
    options: Record<string, unknown>;
    cols = 80;
    rows = 24;
    element = document.createElement("div");
    modes = { applicationCursorKeysMode: false };
    constructor(o: Record<string, unknown>) {
      this.options = { ...o };
    }
    onData(cb: (d: string) => void) {
      hub.typed = cb;
      return disposable();
    }
    onResize = disposable;
    onWriteParsed = disposable;
    onSelectionChange = disposable;
    loadAddon() {}
    open() {}
    attachCustomKeyEventHandler() {}
    attachCustomWheelEventHandler() {}
    focus() {}
    blur() {}
    hasSelection() {
      return false;
    }
    resize() {}
    write() {}
    reset() {}
    dispose() {}
  }
  class FitAddon {
    proposeDimensions() {
      return { cols: 80, rows: 24 };
    }
  }
  class SearchAddon {
    onDidChangeResults = disposable;
  }
  class Addon {}
  return { Terminal, FitAddon, SearchAddon, ClipboardAddon: Addon, WebLinksAddon: Addon, matchHerdrWidths() {} };
});

import { TerminalFeed } from "./feed";

const MID = "m1";
const SID = "s1";

function emit(type: string, msg: Record<string, unknown>) {
  hub.listeners.get(type)?.forEach((l) => l({ type, machine_id: MID, session_id: SID, ...msg }));
}

/** What went in as typed text, in order. */
function inputs() {
  return hub.sent.filter((m) => m.type === "terminal_input" && typeof m.text === "string").map((m) => m.text);
}

let feed: TerminalFeed;

beforeEach(async () => {
  hub.sent = [];
  hub.listeners.clear();
  hub.typed = null;
  localStorage.clear();
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
  feed = new TerminalFeed(MID, SID);
  const host = document.createElement("div");
  await feed.mount(host, document.createElement("div"));
  emit("terminal_control", { state: "active" });
  expect(feed.snapshot().control).toBe("active");
});

afterEach(() => {
  feed.dispose();
  vi.unstubAllGlobals();
});

describe("TerminalFeed key-bar modifiers", () => {
  it("apply to the next key while driving", () => {
    feed.tapModifier("ctrl", false);
    hub.typed!("c");
    expect(inputs()).toEqual(["\x03"]);
    expect(feed.snapshot().mods).toEqual(NO_MODIFIERS); // a tapped one is spent by the key
  });

  const losses: [string, () => void][] = [
    ["taken", () => emit("terminal_control", { state: "taken" })],
    ["lost", () => emit("terminal_control", { state: "lost" })],
    ["busy", () => emit("terminal_control", { state: "busy" })],
    ["the stream ended", () => emit("terminal", { error: "pane closed" })],
    ["watch-only chosen", () => feed.watchOnly()],
    ["watching chosen", () => feed.setWants(false)],
  ];

  for (const [name, lose] of losses) {
    for (const [how, press] of [
      ["tapped once", () => feed.tapModifier("ctrl", false)],
      ["locked", () => (feed.tapModifier("ctrl", false), feed.tapModifier("ctrl", true))],
      ["held", () => feed.holdModifier("ctrl")],
    ] as [string, () => void][]) {
      it(`a ctrl ${how} is let go when control is lost (${name})`, () => {
        press();
        expect(feed.snapshot().mods.ctrl).not.toBe("off");
        lose();
        expect(feed.snapshot().control).not.toBe("active");
        expect(feed.snapshot().mods).toEqual(NO_MODIFIERS);
        if (name === "watching chosen") feed.setWants(true);
        emit("terminal_control", { state: "active" });
        expect(feed.snapshot().control).toBe("active");
        hub.typed!("c");
        expect(inputs()).toEqual(["c"]);
      });
    }
  }
});
