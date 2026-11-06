// @vitest-environment jsdom
// The catalog's empty line (#4480): with several markets and none picked, one unreadable and the others empty, it does
// not say "This marketplace has no extensions yet" under the unreadable banner. A GitHub market without a token that failed to read offers
// the token dialog here, where the user sees the error.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Catalog, Market } from "../../lib/extension-market";

const catalog = vi.hoisted(() => ({ value: null as unknown }));
vi.mock("../../lib/extension-market", async (original) => ({
  ...(await original<typeof import("../../lib/extension-market")>()),
  getCatalog: () => Promise.resolve(catalog.value),
}));
import { MarketCatalog } from "./ExtensionMarket";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 6; i++) await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
};
const market = (id: string, extra: Partial<Market> = {}): Market => ({ id, spec: `https://example.com/${id}`, name: id, has_token: false, extensions: 0, ...extra });
const button = (label: string) => [...document.querySelectorAll("button")].find((b) => b.textContent?.includes(label));

beforeEach(() => {
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
  document.body.innerHTML = "";
});

describe("MarketCatalog empty line", () => {
  it("says the readable markets are empty when another is unreadable", async () => {
    catalog.value = { markets: [market("official", { error: "timeout" }), market("team")], extensions: [] } satisfies Catalog;
    act(() => root.render(<MarketCatalog onBack={() => {}} onOpen={() => {}} onMarkets={() => {}} />));
    await flush();
    expect(host.textContent).toContain("读不到「official」");
    expect(host.textContent).toContain("读到的市场里还没有扩展");
    expect(host.textContent).not.toContain("这个市场还没有扩展");
  });

  it("keeps 这个市场还没有扩展 for a single readable market", async () => {
    catalog.value = { markets: [market("official")], extensions: [] } satisfies Catalog;
    act(() => root.render(<MarketCatalog onBack={() => {}} onOpen={() => {}} onMarkets={() => {}} />));
    await flush();
    expect(host.textContent).toContain("这个市场还没有扩展");
  });
});

describe("MarketCatalog rate-limited GitHub market", () => {
  it("offers a token under the error of a GitHub market without one", async () => {
    catalog.value = { markets: [market("team", { spec: "github:example/extensions", error: "HTTP 403" })], extensions: [] } satisfies Catalog;
    act(() => root.render(<MarketCatalog onBack={() => {}} onOpen={() => {}} onMarkets={() => {}} />));
    await flush();
    expect(host.textContent).toContain("GitHub 不带 token 时每小时只能读 60 次");
    expect(button("填 token")).toBeTruthy();
  });

  it("does not offer a token when the market has one or is not GitHub", async () => {
    catalog.value = {
      markets: [market("with-token", { spec: "github:example/extensions", has_token: true, error: "HTTP 403" }), market("site", { error: "HTTP 500" })],
      extensions: [],
    } satisfies Catalog;
    act(() => root.render(<MarketCatalog onBack={() => {}} onOpen={() => {}} onMarkets={() => {}} />));
    await flush();
    expect(host.textContent).not.toContain("GitHub 不带 token 时每小时只能读 60 次");
    expect(button("填 token")).toBeUndefined();
  });

  it("asks no token of a newcomer for the official market: grey, and a quiet way to the markets page", async () => {
    catalog.value = {
      markets: [market("official", { spec: "github:example/official", official: true, error: "HTTP 403" })],
      extensions: [],
    } satisfies Catalog;
    const onMarkets = vi.fn();
    act(() => root.render(<MarketCatalog onBack={() => {}} onOpen={() => {}} onMarkets={onMarkets} />));
    await flush();
    expect(host.textContent).toContain("暂时读不到官方扩展");
    expect(host.textContent).not.toContain("GitHub 不带 token 时每小时只能读 60 次");
    expect(button("填 token")).toBeUndefined();
    const link = button("去「管理市场」填一个 GitHub token");
    expect(link).toBeTruthy();
    act(() => link!.click());
    expect(onMarkets).toHaveBeenCalledOnce();
  });

  it("opens the token dialog for that market", async () => {
    catalog.value = { markets: [market("team", { spec: "github:example/extensions", error: "HTTP 403" })], extensions: [] } satisfies Catalog;
    act(() => root.render(<MarketCatalog onBack={() => {}} onOpen={() => {}} onMarkets={() => {}} />));
    await flush();
    const fix = button("填 token");
    expect(fix).toBeTruthy();
    act(() => fix!.click());
    await flush();
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain("「team」的 token");
    // and how to get one: GitHub's token page, filled in
    const create = [...document.querySelectorAll('[role="dialog"] a')].find((a) => a.textContent?.includes("去 GitHub 生成")) as HTMLAnchorElement | undefined;
    expect(create?.href).toContain("https://github.com/settings/personal-access-tokens/new?");
    expect(create?.target).toBe("_blank");
  });
});
