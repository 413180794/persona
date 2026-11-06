// @vitest-environment jsdom
// The catalog's empty line (#4480): with several markets and none picked, one unreadable and the others empty, it does
// not say 这个市场还没有扩展 under the unreadable banner.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Catalog } from "../../lib/extension-market";

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
const market = (id: string, error?: string) => ({ id, spec: `https://example.com/${id}`, name: id, has_token: false, extensions: 0, error });

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

describe("MarketCatalog empty line", () => {
  it("says the readable markets are empty when another is unreadable", async () => {
    catalog.value = { markets: [market("official", "timeout"), market("team")], extensions: [] } satisfies Catalog;
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
