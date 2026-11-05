// @vitest-environment jsdom
// 设置 › 模型供应商 › 新增供应商 starts from the hub's presets (docs/onboarding.md 4.3「三条路」B): the built-in ones listed
// first, the gateway typed by hand last; a preset takes only the key, tests it by itself with the preset, saves it with
// assign and says what was assigned and what was kept.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});
import { getState } from "../../lib/store";
import { ProviderWizard } from "./ProviderWizard";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const presets = [
  { id: "builtin/glm", plugin: "builtin", name: "Zhipu GLM", display_name: "智谱 GLM", description: "国内直接开通", kind: "launcher", base_url: "https://open.example.cn/api/anthropic", auth: "bearer", model: "glm-x", signup_url: "https://open.example.cn/keys" },
  { id: "example-gateway/team", plugin: "example-gateway", name: "示例网关", kind: "launcher", base_url: "https://gateway.example.com", model: "example-pro" },
  { id: "example-gateway/api", plugin: "example-gateway", name: "示例 Responses", kind: "api", api: "responses", base_url: "https://api.example.com", model: "example-r" },
];
let calls: { method: string; path: string; body?: Record<string, unknown> }[] = [];
let testOK = true;

const json = (v: unknown, status = 200) => new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
function hub(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const path = String(input).split("?")[0]!;
  const method = init?.method ?? "GET";
  const body = init?.body ? JSON.parse(String(init.body)) : undefined;
  calls.push({ method, path, body });
  if (path.endsWith("/launchers/presets")) return Promise.resolve(json({ presets }));
  if (path.endsWith("/launchers/test")) return Promise.resolve(json(testOK ? { ok: true, ms: 321, model: "glm-x" } : { ok: false, ms: 80, model: "glm-x", error: "Key 不对（401）" }));
  if (path.endsWith("/launchers") && method === "POST")
    return Promise.resolve(json({ launcher: { name: "glm", display_name: "智谱 GLM", has_key: true }, assigned: ["assistant"], kept: [{ purpose: "agent.new", reason: "customized" }] }));
  return Promise.resolve(json({}));
}

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); });
};
const text = () => document.body.textContent ?? "";
const button = (label: string) => [...document.body.querySelectorAll("button")].find((b) => b.textContent?.includes(label));
function type(input: HTMLInputElement, value: string) {
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
  act(() => {
    set.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}
const keyInput = () => document.body.querySelector<HTMLInputElement>('input[type="password"]')!;
const waitAutoTest = async () => {
  await act(async () => { await new Promise((r) => setTimeout(r, 800)); });
  await flush();
};

beforeEach(() => {
  Object.assign(getState(), { features: ["builtin_presets"] });
  calls = [];
  testOK = true;
  vi.stubGlobal("fetch", vi.fn(hub));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function open() {
  const saved = vi.fn();
  act(() => root.render(<ProviderWizard existing="new" onClose={() => {}} onSaved={saved} />));
  await flush();
  return saved;
}

describe("ProviderWizard from presets", () => {
  it("lists the hub's presets with the custom gateway last and no hard-coded Anthropic", async () => {
    await open();
    const rows = [...document.body.querySelectorAll("ul li")].map((li) => li.textContent ?? "");
    expect(rows).toHaveLength(4);
    expect(rows[0]).toContain("智谱 GLM");
    expect(rows[1]).toContain("示例网关");
    expect(rows[2]).toContain("示例 Responses");
    expect(rows[3]).toContain("自定义兼容网关");
    expect(text()).not.toContain("Anthropic 官方");
  });

  it("takes only the key, tests it with the preset by itself, and saves it with assign", async () => {
    const saved = await open();
    act(() => button("智谱 GLM")!.click());
    expect(text()).not.toContain("鉴权方式");
    const link = document.body.querySelector<HTMLAnchorElement>('a[href="https://open.example.cn/keys"]');
    expect(link?.target).toBe("_blank");
    expect(link?.rel).toContain("noopener");

    type(keyInput(), "sk-test-123456");
    await waitAutoTest();
    const tests = calls.filter((c) => c.path.endsWith("/launchers/test"));
    expect(tests).toHaveLength(1);
    expect(tests[0]!.body).toEqual({ preset: "builtin/glm", api_key: "sk-test-123456", auth: "bearer" });
    expect(text()).toContain("Key 能用");

    await act(async () => button("保存")!.click());
    await flush();
    const post = calls.find((c) => c.method === "POST" && c.path.endsWith("/launchers"));
    expect(post?.body).toEqual({ api_key: "sk-test-123456", preset: "builtin/glm", assign: ["assistant", "agent.new"] });
    expect(saved).toHaveBeenCalled();
    expect(text()).toContain("智谱 GLM 接好了");
    expect(text()).toContain("现在用它：主对话");
    expect(text()).toContain("新开的 Agent：没有改");
  });

  it("says why a key failed and leaves assign out when turned off", async () => {
    testOK = false;
    await open();
    act(() => button("智谱 GLM")!.click());
    type(keyInput(), "sk-wrong-123456");
    await waitAutoTest();
    expect(text()).toContain("Key 不对（401）");
    expect(button("重新测试")).toBeDefined();

    act(() => document.body.querySelector<HTMLButtonElement>('button[role="switch"]')!.click());
    await act(async () => button("保存")!.click());
    await flush();
    const post = calls.find((c) => c.method === "POST" && c.path.endsWith("/launchers"));
    expect(post?.body).not.toHaveProperty("assign");
    expect(text()).toContain("到「模型配置」里选");
  });

  it("asks an older hub's preset for the name and sends no assign", async () => {
    Object.assign(getState(), { features: ["channels"] });
    await open();
    act(() => button("示例网关")!.click());
    expect(document.body.querySelector('button[role="switch"]')).toBeNull();
    type(keyInput(), "sk-test-123456");
    await waitAutoTest();
    expect(button("保存")!.disabled).toBe(true);
    type(document.body.querySelector<HTMLInputElement>('input:not([type="password"])')!, "jv_team");
    expect(button("保存")!.disabled).toBe(false);
    await act(async () => button("保存")!.click());
    await flush();
    const post = calls.find((c) => c.method === "POST" && c.path.endsWith("/launchers"));
    expect(post?.body).toEqual({ name: "jv_team", api_key: "sk-test-123456", preset: "example-gateway/team" });
  });

  it("does not test a Responses preset's key, which the hub's test cannot reach", async () => {
    await open();
    act(() => button("示例 Responses")!.click());
    type(keyInput(), "sk-test-123456");
    await waitAutoTest();
    expect(calls.filter((c) => c.path.endsWith("/launchers/test"))).toHaveLength(0);
    expect(text()).toContain("Responses 协议的 Key 没法在这里测试");
    expect(text()).not.toContain("重新测试");
    expect(button("保存")!.disabled).toBe(false);
  });

  it("asks for the address and the auth only for the custom gateway", async () => {
    await open();
    act(() => button("自定义兼容网关")!.click());
    expect(text()).toContain("鉴权方式");
    expect(text()).toContain("地址");
  });
});
