// @vitest-environment jsdom
// The key bar's ctrl / alt / shift do not outlive the live view: a disabled key cap gets no pointerup, so a modifier
// pressed (or tapped once, or locked) when the view ends would otherwise turn the first key typed in the next one into
// a chord — a plain "c" sent as Ctrl+C.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NO_MODIFIERS } from "../../../lib/terminal";
import { HINT_KEY } from "../../../lib/terminal-mode";

const hub = vi.hoisted(() => ({
  sent: [] as Record<string, unknown>[],
  listeners: new Map<string, Set<(m: Record<string, unknown>) => void>>(),
  typed: null as ((d: string) => void) | null,
}));

vi.mock("../../../lib/store", async (importOriginal) => {
  const real = await importOriginal<typeof import("../../../lib/store")>();
  return {
    ...real,
    getState: () => ({ ...real.getState(), conn: "online", features: ["herdr_client"] }),
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
    parser = { registerCsiHandler: disposable };
    constructor(o: Record<string, unknown>) {
      this.options = { ...o };
    }
    onData(cb: (d: string) => void) {
      hub.typed = cb;
      return disposable();
    }
    onSelectionChange = disposable;
    loadAddon() {}
    open() {}
    attachCustomKeyEventHandler() {}
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

import { FullFeed } from "./full";

/** The id of the view the feed opened last. */
function openId() {
  const open = hub.sent.filter((m) => m.type === "herdr_open").at(-1);
  expect(open).toBeDefined();
  return open!.id as string;
}

function emit(type: string, msg: Record<string, unknown>) {
  hub.listeners.get(type)?.forEach((l) => l({ type, ...msg }));
}

/** The hub says the view that was opened last is up. */
function goLive(feed: FullFeed) {
  emit("herdr_state", { id: openId(), cols: 80, rows: 24 });
  expect(feed.snapshot().control).toBe("active");
}

/** What went in to the client, in order. */
function inputs() {
  return hub.sent.filter((m) => m.type === "herdr_input").map((m) => m.data);
}

let feed: FullFeed;

beforeEach(async () => {
  hub.sent = [];
  hub.listeners.clear();
  hub.typed = null;
  localStorage.clear();
  localStorage.setItem(HINT_KEY, "1");
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
  feed = new FullFeed("m1", "s1");
  await feed.mount(document.createElement("div"));
  feed.reconnect(); // a new feed waits to be claimed; this opens it
  goLive(feed);
});

afterEach(() => {
  feed.dispose();
  vi.unstubAllGlobals();
});

describe("FullFeed key-bar modifiers", () => {
  it("apply to the next key while live", () => {
    feed.tapModifier("ctrl", false);
    hub.typed!("c");
    expect(inputs()).toEqual(["\x03"]);
    expect(feed.snapshot().mods).toEqual(NO_MODIFIERS); // a tapped one is spent by the key
  });

  const ends: [string, () => void][] = [
    ["the view ended", () => emit("herdr_end", { id: openId(), error: "client exited" })],
    ["released", () => feed.release()],
    ["reconnected", () => feed.reconnect()],
    ["the hub said hello again", () => emit("hello", {})],
  ];

  for (const [name, end] of ends) {
    for (const [how, press] of [
      ["tapped once", () => feed.tapModifier("ctrl", false)],
      ["locked", () => (feed.tapModifier("ctrl", false), feed.tapModifier("ctrl", true))],
      ["held", () => feed.holdModifier("ctrl")],
    ] as [string, () => void][]) {
      it(`a ctrl ${how} is let go when the keys stop going in (${name})`, () => {
        press();
        expect(feed.snapshot().mods.ctrl).not.toBe("off");
        end();
        expect(feed.snapshot().control).toBe("off");
        expect(feed.snapshot().mods).toEqual(NO_MODIFIERS);
        if (name !== "reconnected" && name !== "the hub said hello again") feed.reconnect();
        goLive(feed);
        hub.typed!("c");
        expect(inputs()).toEqual(["c"]);
      });
    }
  }
});
