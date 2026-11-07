// @vitest-environment jsdom
// A routine's `brief` item (Brief.tsx): the fixed part of a morning or evening run, drawn in the conversation, with an
// Accept button per task to verify that posts the round it was made for.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../../lib/api";
import type { FeedItem } from "../../lib/feed";

const api = vi.hoisted(() => vi.fn());
vi.mock("../../lib/api", async (orig) => ({ ...(await orig<typeof import("../../lib/api")>()), api }));
import { FeedLineView } from "./Tasks";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
beforeEach(() => {
  api.mockReset();
  api.mockResolvedValue({});
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const ASKED = "2026-11-06T01:02:03.123456789Z";
const body = (over: Record<string, unknown> = {}): Record<string, unknown> => ({
  title: "早报",
  at: "2026-11-07T01:00:00Z",
  hours: 12,
  waiting: [{ id: "ix1", title: "先发预发还是直接上线？", from: "来自 Agent「登录修复」", link: "https://hub.example/s/box/ag1", waited_s: 4800, nudged: true }],
  failed: [{ kind: "task", id: "todo_f", title: "修登录崩溃", at: "2026-11-07T00:10:00Z", why: "machine offline" }, { kind: "routine", id: "patrol", title: "巡检", at: "2026-11-07T00:20:00Z", skipped: true }],
  failed_total: 2,
  done: [{ id: "todo_d", title: "整理周报", at: "2026-11-06T22:00:00Z" }],
  done_total: 4,
  accept: [{ id: "todo_v1", title: "改设置页", detail: "设置页多了一个开关", from: "来自某个 Agent", waited_s: 7200, asked_at: ASKED }],
  ...over,
});
const item = (b: Record<string, unknown> = body()): FeedItem => ({ id: "f1", conversation: "c1", at: "2026-11-07T01:00:05Z", kind: "brief", ref: "morning", body: b });

function draw(it: FeedItem) {
  act(() => root.render(<FeedLineView item={it} />));
}
const rows = (kind: string) => [...host.querySelectorAll(`[data-brief-row=${kind}]`)];

describe("a brief item", () => {
  it("draws the sections: what waits, the last hours (failed first), what is to be verified", () => {
    draw(item());
    const text = host.textContent ?? "";
    for (const want of ["早报", "等你处理（1）", "先发预发还是直接上线？", "已等 1 小时 20 分", "已催过", "过去 12 小时：做完 4 · 没成 2", "修登录崩溃", "没开起来", "machine offline", "巡检", "被跳过", "整理周报", "待验收（1）", "改设置页", "设置页多了一个开关"]) {
      expect(text, want).toContain(want);
    }
    expect(text.indexOf("修登录崩溃")).toBeLessThan(text.indexOf("整理周报"));
    expect(host.querySelector<HTMLAnchorElement>("a[href='https://hub.example/s/box/ag1']")).not.toBeNull();
    expect(rows("accept")).toHaveLength(1);
    expect(text).toContain("还有 3 项"); // 4 done + 2 failed in all, 3 listed
  });

  it("draws a link only when it is a web address", () => {
    draw(item(body({ waiting: [{ id: "ix1", title: "一个问题", waited_s: 60, link: "javascript:alert(1)" }, { id: "ix2", title: "另一个", waited_s: 60, link: "https://hub.example/s/box/ag2" }] })));
    expect([...host.querySelectorAll("a")].map((a) => a.getAttribute("href"))).toEqual(["https://hub.example/s/box/ag2"]);
  });

  it("draws what is in progress last, with its stage bar and how many more there are", () => {
    draw(item(body({ progress: [{ id: "todo_w1", title: "重构设置页", stage: "develop" }, { id: "todo_w2", title: "整理文档" }], progress_total: 3 })));
    const text = host.textContent ?? "";
    expect(text).toContain("进行中（3）");
    expect(text.indexOf("待验收（1）")).toBeLessThan(text.indexOf("进行中（3）"));
    expect(rows("progress")).toHaveLength(2);
    expect(rows("progress")[0].querySelector("ol[aria-label]")).not.toBeNull(); // the stage bar
    expect(rows("progress")[1].querySelector("ol[aria-label]")).toBeNull(); // no stage known: no bar
    expect(text).toContain("还有 1 项");
    draw(item()); // nothing running: no section
    expect(host.textContent).not.toContain("进行中");
  });

  it("says so when nothing is there, and does not say that nothing waits when the cards were not read", () => {
    draw(item(body({ waiting: [], failed: [], failed_total: 0, done: [], done_total: 0, accept: [] })));
    expect(host.textContent).toContain("等你处理（0）");
    expect(host.querySelectorAll("[data-brief-section]")).toHaveLength(3);
    expect(host.textContent).not.toContain("还有");
    draw(item(body({ waiting: [], waiting_unknown: true })));
    expect(host.textContent).toContain("还没读到等你处理的卡片");
    expect(host.textContent).not.toContain("等你处理（0）");
  });

  it("draws nothing for a body that is not one", () => {
    draw({ ...item(), body: undefined });
    expect(host.querySelector("[data-testid=brief-card]")).toBeNull();
    draw(item(body({ waiting: null, failed: "x", done: undefined, accept: 7 })));
    expect(host.querySelector("[data-testid=brief-card]")).not.toBeNull(); // lists it does not understand are empty ones
  });

  it("Accept posts the round it was made for, then shows the task as accepted", async () => {
    draw(item());
    const btn = [...host.querySelectorAll("button")].find((b) => b.textContent === "通过")!;
    await act(async () => btn.click());
    expect(api).toHaveBeenCalledWith("/todos/todo_v1/accept", { method: "POST", json: { asked_at: ASKED } });
    expect(host.querySelector("[data-brief-accepted=accepted]")?.textContent).toBe("已验收");
    expect([...host.querySelectorAll("button")].some((b) => b.textContent === "通过")).toBe(false);
  });

  it("a round that is over (409) says it was handled; another failure keeps the button for a retry", async () => {
    draw(item());
    api.mockRejectedValueOnce(new ApiError(409, "已经处理过了", {}));
    await act(async () => [...host.querySelectorAll("button")].find((b) => b.textContent === "通过")!.click());
    expect(host.querySelector("[data-brief-accepted=handled]")?.textContent).toBe("已经处理过了");

    draw(item(body({ accept: [{ id: "todo_v2", title: "另一件", waited_s: 60, asked_at: ASKED }] })));
    api.mockRejectedValueOnce(new ApiError(500, "boom", {}));
    await act(async () => [...host.querySelectorAll("button")].find((b) => b.textContent === "通过")!.click());
    expect(host.querySelector("[data-brief-accepted]")).toBeNull();
    expect([...host.querySelectorAll("button")].some((b) => b.textContent === "通过" && !b.disabled)).toBe(true);
  });

  it("has no Accept button for a task that has no round to name", () => {
    draw(item(body({ accept: [{ id: "todo_v3", title: "没有轮次", waited_s: 60 }] })));
    expect([...host.querySelectorAll("button")].find((b) => b.textContent === "通过")?.disabled).toBe(true);
  });
});
