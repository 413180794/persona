// @vitest-environment jsdom
// 设置 › 通知 (#2971): loading, a load that fails and 重试, a save the hub refuses (the control goes back, a toast says
// why), and the system switch when the browser has said no or has no notifications at all.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

type Pending = { method: string; resolve: (v: unknown) => void; reject: (e: unknown) => void };
const pending: Pending[] = [];
vi.mock("../../lib/api", () => ({
  api: (_path: string, init?: { method?: string }) => new Promise((resolve, reject) => pending.push({ method: init?.method ?? "GET", resolve, reject })),
  enc: encodeURIComponent,
  errorText: (e: unknown) => (e instanceof Error ? e.message : String(e)),
}));
const toast = vi.fn();
vi.mock("../../ui/atoms", async (orig) => ({ ...(await orig<typeof import("../../ui/atoms")>()), toast: (...a: unknown[]) => toast(...a) }));

const { NotificationSettings } = await import("./NotificationSettings");

let root: Root;
let host: HTMLDivElement;
beforeEach(() => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  pending.length = 0;
  toast.mockClear();
  localStorage.clear();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const render = () => act(async () => root.render(<NotificationSettings />));
const switches = () => [...host.querySelectorAll<HTMLButtonElement>('[role="switch"]')];
const answer = async (i: number, v: unknown) => act(async () => pending[i].resolve(v));
const fail = async (i: number, e: Error) => act(async () => pending[i].reject(e));

describe("设置 › 通知", () => {
  it("先转圈，hub 答了再显示两项设置", async () => {
    vi.stubGlobal("Notification", { permission: "granted", requestPermission: async () => "granted" });
    await render();
    expect(host.textContent).not.toContain("需要你处理时");
    await answer(0, { input: false, done: "always" });
    expect(host.textContent).toContain("需要你处理时");
    expect(switches()[1].getAttribute("aria-checked")).toBe("false");
    expect(host.querySelector('[role="radio"][aria-checked="true"]')?.textContent).toBe("每次都推");
  });

  it("读不到时说为什么，点重试再读", async () => {
    await render();
    await fail(0, new Error("hub 没回答"));
    expect(host.textContent).toContain("读不到通知设置");
    expect(host.textContent).toContain("hub 没回答");
    const retry = [...host.querySelectorAll("button")].find((b) => b.textContent === "重试")!;
    await act(async () => retry.click());
    expect(pending).toHaveLength(2);
    await answer(1, { input: true, done: "long" });
    expect(host.textContent).toContain("需要你处理时");
    expect(host.textContent).not.toContain("读不到通知设置");
  });

  it("hub 不让保存：开关回到原来的样子，并提示没保存上", async () => {
    await render();
    await answer(0, { input: true, done: "long" });
    const input = switches()[1];
    await act(async () => input.click());
    expect(switches()[1].getAttribute("aria-checked")).toBe("false"); // optimistic
    expect(pending[1].method).toBe("PUT");
    await fail(1, new Error("500"));
    expect(switches()[1].getAttribute("aria-checked")).toBe("true");
    expect(toast).toHaveBeenCalledWith(expect.stringContaining("没保存上"), "error");
  });

  it("浏览器拒绝了：开关关着、点不了，并说去哪改", async () => {
    vi.stubGlobal("Notification", { permission: "denied", requestPermission: async () => "denied" });
    localStorage.setItem("persona.systemNotify", "1");
    await render();
    const sys = switches()[0];
    expect(sys.disabled).toBe(true);
    expect(sys.getAttribute("aria-checked")).toBe("false");
    expect(host.textContent).toContain("已拒绝");
    expect(host.textContent).toContain("网站设置");
  });

  it("浏览器不支持：开关点不了，并说明", async () => {
    vi.stubGlobal("Notification", undefined);
    await render();
    expect(switches()[0].disabled).toBe(true);
    expect(host.textContent).toContain("这个浏览器不支持系统通知");
  });
});
