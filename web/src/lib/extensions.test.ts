import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./api";
import {
  clearExtensionSecret, compact, configError, fieldTitle, isDirty, listItems, listExtensions, listSummary, numberProblems, placeholder, extensionDetail,
  machineStatus, extensionState, extensionTone, readNumber, runNote, same, secretState, setAt, setExtensionConfig, setExtensionEnabled, setExtensionSecret, sourceText,
  stateLine, valueAt, morePermissions, extensionUpgrades, setExtensionAutoUpgrade, setExtensionUpgrades, upgradeAction, upgradeDetail, upgradeNote,
  type HubExtension, type ExtensionField, type ExtensionUpgrade,
} from "./extensions";

const extension = (status: string, extra: Partial<HubExtension> = {}): HubExtension => ({ id: `p-${status}`, name: status, kind: "connectors", status, ...extra });

// The hub's features as the extension routes read them (lib/extension-api.ts): a hub with the new routes, unless a test
// says otherwise.
const hub = vi.hoisted(() => ({ features: ["extensions"] as string[] }));
vi.mock("./store", async (importOriginal) => {
  const m = await importOriginal<typeof import("./store")>();
  return { ...m, getState: () => ({ ...m.getState(), features: hub.features }) };
});

afterEach(() => {
  vi.unstubAllGlobals();
  hub.features = ["extensions"];
});

describe("扩展的文字", () => {
  it("says each state, its tone, and the list's summary", () => {
    // Running or idle until something needs it: both are just on to the user.
    expect(["running", "stopped", "disabled", "starting", "needs_setup"].map((s) => extensionState(extension(s)))).toEqual([
      "启动", "启动", "停用", "正在启动", "需要设置",
    ]);
    expect(extensionState(extension("weird"))).toBe("状态未知");
    expect(["running", "needs_setup", "error", "incompatible", "disabled", "stopped"].map((s) => extensionTone(extension(s)))).toEqual([
      "green", "orange", "red", "red", "idle", "green",
    ]);
    expect(["running", "stopped", "disabled", "error"].map((s) => runNote(extension(s)))).toEqual(["正在运行", "暂未运行，用到时自动启动", "", ""]);
    expect(extensionTone(null)).toBe("idle");
    expect(listSummary(null)).toBe("");
    expect(listSummary([])).toBe("没有");
    expect(listSummary([extension("running"), extension("needs_setup"), extension("error"), extension("disabled")])).toBe("4 个 · 2 个要处理");
    expect(listSummary([extension("running")])).toBe("1 个");
  });
  it("puts the state, version and source on one line", () => {
    expect(stateLine(extension("running", { version: "1.2.0", source: "installed" }))).toBe("启动 · 1.2.0 · 已安装");
    expect(stateLine(extension("running", { source: "hub.json" }))).toBe("启动 · hub.json");
    expect(sourceText("other")).toBeUndefined();
  });
  it("marks required fields without a default, and shows the default as a placeholder", () => {
    expect(fieldTitle({ name: "url", type: "string", required: true })).toBe("url（必填）");
    expect(fieldTitle({ name: "url", type: "string", title: "地址", required: true, default: "https://x" })).toBe("地址");
    expect(placeholder({ name: "n", type: "integer", default: 30 })).toBe("默认：30");
    expect(placeholder({ name: "m", type: "string", default: "safe", enum: [{ value: "safe", title: "全部核对" }] })).toBe("默认：全部核对");
    expect(placeholder({ name: "s", type: "string" })).toBe("未设置");
    expect(secretState({ name: "k", title: "K", set: false, required: true })).toBe("未设置（必填）");
    expect(secretState({ name: "k", title: "K", set: true, required: true })).toBe("已设置");
  });
});

describe("草稿", () => {
  it("reads and sets values by path, and drops a group left empty", () => {
    const d = setAt(setAt({}, ["proxy", "host"], "h.example.com"), ["proxy", "port"], 8080);
    expect(d).toEqual({ proxy: { host: "h.example.com", port: 8080 } });
    expect(valueAt(d, ["proxy", "port"])).toBe(8080);
    expect(valueAt(d, ["proxy", "host", "x"])).toBeUndefined();
    expect(valueAt({ a: null }, ["a"])).toBeUndefined();
    const less = setAt(setAt(d, ["proxy", "host"], undefined), ["proxy", "port"], undefined);
    expect(less).toEqual({});
    expect(d).toEqual({ proxy: { host: "h.example.com", port: 8080 } }); // not mutated
  });
  it("compacts what the user cleared, and compares what would be sent", () => {
    expect(compact({ a: "", b: [], c: { d: "" }, e: false, f: 0, g: ["x"], h: null })).toEqual({ e: false, f: 0, g: ["x"] });
    const config = { folders: ["工作"], mode: "safe" };
    expect(isDirty({ mode: "safe", folders: ["工作"] }, config)).toBe(false);
    expect(isDirty({ mode: "safe", folders: ["工作"], base_url: "" }, config)).toBe(false);
    expect(isDirty({ mode: "fast", folders: ["工作"] }, config)).toBe(true);
    expect(isDirty({}, undefined)).toBe(false);
    expect(same([1, { a: 2 }], [1, { a: 2 }])).toBe(true);
    expect(same([1], { 0: 1 })).toBe(false);
  });
  it("reads numbers as typed", () => {
    expect(readNumber(" 30 ", true)).toEqual({ value: 30 });
    expect(readNumber("", true)).toEqual({});
    expect(readNumber("1.5", true)).toEqual({ error: "要是一个整数" });
    expect(readNumber("1.5", false)).toEqual({ value: 1.5 });
    expect(readNumber("abc", false)).toEqual({ error: "要是一个数" });
    expect(numberProblems({ every: { text: "x", integer: true }, port: { text: "80", integer: true } })).toEqual({ every: "要是一个数" });
  });
  it("lists a list field's items, falling back to the default", () => {
    const f: ExtensionField = { name: "tags", type: "array", default: ["a"] };
    expect(listItems({}, ["tags"], f)).toEqual(["a"]);
    expect(listItems({ tags: ["b", 3] }, ["tags"], f)).toEqual(["b"]);
  });
});

describe("扩展的接口", () => {
  it("calls the hub's reserved paths", async () => {
    const calls: { url: string; method: string; body?: string }[] = [];
    vi.stubGlobal("localStorage", { getItem: () => "t", setItem: () => {}, removeItem: () => {} });
    vi.stubGlobal("fetch", async (url: string, init: RequestInit) => {
      calls.push({ url, method: init.method ?? "GET", body: init.body as string | undefined });
      return new Response(JSON.stringify(url.endsWith("/extensions") ? { extensions: [extension("running")] } : { id: "a b", name: "x", kind: "k", status: "running" }));
    });
    expect((await listExtensions()).map((p) => p.status)).toEqual(["running"]);
    await extensionDetail("a b");
    await setExtensionEnabled("a b", false);
    await setExtensionConfig("a b", { x: 1 });
    await setExtensionSecret("a b", "api_key", "v");
    await clearExtensionSecret("a b", "api_key");
    expect(calls.map((c) => `${c.method} ${c.url} ${c.body ?? ""}`.trim())).toEqual([
      "GET /api/v1/extensions",
      "GET /api/v1/extensions/a%20b/-/",
      'PUT /api/v1/extensions/a%20b/-/enabled {"enabled":false}',
      'PUT /api/v1/extensions/a%20b/-/config {"config":{"x":1}}',
      'PUT /api/v1/extensions/a%20b/-/secrets/api_key {"value":"v"}',
      "DELETE /api/v1/extensions/a%20b/-/secrets/api_key",
    ]);
  });
  it("calls a hub from before #1844 on its old routes, and reads its old keys", async () => {
    hub.features = ["plugins", "plugin_manager"];
    const calls: string[] = [];
    vi.stubGlobal("localStorage", { getItem: () => "t", setItem: () => {}, removeItem: () => {} });
    vi.stubGlobal("fetch", async (url: string, init: RequestInit) => {
      calls.push(`${init.method ?? "GET"} ${url}`);
      if (url.endsWith("/plugins")) return new Response(JSON.stringify({ plugins: [extension("running")], can_install: true }));
      return new Response(JSON.stringify({ id: "a", name: "x", kind: "k", status: "running", ui: [{ plugin: "a", plugin_name: "A", id: "usage", title: "用量", placement: "plugin_page" }] }));
    });
    expect((await listExtensions()).map((p) => p.status)).toEqual(["running"]);
    const d = await extensionDetail("a");
    expect(d.ui).toEqual([{ extension: "a", extension_name: "A", id: "usage", title: "用量", placement: "plugin_page" }]);
    expect(calls).toEqual(["GET /api/v1/plugins", "GET /api/v1/plugins/a/-/"]);
  });
  it("reads the field the hub refused", () => {
    expect(configError(new ApiError(400, "base_url: 要是一个网址", { code: "invalid_config", field: "base_url", message: "要是一个网址" }))).toEqual({
      field: "base_url", message: "要是一个网址",
    });
    expect(configError(new ApiError(400, "bad", { code: "invalid_config" }))).toEqual({ field: "", message: "bad" });
    expect(configError(new ApiError(500, "x", {}))).toBeNull();
    expect(configError(new Error("x"))).toBeNull();
  });
});

describe("an extension's machine switches", () => {
  const mac = { id: "macbook", name: "MacBook", online: true, os: "darwin" };
  it("say online or not, and for a computer extension how Cua Driver is", () => {
    const note = "…";
    expect(machineStatus({ ...mac, online: false, computer: { enabled: true, installed: false, note } }, true)).toBe("离线");
    expect(machineStatus({ ...mac, computer: { enabled: false, installed: false, note } }, true)).toBe("在线 · Cua 没装");
    expect(machineStatus({ ...mac, computer: { enabled: true, installed: true, daemon: false, note } }, true)).toBe("在线 · Cua 没在运行");
    expect(machineStatus({ ...mac, computer: { enabled: true, installed: true, daemon: true, screen_recording: "missing", note } }, true)).toBe("在线 · Cua 缺权限");
    expect(machineStatus({ ...mac, computer: { enabled: true, installed: true, daemon: true, ready: true } }, true)).toBe("在线 · Cua 可以用");
    expect(machineStatus({ ...mac, computer: { enabled: false } }, true)).toBe("在线");
    expect(machineStatus({ ...mac, computer: { enabled: true, ready: true } }, false)).toBe("在线");
  });
});

describe("auto-upgrade (docs/extensions.md「自动升级」)", () => {
  const up = (state: string, extra: Partial<ExtensionUpgrade> = {}): ExtensionUpgrade => ({ id: "p", market: "m1", from: "1.0.0", to: "1.1.0", state, ...extra });

  it("says what happens to a newer version, and what the user does about it", () => {
    const ask = up("needs_permission", { added_permissions: [{ key: "network", title: "访问更多网络", detail: "status.example.com" }] });
    expect(upgradeDetail(ask)).toBe("要多一项权限：访问更多网络（status.example.com）");
    expect(upgradeAction(ask)).toBe("去确认");
    expect(upgradeDetail(up("waiting_idle"))).toBe("扩展空闲时自动升级");
    expect(upgradeAction(up("waiting_idle"))).toBeNull();
    expect([upgradeDetail(up("off")), upgradeAction(up("off"))]).toEqual(["自动升级关着", "去升级"]);
    expect(upgradeDetail(up("skipped"))).toBe("上次升级后起不来，已回到 1.0.0，不再自动升级到这个版本");
    expect(upgradeDetail(up("failed", { detail: "下载失败" }))).toBe("没装上：下载失败");
    expect(morePermissions([])).toBe("要的权限有变化");
    expect(morePermissions([{ key: "a", title: "甲" }, { key: "b", title: "乙", detail: "x" }])).toBe("要多两项权限：甲、乙（x）");
  });

  it("tells of the last upgrade only when it is news of its own", () => {
    const at = "2026-11-04T14:34:21.439310005Z";
    expect(upgradeNote({ kind: "upgraded", from: "0.9.0", to: "1.0.0", at })).toBe("已自动升级到 1.0.0（原来是 0.9.0）");
    expect(upgradeNote({ kind: "rolled_back", from: "1.0.0", to: "1.1.0", at })).toBe("1.1.0 起不来，已回到 1.0.0");
    expect(upgradeNote({ kind: "needs_permission", from: "1.0.0", to: "1.1.0", at })).toBeNull();
  });

  it("puts the switches to the hub; a hub without the route has no global switch", async () => {
    const calls: { url: string; init?: RequestInit }[] = [];
    let has = false;
    vi.stubGlobal("localStorage", { getItem: () => "t", setItem: () => {}, removeItem: () => {} });
    vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url, init });
      if (url.endsWith("/extensions/-/upgrades")) {
        if (!has) return new Response(JSON.stringify({ error: "no such extension", code: "not_found" }), { status: 404, headers: { "Content-Type": "application/json" } });
        return new Response(JSON.stringify({ auto: init?.method === "PUT" ? JSON.parse(String(init.body)).auto : true, upgrades: [] }), { headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ id: "p", name: "P", kind: "ui", status: "running", auto_upgrade: false }), { headers: { "Content-Type": "application/json" } });
    }));
    expect(await extensionUpgrades()).toBeNull();
    has = true;
    expect(await extensionUpgrades()).toBe(true);
    expect(await setExtensionUpgrades(false)).toBe(false);
    expect((await setExtensionAutoUpgrade("p", false)).auto_upgrade).toBe(false);
    const last = calls[calls.length - 1];
    expect(last.url).toContain("/extensions/p/-/auto-upgrade");
    expect(last.init?.method).toBe("PUT");
    expect(JSON.parse(String(last.init?.body))).toEqual({ auto: false });
  });
});
