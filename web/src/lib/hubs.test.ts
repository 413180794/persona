import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { acceptHubs, addHub, hubTitle, loadHubs, MAX_HUBS, mergeHubs, normalizeHubURL, rememberHub, removeHub, renameHub, switchURL, takeHubs, type SavedHub } from "./hubs";

describe("normalizeHubURL", () => {
  it("keeps only the origin, the way location.origin writes it", () => {
    expect(normalizeHubURL("https://hub.example.com")).toEqual({ url: "https://hub.example.com" });
    expect(normalizeHubURL(" https://hub.example.com:443/ ")).toEqual({ url: "https://hub.example.com" });
    expect(normalizeHubURL("http://192.0.2.10:18660")).toEqual({ url: "http://192.0.2.10:18660" });
    expect(normalizeHubURL("HTTPS://Hub.Example.COM:8443")).toEqual({ url: "https://hub.example.com:8443" });
    expect(normalizeHubURL("http://[2001:db8::10]:18660")).toEqual({ url: "http://[2001:db8::10]:18660" });
    // the sub-path a reverse proxy serves the hub under (docs/web/web.md "Sub-path")
    expect(normalizeHubURL("https://hub.example.com/persona-hub/")).toEqual({ url: "https://hub.example.com/persona-hub" });
    expect(normalizeHubURL("https://hub.example.com/persona")).toEqual({ url: "https://hub.example.com/persona" });
    expect(normalizeHubURL("https://hub.example.com/x/../hub")).toEqual({ url: "https://hub.example.com/hub" });
  });
  it("reads no scheme as https", () => {
    expect(normalizeHubURL("hub.example.com")).toEqual({ url: "https://hub.example.com" });
    expect(normalizeHubURL("hub.example.com:8443")).toEqual({ url: "https://hub.example.com:8443" });
  });
  it("says what is wrong with anything that is not just an address", () => {
    expect(normalizeHubURL("  ")).toEqual({ error: "填 hub 的地址" });
    expect(normalizeHubURL("ftp://hub.example.com")).toEqual({ error: "只能是 http:// 或 https:// 的地址" });
    expect(normalizeHubURL("https://me:pw@hub.example.com")).toEqual({ error: "地址里不要带用户名和密码" });
    const noPath = "只填 hub 的地址（可以带反向代理的子路径），不要带 ? 或 # 之后的部分";
    expect(normalizeHubURL("https://hub.example.com/settings")).toEqual({ error: noPath });
    expect(normalizeHubURL("https://hub.example.com/persona/s/laptop/abc")).toEqual({ error: noPath });
    expect(normalizeHubURL("https://hub.example.com/?token=x")).toEqual({ error: noPath });
    expect(normalizeHubURL("https://hub.example.com/#token=x")).toEqual({ error: noPath });
    expect(normalizeHubURL("https://hub.example.com/a%20b")).toEqual({ error: noPath });
    expect(normalizeHubURL("https://")).toEqual({ error: "不是一个地址" });
  });
});

describe("hubTitle", () => {
  it("prefers the user's name, then the hub's, then the host", () => {
    expect(hubTitle({ url: "https://hub.example.com", name: "家里", label: "我的" })).toBe("我的");
    expect(hubTitle({ url: "https://hub.example.com", name: "家里" })).toBe("家里");
    expect(hubTitle({ url: "https://hub.example.com:8443" })).toBe("hub.example.com:8443");
  });
});

describe("handing the list over", () => {
  const list: SavedHub[] = [
    { url: "https://a.example.com", name: "A" },
    { url: "http://192.0.2.10:18660", label: "B 的" },
  ];
  it("round-trips through the fragment, and the link loses it", () => {
    expect(switchURL("https://b.example.com/x", list).startsWith("https://b.example.com/x/#hubs=")).toBe(true);
    const href = switchURL("https://b.example.com", list);
    expect(href.startsWith("https://b.example.com/#hubs=")).toBe(true);
    expect(href).not.toContain("token");
    expect(takeHubs(href)).toEqual({ hubs: list, rest: "/" });
  });
  it("keeps the rest of the link, a token next to it included (takeToken takes that one)", () => {
    const hash = new URLSearchParams({ hubs: JSON.stringify(list), token: "t0k" }).toString();
    expect(takeHubs(`https://b.example.com/s/m1/abc?tab=x#${hash}`)).toEqual({ hubs: list, rest: "/s/m1/abc?tab=x#token=t0k" });
  });
  it("leaves links without one alone", () => {
    expect(takeHubs("https://b.example.com/settings?tab=general")).toBeNull();
    expect(takeHubs("https://b.example.com/#token=t0k")).toBeNull();
  });
  it("drops what does not parse or is not an address, but still cleans the link", () => {
    expect(takeHubs("https://b.example.com/#hubs=%7Bnope")).toEqual({ hubs: [], rest: "/" });
    const odd = JSON.stringify([{ url: "javascript:alert(1)" }, { url: "https://c.example.com/settings" }, { name: "no url" }, "x", { url: "https://a.example.com", name: 7 }]);
    expect(takeHubs(`https://b.example.com/#hubs=${encodeURIComponent(odd)}`)).toEqual({ hubs: [{ url: "https://a.example.com" }], rest: "/" });
  });
  it("carries at most MAX_HUBS", () => {
    const many = Array.from({ length: MAX_HUBS + 5 }, (_, i) => ({ url: `https://h${i}.example.com` }));
    expect(takeHubs(switchURL("https://b.example.com", many))?.hubs).toHaveLength(MAX_HUBS);
  });
});

describe("mergeHubs", () => {
  it("keeps this browser's order and labels, fills in names, appends new hubs", () => {
    const local: SavedHub[] = [{ url: "https://a.example.com", label: "我的" }, { url: "https://b.example.com" }];
    const incoming: SavedHub[] = [{ url: "https://c.example.com", name: "C" }, { url: "https://a.example.com", name: "A", label: "别处的" }, { url: "https://b.example.com", name: "B" }];
    expect(mergeHubs(local, incoming)).toEqual([
      { url: "https://a.example.com", name: "A", label: "我的" },
      { url: "https://b.example.com", name: "B" },
      { url: "https://c.example.com", name: "C" },
    ]);
  });
});

describe("the stored list", () => {
  let store: Map<string, string>;
  beforeEach(() => {
    store = new Map();
    vi.stubGlobal("localStorage", {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => void store.set(k, v),
      removeItem: (k: string) => void store.delete(k),
    });
  });
  afterEach(() => vi.unstubAllGlobals());

  it("remembers the hub this page is on and refreshes its name", () => {
    rememberHub("https://a.example.com", "");
    expect(loadHubs()).toEqual([{ url: "https://a.example.com" }]);
    rememberHub("https://a.example.com:443", "家里");
    expect(loadHubs()).toEqual([{ url: "https://a.example.com", name: "家里" }]);
    rememberHub("https://a.example.com", ""); // a page that has not heard the name yet keeps it
    expect(loadHubs()).toEqual([{ url: "https://a.example.com", name: "家里" }]);
  });
  it("adds, renames and removes; refuses duplicates and bad addresses", () => {
    expect(addHub("b.example.com", "工作")).toEqual({ list: [{ url: "https://b.example.com", label: "工作" }] });
    expect(addHub("https://b.example.com:443/")).toEqual({ error: "这台 hub 已经在列表里" });
    expect(addHub("https://b.example.com/settings")).toEqual({ error: "只填 hub 的地址（可以带反向代理的子路径），不要带 ? 或 # 之后的部分" });
    // the same origin under a reverse proxy's sub-path is another hub
    expect(addHub("https://b.example.com/x")).toEqual({ list: [{ url: "https://b.example.com", label: "工作" }, { url: "https://b.example.com/x" }] });
    removeHub("https://b.example.com/x");
    rememberHub("https://b.example.com", "B");
    expect(renameHub("https://b.example.com", "")).toEqual([{ url: "https://b.example.com", name: "B" }]);
    expect(renameHub("https://b.example.com", " 公司 ")).toEqual([{ url: "https://b.example.com", name: "B", label: "公司" }]);
    expect(removeHub("https://b.example.com")).toEqual([]);
  });
  it("stops adding at MAX_HUBS", () => {
    for (let i = 0; i < MAX_HUBS; i++) expect("list" in addHub(`https://h${i}.example.com`)).toBe(true);
    expect(addHub("https://one-more.example.com")).toEqual({ error: `最多记 ${MAX_HUBS} 台 hub` });
  });
  it("merges a handed-over list into what it has", () => {
    rememberHub("https://b.example.com", "B");
    acceptHubs([{ url: "https://a.example.com", name: "A" }, { url: "https://b.example.com", label: "别处" }]);
    expect(loadHubs()).toEqual([{ url: "https://b.example.com", name: "B", label: "别处" }, { url: "https://a.example.com", name: "A" }]);
  });
  it("reads a broken stored value as an empty list", () => {
    store.set("persona.hubs", "{not json");
    expect(loadHubs()).toEqual([]);
  });
});
