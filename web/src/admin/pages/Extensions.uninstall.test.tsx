// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  window.matchMedia ??= ((media: string) => ({ matches: false, media, addEventListener() {}, removeEventListener() {} })) as typeof window.matchMedia;
});
vi.mock("../../ui/atoms", async (original) => ({ ...(await original<typeof import("../../ui/atoms")>()), toast: vi.fn() }));
import { toast } from "../../ui/atoms";
import { resetExtensionRoot, setCsrf, setTransport } from "../api";
import { Extensions } from "./Extensions";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const warnings = Array.from({ length: 5 }, (_, i) => `请手动删除 /tmp/example-extension-${i % 4}/${"long-directory/".repeat(16)}data`);
let host: HTMLDivElement, root: Root;
let replies: { status?: number; body: unknown }[];
let requests: { path: string; body: unknown }[];
const flush = async () => { for (let i = 0; i < 8; i++) await act(async () => { await Promise.resolve(); }); };
const dialog = () => document.querySelector<HTMLElement>('[role="dialog"]');
const click = async (label: string) => {
  const button = [...dialog()!.querySelectorAll("button")].find((b) => b.textContent?.trim() === label);
  expect(button, label).toBeTruthy();
  await act(async () => button!.click());
  await flush();
};
const open = async () => {
  await act(async () => root.render(<Extensions />));
  await flush();
  const remove = host.querySelector<HTMLButtonElement>('button[aria-label="卸载「示例扩展」"]');
  expect(remove).toBeTruthy();
  await act(async () => remove!.click());
  await flush();
};

beforeEach(() => {
  vi.useFakeTimers();
  vi.mocked(toast).mockClear();
  resetExtensionRoot();
  setCsrf("tok");
  requests = [];
  replies = [{ body: { ok: true, warnings } }];
  setTransport(async (path, init) => {
    const reply = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status });
    if (init?.method === "DELETE") {
      requests.push({ path, body: JSON.parse(String(init.body)) });
      const result = replies.shift()!;
      return reply(result.body, result.status);
    }
    if (path === "/api/v1/admin/extensions") return reply({ can_install: false, extensions: [{ id: "example", name: "示例扩展", source: "installed", status: "running", enabled: true, permissions: [] }] });
    return reply({ extensions: [], items: [] });
  });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  document.body.innerHTML = "";
  setTransport((path, init) => fetch(path, init));
  resetExtensionRoot();
  setCsrf("");
  vi.useRealTimers();
});

describe("admin uninstall warnings", () => {
  it("keeps all warnings in the completed dialog until closed", async () => {
    await open();
    await click("卸载");
    expect(dialog()?.textContent ?? "").toContain(warnings[0]);
    expect([...dialog()!.querySelectorAll("li")].map((li) => li.textContent)).toEqual(warnings);
    expect(toast).not.toHaveBeenCalled();
    expect(dialog()?.querySelectorAll("input")).toHaveLength(0);
    expect([...dialog()!.querySelectorAll("button")].map((b) => b.textContent?.trim()).filter(Boolean)).toEqual(["关闭"]);
    await act(async () => { vi.advanceTimersByTime(60_000); });
    await flush();
    for (const warning of warnings) expect(dialog()?.textContent).toContain(warning);
    await click("关闭");
    await act(async () => { vi.advanceTimersByTime(500); });
    expect(dialog()).toBeNull();
    expect(requests).toEqual([{ path: "/api/v1/admin/extensions/example", body: { keep_data: false } }]);
  });

  it.each([{ value: undefined }, { value: null }, { value: [] }])("keeps the old success flow for warnings=$value", async ({ value }) => {
    replies = [{ body: { ok: true, ...(value === undefined ? {} : { warnings: value }) } }];
    await open();
    await click("卸载");
    expect(toast).toHaveBeenCalledWith("已卸载「示例扩展」", "ok");
    await act(async () => { vi.advanceTimersByTime(500); });
    expect(dialog()).toBeNull();
  });

  it("preserves a failed request for retry, including the keep-data choice", async () => {
    replies = [{ status: 500, body: { error: "cleanup could not start" } }, { body: { ok: true } }];
    await open();
    await act(async () => dialog()!.querySelector<HTMLInputElement>('input[type="checkbox"]')!.click());
    await click("卸载");
    expect(dialog()?.textContent).toContain("cleanup could not start");
    await click("卸载");
    expect(requests).toEqual(Array(2).fill({ path: "/api/v1/admin/extensions/example", body: { keep_data: true } }));
  });
});
