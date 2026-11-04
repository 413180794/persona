import { afterEach, describe, expect, it, vi } from "vitest";
import {
  addMarket, agreeLabel, canUninstall, catalogQuery, getCatalog, installPreview, isGitMarket, itemLine, itemState, listMarkets, MARKET_SPEC_HINT, marketKind, marketLine, marketOf,
  pluginsRoute, previewFromMarket, previewVersion, refreshMarket, removeMarket, routeSub, setMarketToken, specLabel, uninstallPlugin, upgradePreview,
  type CatalogItem, type Market, type MarketPreview, type PluginsRoute,
} from "./plugin-market";

afterEach(() => vi.unstubAllGlobals());

const item = (extra: Partial<CatalogItem> = {}): CatalogItem => ({ market: "m1", market_name: "Example 插件", id: "example-cal", name: "Example 日历", version: "1.1.0", ...extra });

describe("市场的规矩", () => {
  it("says where a catalog plugin stands", () => {
    expect(itemState(item())).toEqual({ kind: "install", label: "未安装", tone: "gray" });
    expect(itemState(item({ problem: "没有这台 hub 能用的包" }))).toMatchObject({ kind: "problem", label: "装不了" });
    expect(itemState(item({ installed: "1.0.0", installed_source: "installed", upgradable: true }))).toMatchObject({ kind: "upgrade", label: "可升级到 1.1.0" });
    expect(itemState(item({ installed: "1.1.0", installed_source: "installed" }))).toMatchObject({ kind: "installed", label: "已装 1.1.0" });
    expect(itemState(item({ installed: "1.0.0", installed_source: "builtin" }))).toMatchObject({ kind: "elsewhere", label: "hub 自带" });
    expect(itemState(item({ installed: "1.0.0", installed_source: "hub.json" }))).toMatchObject({ kind: "elsewhere" });
    expect(itemState(item({ installed: "1.0.0", installed_source: "installed", upgradable: true, problem: "要更新的 hub" }))).toMatchObject({ kind: "problem" });
  });
  it("uninstalls only what this market installed", () => {
    expect(canUninstall(item({ installed: "1.0.0", from_this_market: true }))).toBe(true);
    expect(canUninstall(item({ installed: "1.0.0" }))).toBe(false);
    expect(canUninstall(item({ from_this_market: true }))).toBe(false);
    expect(marketOf("market:3f2a/example-gateway")).toBe("3f2a");
    expect(marketOf("dir:/opt/x")).toBe("");
    expect(marketOf(undefined)).toBe("");
  });
  it("leaves the address to the hub, and knows git markets (no token)", () => {
    expect(["git:git@example.com:team/plugins.git#main", "git@example.com:team/plugins.git", "git:ssh://git@example.com/team/plugins.git", "git:https://example.com/team/plugins.git"].map(isGitMarket)).toEqual([true, true, true, true]);
    expect(["https://example.com/plugins/", "github:example/x", "not even an address"].map(isGitMarket)).toEqual([false, false, false]);
    expect(["github:example/x", "git@example.com:a/b.git", "https://example.com/"].map(marketKind)).toEqual(["github", "git", "web"]);
    expect(specLabel("git:ssh://git@example.com/team/plugins.git")).toBe("ssh://git@example.com/team/plugins.git");
    for (const w of ["https", "github:owner/repo", "git:"]) expect(MARKET_SPEC_HINT).toContain(w);
  });
  it("words a market and a preview", () => {
    const m: Market = { id: "m1", spec: "github:example/x", name: "x", has_token: true, plugins: 3, fetched_at: "2026-11-04T06:00:00Z" };
    expect(marketLine(m, () => "1 小时前")).toBe("3 个插件 · 1 小时前读过 · 有 token");
    expect(marketLine({ ...m, has_token: false, fetched_at: undefined }, () => "")).toBe("3 个插件 · 还没读过");
    expect(specLabel("github:example/x")).toBe("example/x");
    expect(specLabel("https://plugins.example.com/persona/persona-plugins.json")).toBe("plugins.example.com/persona/persona-plugins.json");
    expect(specLabel("https://example.com/")).toBe("example.com");
    expect(itemLine(item({ publisher: "example" }))).toBe("1.1.0 · example · Example 插件");
    const pv: MarketPreview = { preview_id: "p", manifest: { id: "x", name: "X", version: "1.1.0" }, permissions: [], permissions_digest: "d", expires_at: "" };
    expect([previewVersion(pv), agreeLabel(pv)]).toEqual(["1.1.0", "同意并安装"]);
    expect([previewVersion({ ...pv, upgrade: true, from: "1.0.0" }), agreeLabel({ ...pv, upgrade: true })]).toEqual(["1.0.0 → 1.1.0", "升级"]);
    expect(agreeLabel({ ...pv, upgrade: true, more_permissions: true })).toBe("同意新权限并升级");
  });
  it("reads and writes the tab's route", () => {
    const cases: [string | null, PluginsRoute][] = [
      [null, { kind: "list" }],
      ["example-notes", { kind: "plugin", id: "example-notes" }],
      ["example-gateway/ui/home", { kind: "page", id: "example-gateway", ui: "home" }],
      ["-market", { kind: "catalog" }],
      ["-market/3f2a/example-cal", { kind: "item", market: "3f2a", id: "example-cal" }],
      ["-markets", { kind: "markets" }],
    ];
    for (const [sub, r] of cases) {
      expect(pluginsRoute(sub)).toEqual(r);
      expect(routeSub(r)).toBe(sub);
    }
    expect(pluginsRoute("-weird")).toEqual({ kind: "list" });
    expect(pluginsRoute("a/b")).toEqual({ kind: "list" });
  });
});

describe("市场的接口", () => {
  it("calls the hub's market routes", async () => {
    const calls: string[] = [];
    vi.stubGlobal("localStorage", { getItem: () => "t", setItem: () => {}, removeItem: () => {} });
    vi.stubGlobal("fetch", async (url: string, init: RequestInit) => {
      calls.push(`${init.method ?? "GET"} ${url} ${(init.body as string) ?? ""}`.trim());
      if (url.endsWith("/markets") && !init.method) return new Response(JSON.stringify({ markets: [{ id: "m1" }] }));
      if (url.includes("/catalog?") || url.endsWith("/catalog")) return new Response(JSON.stringify({ plugins: [item()] }));
      return new Response(JSON.stringify({ id: "x", name: "X", version: "1" }));
    });
    expect((await listMarkets()).map((m) => m.id)).toEqual(["m1"]);
    await addMarket("github:example/x");
    await addMarket("github:example/y", "tok");
    await removeMarket("m 1");
    await refreshMarket("m1");
    await setMarketToken("m1", "");
    const c = await getCatalog(" 日历 ", "m1", true);
    expect(c.markets).toEqual([]);
    expect(c.plugins).toHaveLength(1);
    await getCatalog();
    const pv: MarketPreview = { preview_id: "pv1", manifest: { id: "x", name: "X", version: "1" }, permissions: [], permissions_digest: "d1", expires_at: "" };
    await previewFromMarket("m1", "x");
    await installPreview(pv);
    await upgradePreview("x", pv);
    await uninstallPlugin("x");
    await uninstallPlugin("x", true);
    expect(calls).toEqual([
      "GET /api/v1/plugins/-/markets",
      'POST /api/v1/plugins/-/markets {"spec":"github:example/x"}',
      'POST /api/v1/plugins/-/markets {"spec":"github:example/y","token":"tok"}',
      "DELETE /api/v1/plugins/-/markets/m%201",
      "POST /api/v1/plugins/-/markets/m1/refresh",
      'PUT /api/v1/plugins/-/markets/m1/token {"token":""}',
      "GET /api/v1/plugins/-/catalog?q=%E6%97%A5%E5%8E%86&market=m1&refresh=1",
      "GET /api/v1/plugins/-/catalog",
      'POST /api/v1/plugins/-/catalog/preview {"market":"m1","plugin":"x"}',
      'POST /api/v1/plugins/-/install {"preview_id":"pv1","permissions_digest":"d1"}',
      'POST /api/v1/plugins/x/-/upgrade {"preview_id":"pv1","permissions_digest":"d1"}',
      "DELETE /api/v1/plugins/x/-/",
      "DELETE /api/v1/plugins/x/-/?keep_data=1",
    ]);
    expect(catalogQuery()).toBe("");
  });
});
