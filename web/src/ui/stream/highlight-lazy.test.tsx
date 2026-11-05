// @vitest-environment jsdom
// Code highlighting stays off the first draw: a reply with code draws its blocks plain, the highlighter is asked for
// once the page is idle, and the same blocks take its colors up when it arrives (lazy-plugin.ts, highlight.ts).
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Markdown } from "../Markdown";
import { loadHighlight } from "./highlight";
import { lazyPlugin } from "./lazy-plugin";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe("a lazy plugin", () => {
  it("is asked for when the page is idle, not while the reply first draws", async () => {
    const idle: (() => void)[] = [];
    vi.stubGlobal("requestIdleCallback", (run: () => void) => idle.push(run));
    vi.stubGlobal("cancelIdleCallback", () => {});
    const importer = vi.fn(async () => "plugin");
    const lp = lazyPlugin(importer);
    function Probe({ wanted }: { wanted: boolean }) {
      return <span>{lp.use(wanted) ?? "plain"}</span>;
    }
    act(() => root.render(<Probe wanted />));
    expect(host.textContent).toBe("plain");
    expect(importer).not.toHaveBeenCalled();
    expect(idle).toHaveLength(1);
    await act(async () => {
      idle[0]();
      await lp.load();
    });
    expect(importer).toHaveBeenCalledTimes(1);
    expect(host.textContent).toBe("plugin");
  });

  it("is not asked for by a reply that has no use for it", () => {
    const importer = vi.fn(async () => "plugin");
    const lp = lazyPlugin(importer);
    function Probe() {
      return <span>{lp.use(false) ?? "plain"}</span>;
    }
    act(() => root.render(<Probe />));
    expect(host.textContent).toBe("plain");
    expect(importer).not.toHaveBeenCalled();
  });
});

describe("Markdown's code before and after the highlighter", () => {
  it("draws the block plain first, then colors the same block", async () => {
    act(() => root.render(<Markdown text={"```ts\nconst a = 1\n```"} />));
    const pre = host.querySelector("pre")!;
    expect(pre.textContent).toBe("const a = 1\n");
    expect(pre.querySelector("[class*=hljs-]")).toBeNull();
    // the idle moment (a timer where there is no requestIdleCallback) asks for it; the block takes it up when it is in
    await act(async () => {
      await new Promise((r) => setTimeout(r, 100));
      await loadHighlight();
    });
    expect(host.querySelector(".hljs-keyword")!.textContent).toBe("const");
    // the same block: nothing is mounted again, so nothing moves
    expect(host.querySelector("pre")).toBe(pre);
    expect(pre.textContent).toBe("const a = 1\n");
  });
});
