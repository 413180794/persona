// @vitest-environment jsdom
// The hub's `notice` line (#4305) is drawn in the conversation as one small centered line; a kind this client does not
// know is still drawn as nothing.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { FeedItem } from "../../lib/feed";
import { FeedLineView } from "./Tasks";

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
});

const item = (kind: string, body?: Record<string, unknown>): FeedItem =>
  ({ id: "f1", conversation: "c1", at: "2026-11-06T08:00:00Z", kind, body }) as FeedItem;

describe("FeedLineView notice", () => {
  it("draws the hub's text as one centered line", () => {
    act(() => root.render(<FeedLineView item={item("notice", { text: "电脑和模型接好了，定时任务恢复运行，这次是「早报」" })} />));
    const line = host.querySelector('[data-testid="feed-notice"]');
    expect(line?.textContent).toBe("电脑和模型接好了，定时任务恢复运行，这次是「早报」");
    expect(line?.className).toContain("text-center");
  });
  it("draws nothing without words", () => {
    act(() => root.render(<FeedLineView item={item("notice", { text: " " })} />));
    expect(host.innerHTML).toBe("");
  });
  it("still skips a kind it does not know", () => {
    act(() => root.render(<FeedLineView item={item("something_newer", { text: "not for this client" })} />));
    expect(host.innerHTML).toBe("");
  });
});
