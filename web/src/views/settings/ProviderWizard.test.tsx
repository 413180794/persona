// @vitest-environment jsdom
// Settings › Model providers › Add provider starts from the hub's presets (docs/onboarding.md 4.3 "Three paths" B): the built-in ones listed
// first, the gateway typed by hand last; a preset takes only the key, tests it by itself with the preset, saves it with
// assign and says what was assigned and what was kept.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});
import { resetReadiness, type Readiness } from "../../lib/readiness";
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
let readiness: Readiness | null = null;
// set: readiness answers only once it resolves (a node self-check can take seconds)
let readinessGate: Promise<void> | null = null;
const readinessGets = () => calls.filter((c) => c.path.endsWith("/assistant/readiness")).length;
const step = (id: string, state: string, code?: string) => ({ id, state, ...(code ? { code } : {}) });
const withSteps = (mode: string, steps: Readiness["steps"]): Readiness => ({
  version: 1, mode, ready: mode === "full", next: null, machine: { id: "mbp", name: "小林的 MacBook" }, launcher: "claude", steps, dismissed: false, checked_at: "2026-11-06T09:00:00Z",
});

const json = (v: unknown, status = 200) => new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
function hub(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const path = String(input).split("?")[0]!;
  const method = init?.method ?? "GET";
  const body = init?.body ? JSON.parse(String(init.body)) : undefined;
  calls.push({ method, path, body });
  if (path.endsWith("/assistant/readiness")) return (readinessGate ?? Promise.resolve()).then(() => (readiness ? json(readiness) : json({ error: "not found" }, 404)));
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
// The key auto-test debounce (AUTO_TEST_MS in ProviderWizard.tsx); timers are fake, so this costs no real time.
const AUTO_TEST_MS = 700;
const advance = async (ms: number) => {
  await act(async () => { await vi.advanceTimersByTimeAsync(ms); });
};
const keyTests = () => calls.filter((c) => c.path.endsWith("/launchers/test"));
const waitAutoTest = async () => {
  await advance(AUTO_TEST_MS);
  await flush();
};

beforeEach(() => {
  // only timers are faked: promises, fetch and React's scheduling keep running for real
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
  Object.assign(getState(), { features: ["builtin_presets"] });
  calls = [];
  testOK = true;
  readiness = null;
  readinessGate = null;
  resetReadiness();
  vi.stubGlobal("fetch", vi.fn(hub));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  vi.useRealTimers();
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
    const tests = keyTests();
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

  it("tests the key only once it has stopped changing for the whole debounce", async () => {
    await open();
    act(() => button("智谱 GLM")!.click());
    type(keyInput(), "sk-test-1234");
    await advance(AUTO_TEST_MS - 1);
    expect(keyTests()).toHaveLength(0);
    // another keystroke starts the wait over
    type(keyInput(), "sk-test-123456");
    await advance(AUTO_TEST_MS - 1);
    expect(keyTests()).toHaveLength(0);
    // the last millisecond of the wait sends it
    await advance(1);
    await flush();
    expect(keyTests()).toHaveLength(1);
    expect(keyTests()[0]!.body).toMatchObject({ api_key: "sk-test-123456" });
    expect(text()).toContain("Key 能用");
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
    expect(keyTests()).toHaveLength(0);
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

  it("leaves a Claude Code that already answers in place unless told otherwise", async () => {
    Object.assign(getState(), { features: ["builtin_presets", "assistant_readiness"] });
    readiness = withSteps("full", [step("computer", "ok"), step("claude", "ok"), step("model", "ok"), step("first_message", "ok")]);
    await open();
    act(() => button("智谱 GLM")!.click());
    expect(document.body.querySelector('button[role="switch"]')?.getAttribute("aria-checked")).toBe("false");
    expect(text()).toContain("小林的 MacBook 上的 Claude Code，已经能用");
    type(keyInput(), "sk-test-123456");
    await waitAutoTest();
    const before = readinessGets();
    await act(async () => button("保存")!.click());
    await flush();
    const post = calls.find((c) => c.method === "POST" && c.path.endsWith("/launchers"));
    expect(post?.body).not.toHaveProperty("assign");
    // saving asks for the readiness again, so the main conversation's card follows
    expect(readinessGets()).toBeGreaterThan(before);
    expect(document.body.querySelector('[data-testid="wizard-no-claude"]')).toBeNull();
  });

  it("waits for a slow readiness before deciding to take over the main conversation", async () => {
    Object.assign(getState(), { features: ["builtin_presets", "assistant_readiness"] });
    readiness = withSteps("full", [step("computer", "ok"), step("claude", "ok"), step("model", "ok"), step("first_message", "ok")]);
    let answer!: () => void;
    readinessGate = new Promise((r) => (answer = r));
    await open();
    act(() => button("智谱 GLM")!.click());
    // not known yet: no switch to read as on
    expect(document.body.querySelector('button[role="switch"]')).toBeNull();
    expect(text()).toContain("正在看主对话现在能不能用");
    type(keyInput(), "sk-test-123456");
    // the key test runs and saving is asked for while the readiness is still out: only the gate releases it
    await waitAutoTest();
    expect(keyTests()).toHaveLength(1);
    expect(document.body.querySelector('button[role="switch"]')).toBeNull();
    await act(async () => button("保存")!.click());
    await flush();
    expect(calls.some((c) => c.method === "POST" && c.path.endsWith("/launchers"))).toBe(false);
    await act(async () => answer());
    await flush();
    const post = calls.find((c) => c.method === "POST" && c.path.endsWith("/launchers"));
    expect(post?.body).not.toHaveProperty("assign");
  });

  it("says Claude Code is missing without promising the main conversation when the key did not go to it", async () => {
    Object.assign(getState(), { features: ["builtin_presets", "assistant_readiness"] });
    readiness = withSteps("setup", [step("computer", "ok"), step("claude", "problem", "claude_missing"), step("model", "unknown"), step("first_message", "todo")]);
    await open();
    act(() => button("智谱 GLM")!.click());
    act(() => (document.body.querySelector('button[role="switch"]') as HTMLButtonElement).click());
    type(keyInput(), "sk-test-123456");
    await waitAutoTest();
    await act(async () => button("保存")!.click());
    await flush();
    const note = document.body.querySelector('[data-testid="wizard-no-claude"]');
    expect(note?.textContent).toContain("Key 存好了；要用它，还得先在 小林的 MacBook 上装 Claude Code。");
  });

  it("says the key is saved but the computer still needs Claude Code", async () => {
    Object.assign(getState(), { features: ["builtin_presets", "assistant_readiness"] });
    readiness = withSteps("setup", [step("computer", "ok"), step("claude", "problem", "claude_missing"), step("model", "unknown"), step("first_message", "todo")]);
    await open();
    act(() => button("智谱 GLM")!.click());
    // nothing answers yet: the new key takes over the defaults
    expect(document.body.querySelector('button[role="switch"]')?.getAttribute("aria-checked")).toBe("true");
    type(keyInput(), "sk-test-123456");
    await waitAutoTest();
    // after saving the hub may no longer say it (a provider of its own reads unknown): what it said before still holds
    readiness = withSteps("setup", [step("computer", "ok"), step("claude", "unknown"), step("model", "unknown"), step("first_message", "todo")]);
    await act(async () => button("保存")!.click());
    await flush();
    const note = document.body.querySelector('[data-testid="wizard-no-claude"]');
    expect(note?.textContent).toContain("Key 存好了；还要在 小林的 MacBook 上装 Claude Code，主对话才能用它。");
    act(() => button("怎么装")!.click());
    expect(text()).toContain("claude.ai/install.sh");
  });
});
