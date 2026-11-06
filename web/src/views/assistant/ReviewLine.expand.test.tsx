// @vitest-environment jsdom
// A review's note cut at the line's end opens in full on a click (#4861): what the owner is to decide is read where it is.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FeedItem } from "../../lib/feed";
import { ReviewLine } from "./Supervision";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let wide = 0;
const saved = { scroll: Object.getOwnPropertyDescriptor(HTMLElement.prototype, "scrollHeight"), sw: Object.getOwnPropertyDescriptor(HTMLElement.prototype, "scrollWidth"), cw: Object.getOwnPropertyDescriptor(HTMLElement.prototype, "clientWidth") };

beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, "scrollWidth", { configurable: true, get: () => wide });
  Object.defineProperty(HTMLElement.prototype, "clientWidth", { configurable: true, get: () => 100 });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
  for (const [k, d] of [["scrollWidth", saved.sw], ["clientWidth", saved.cw]] as const) if (d) Object.defineProperty(HTMLElement.prototype, k, d);
});

const note = "两个方案都能上线：A 今晚合、明早发 TestFlight；B 等设计稿再改一轮。你定用哪个。";
const item = (body: Record<string, unknown>): FeedItem => ({ id: "r1", conversation: "c1", at: new Date().toISOString(), kind: "review" as FeedItem["kind"], ref: "todo_1", body });

describe("ReviewLine (#4861)", () => {
  it("opens a cut note in full, and the title still opens the task", () => {
    wide = 400;
    const onOpenTask = vi.fn();
    act(() => root.render(<ReviewLine item={item({ verdict: "needs_user", note })} title="发版" onOpenTask={onOpenTask} />));
    const toggle = host.querySelector<HTMLButtonElement>("button[aria-expanded]")!;
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(toggle.textContent).toContain("等你决定");
    expect(host.querySelector("[data-testid=review-note]")).toBeNull();
    act(() => toggle.click());
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    expect(host.querySelector("[data-testid=review-note]")?.textContent).toBe(note);
    expect(onOpenTask).not.toHaveBeenCalled();
    act(() => toggle.click());
    expect(host.querySelector("[data-testid=review-note]")).toBeNull();
    act(() => host.querySelector<HTMLButtonElement>("button:not([aria-expanded])")!.click());
    expect(onOpenTask).toHaveBeenCalledOnce();
  });

  it("has nothing to open when the note fits or there is none", () => {
    wide = 80;
    act(() => root.render(<ReviewLine item={item({ verdict: "rework", round: 1, note: "补截图" })} title="发版" />));
    expect(host.querySelector("button[aria-expanded]")).toBeNull();
    expect(host.textContent).toContain("补截图");
    act(() => root.render(<ReviewLine item={item({ verdict: "accepted" })} title="发版" />));
    expect(host.querySelector("button[aria-expanded]")).toBeNull();
  });
});
