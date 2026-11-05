// @vitest-environment jsdom
// The toasts at the bottom: an error shows the design system's warning icon (not a bare red dot), every toast has a
// close button that drops it, and the dedupe and timings are as before.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { toast, Toasts } from "./atoms";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const WARNING_PATH = "M10.29 3.86"; // the "warning" icon's triangle (ui/icons.tsx)
let host: HTMLDivElement;
let root: Root;

const rows = () => [...document.body.querySelectorAll<HTMLElement>("[aria-live] > div")];
const show = (...args: Parameters<typeof toast>) => act(() => void toast(...args));
const advance = (ms: number) => act(() => void vi.advanceTimersByTime(ms));

beforeEach(() => {
  vi.useFakeTimers();
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
  act(() => root.render(<Toasts />));
});

afterEach(() => {
  advance(10_000); // whatever a test left showing times out
  act(() => root.unmount());
  host.remove();
  vi.useRealTimers();
});

describe("toasts", () => {
  it("an error shows the warning icon in red and no bare dot", () => {
    show("没发出去", "error");
    const [row] = rows();
    expect(row.innerHTML).toContain(WARNING_PATH);
    expect(row.querySelector("svg")?.getAttribute("class")).toContain("text-red");
    expect(row.querySelector(".rounded-full.bg-red")).toBeNull();
    expect(row.textContent).toContain("没发出去");
  });

  it("an info toast has no status icon, an ok one has the check", () => {
    show("提示", "info");
    show("好了", "ok");
    const [info, ok] = rows();
    expect(info.innerHTML).not.toContain(WARNING_PATH);
    expect(info.querySelectorAll("svg")).toHaveLength(1); // the close button's x only
    expect(ok.querySelector("svg")?.getAttribute("class")).toContain("text-green");
  });

  it("every toast has a labelled close button that drops just that toast", () => {
    show("一", "info");
    show("二", "error");
    show("三", "ok");
    expect(rows()).toHaveLength(3);
    for (const row of rows()) expect(row.querySelector("button[aria-label='关闭']")).not.toBeNull();
    act(() => rows()[1].querySelector<HTMLButtonElement>("button[aria-label='关闭']")!.click());
    expect(rows().map((r) => r.textContent)).toEqual(["一", "三"]);
  });

  it("a toast with an action keeps its button beside the close button; the action drops it", () => {
    const run = vi.fn();
    show("已删除", "info", { label: "撤销", run });
    const [row] = rows();
    const buttons = [...row.querySelectorAll("button")];
    expect(buttons.map((b) => b.getAttribute("aria-label") ?? b.textContent)).toEqual(["撤销", "关闭"]);
    act(() => buttons[0].click());
    expect(run).toHaveBeenCalledOnce();
    expect(rows()).toHaveLength(0);
  });

  it("the same line again within a few seconds is folded into one", () => {
    show("重复", "info");
    show("重复", "info");
    expect(rows()).toHaveLength(1);
  });

  it("keeps the timings: info 2.8s, error 5.2s, with an action 6s", () => {
    show("info", "info");
    show("err", "error");
    show("act", "info", { label: "撤销", run() {} });
    advance(2799);
    expect(rows()).toHaveLength(3);
    advance(1);
    expect(rows().map((r) => r.textContent)).toEqual(["err", "act撤销"]);
    advance(2400);
    expect(rows().map((r) => r.textContent)).toEqual(["act撤销"]);
    advance(800);
    expect(rows()).toHaveLength(0);
  });
});
