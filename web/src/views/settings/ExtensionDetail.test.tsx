// @vitest-environment jsdom
// An extension's page in Settings › Extensions (Android ExtensionModelParts): "Isolation" in its three states with the egress counts,
// and, for an extension that contributes a channel, the link to that channel's page in Settings › Channels.
import { act } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Channel } from "../../lib/channels";
import type { HubExtensionDetail } from "../../lib/extensions";

const detail = vi.hoisted(() => ({ value: null as unknown }));
vi.mock("../../lib/extensions", async (original) => ({
  ...(await original<typeof import("../../lib/extensions")>()),
  extensionDetail: () => Promise.resolve(detail.value),
}));
vi.mock("../../lib/channels", async (original) => ({
  ...(await original<typeof import("../../lib/channels")>()),
  useChannels: (): Channel[] => [{ id: "wechat", name: "微信", kind: "im", source: "plugin", installed: true, enabled: true, state: "ready" } as unknown as Channel],
}));
vi.mock("../../lib/store", async (original) => {
  const real = await original<typeof import("../../lib/store")>();
  return { ...real, useStore: <T,>(select: (s: ReturnType<typeof real.getState>) => T): T => select({ ...real.getState(), features: ["channels"] }) };
});
vi.mock("../../ui/atoms", async (original) => ({ ...(await original<typeof import("../../ui/atoms")>()), toast: vi.fn() }));
import { Extensions, SandboxGroup } from "./Extensions";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); });
};

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

describe("运行隔离", () => {
  it("says the sandbox is in effect, with what went through the egress proxy and the last refusal", () => {
    const html = renderToStaticMarkup(<SandboxGroup s={{ enabled: true, active: true, egress: { allowed: 3, denied: 1, last_denied: "example.com:443" } }} />);
    expect(html).toContain("运行隔离");
    expect(html).toContain("沙箱正在生效");
    expect(html).toContain("经出网代理：放行 3 次 · 拒绝 1 次");
    expect(html).toContain("最近拒绝：example.com:443");
  });
  it("says why a sandbox asked for is not in effect", () => {
    const html = renderToStaticMarkup(<SandboxGroup s={{ enabled: true, active: false, why: "这台机器没有 bubblewrap" }} />);
    expect(html).toContain("要求了沙箱，但现在没开起来");
    expect(html).toContain("这台机器没有 bubblewrap");
    expect(html).not.toContain("经出网代理");
  });
  it("says it is off", () => {
    expect(renderToStaticMarkup(<SandboxGroup s={{ enabled: false, active: false }} />)).toContain("未启用沙箱");
  });
});

describe("扩展详情里的渠道", () => {
  const ext = (over: Partial<HubExtensionDetail>): HubExtensionDetail => ({ id: "example-wechat", name: "示例微信", kind: "process", status: "running", source: "installed", enabled: true, ...over });

  it("links an extension that contributes a channel to its page, and shows its sandbox", async () => {
    detail.value = ext({ contributes: { channel: ["wechat"] }, sandbox: { enabled: true, active: false } });
    const onChannel = vi.fn();
    act(() => root.render(<Extensions open="example-wechat" onOpen={() => {}} onChannel={onChannel} />));
    await flush();
    expect(host.textContent).toContain("要求了沙箱，但现在没开起来");
    const link = [...host.querySelectorAll<HTMLElement>("button, a, [role=button]")].find((b) => b.textContent?.includes("在 设置 › 渠道 里设置微信"));
    expect(link).toBeTruthy();
    act(() => link!.click());
    expect(onChannel).toHaveBeenCalledWith("wechat");
  });

  it("has no channel link or sandbox for an extension without them", async () => {
    detail.value = ext({});
    act(() => root.render(<Extensions open="example-wechat" onOpen={() => {}} onChannel={() => {}} />));
    await flush();
    expect(host.textContent).toContain("示例微信");
    expect(host.textContent).not.toContain("渠道 里设置");
    expect(host.textContent).not.toContain("运行隔离");
  });
});
