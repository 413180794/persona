// @vitest-environment jsdom
// A finished answer is not drawn again when its segment re-renders with the same props (#3024): the chat context
// changes with every streamed item, so every SegmentView re-renders, and each finished Answer used to follow.
import { act, useState, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Item } from "../../lib/types";

// count how often Answer draws its menu, the first thing it renders
const drawn = vi.fn();
vi.mock("./MessageActions", () => ({
  MessageActions: ({ children }: { children: ReactNode }) => {
    drawn();
    return <>{children}</>;
  },
}));

const { Answer } = await import("./SegmentView");

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  drawn.mockClear();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const item = { id: "a1", kind: "assistant", text: "写完了", ts: "2026-11-05T10:00:00Z" } as unknown as Item;

describe("Answer", () => {
  it("is skipped when its parent re-renders with the same props", () => {
    let bump = () => {};
    function Parent() {
      const [n, setN] = useState(0);
      bump = () => setN((x) => x + 1);
      return (
        <div data-n={n}>
          <Answer item={item} mid="m" sid="s" />
        </div>
      );
    }
    act(() => root.render(<Parent />));
    expect(drawn).toHaveBeenCalledTimes(1);
    expect(host.textContent).toContain("写完了");
    for (let i = 0; i < 5; i++) act(() => bump());
    expect(drawn).toHaveBeenCalledTimes(1);
  });

  it("still redraws when its own props change", () => {
    act(() => root.render(<Answer streaming="写到" mid="m" sid="s" />));
    act(() => root.render(<Answer streaming="写到一半" mid="m" sid="s" />));
    // a streaming answer's Markdown draws its text on its own schedule: the count is what matters here
    expect(drawn).toHaveBeenCalledTimes(2);
    act(() => root.render(<Answer item={{ ...item, text: "改过了" }} mid="m" sid="s" />));
    expect(drawn).toHaveBeenCalledTimes(3);
    expect(host.textContent).toContain("改过了");
  });
});
