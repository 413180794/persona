import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./api";
import { actUI, blocks, fieldValues, isHttps, isUnsupported, numberProblem, outcome, percent, renderUI, submitValues, toneOf, uiPlaces, type UIView } from "./plugin-ui";

afterEach(() => vi.unstubAllGlobals());

const view: UIView = {
  title: "概况",
  state: "tab=1",
  sections: [
    {
      items: [
        { type: "metric", label: "余额", value: "¥ 1", tone: "positive" },
        { type: "metric", label: "已用", value: "¥ 2" },
        { type: "progress", label: "今日", ratio: 0.5 },
        { type: "button", title: "刷新", action: { id: "refresh" } },
        { type: "button", title: "停用", role: "destructive", action: { id: "off", confirm: "确定？" } },
        { type: "divider" },
        { type: "row", title: "Key" },
      ],
    },
    {
      items: [
        { type: "field", name: "note", label: "备注", kind: "text", value: "hi" },
        { type: "field", name: "token", label: "Token", kind: "secret", value: "never" },
        { type: "field", name: "auto", label: "提醒", kind: "toggle", value: "true" },
        { type: "field", name: "off", label: "关", kind: "toggle" },
        { type: "field", name: "plan", label: "套餐", kind: "choice", choices: [{ value: "solo" }, { value: "team" }] },
        { type: "field", name: "limit", label: "上限", kind: "number", value: "20" },
        { type: "text", text: "x" },
      ],
    },
  ],
};

describe("画一页", () => {
  it("maps tones, ratios and links", () => {
    expect(["positive", "caution", "danger", "neutral", "pink", undefined].map(toneOf)).toEqual(["positive", "caution", "danger", "neutral", "neutral", "neutral"]);
    expect([0.72, -1, 3, undefined, Number.NaN, 0.333].map(percent)).toEqual([72, 0, 100, 0, 0, 33.3]);
    expect(isHttps("https://example.com/a")).toBe(true);
    expect(isHttps("http://example.com")).toBe(false);
    expect(isHttps("javascript:alert(1)")).toBe(false);
    expect(isHttps(undefined)).toBe(false);
  });
  it("lays metrics side by side and buttons in a row", () => {
    expect(blocks(view.sections[0].items).map((b) => (b.kind === "item" ? b.item.type : `${b.kind}:${b.items.length}`))).toEqual([
      "metrics:2", "progress", "buttons:2", "divider", "row",
    ]);
    expect(blocks([])).toEqual([]);
  });
  it("starts the fields as the plugin filled them in, never a secret", () => {
    expect(fieldValues(view)).toEqual({ note: "hi", token: "", auto: "true", off: "false", plan: "solo", limit: "20" });
    expect(fieldValues(null)).toEqual({});
  });
  it("submits every field, typed or not", () => {
    expect(submitValues(view, { note: "new", token: "s", extra: "no" })).toEqual({ note: "new", token: "s", auto: "true", off: "false", plan: "solo", limit: "20" });
    expect(numberProblem("12.5")).toBe("");
    expect(numberProblem("")).toBe("");
    expect(numberProblem("abc")).toBe("要是一个数");
  });
  it("reads what a press answered", () => {
    expect(outcome({})).toEqual({ view: undefined, reload: true, toast: undefined, url: undefined });
    expect(outcome({ toast: "已刷新" })).toMatchObject({ reload: false, toast: "已刷新" });
    expect(outcome({ view })).toMatchObject({ view, reload: false });
    expect(outcome({ open_url: "https://example.com/keys" })).toMatchObject({ reload: false, url: "https://example.com/keys" });
    expect(outcome({ open_url: "http://example.com/keys" })).toMatchObject({ reload: false, url: undefined });
  });
  it("hides a section the plugin says is not its gateway (404 unsupported), and only that", () => {
    expect(isUnsupported(new ApiError(404, "not my gateway", { code: "unsupported" }))).toBe(true);
    expect(isUnsupported(new ApiError(404, "no such plugin", { code: "not_found" }))).toBe(false);
    expect(isUnsupported(new ApiError(502, "x", { code: "unsupported" }))).toBe(false);
    expect(isUnsupported(new Error("unsupported"))).toBe(false);
  });
});

describe("声明式 UI 的接口", () => {
  it("lists places, draws and presses", async () => {
    const calls: string[] = [];
    vi.stubGlobal("localStorage", { getItem: () => "t", setItem: () => {}, removeItem: () => {} });
    vi.stubGlobal("fetch", async (url: string, init: RequestInit) => {
      calls.push(`${init.method ?? "GET"} ${url} ${(init.body as string) ?? ""}`.trim());
      if (url.includes("unsupported")) return new Response(JSON.stringify({ error: "not my gateway", code: "unsupported" }), { status: 404 });
      return new Response(JSON.stringify(url.includes("placement") ? { ui: [{ plugin: "p", id: "panel", title: "面板", placement: "launcher_section" }] } : { view, toast: "ok" }));
    });
    expect((await uiPlaces("launcher_section", "work cc", "mac 1")).map((u) => u.id)).toEqual(["panel"]);
    await uiPlaces("plugin_page");
    expect((await renderUI("a b", "home")).title).toBe("概况");
    await renderUI("p", "panel", { launcher: "work" }, "tab=1");
    expect((await actUI("p", "home", "save", { state: "tab=1", values: { note: "x" } })).toast).toBe("ok");
    await actUI("p", "home", "refresh");
    const err = await renderUI("p", "unsupported", { launcher: "other" }).catch((e) => e);
    expect(isUnsupported(err)).toBe(true);
    expect(calls).toEqual([
      "GET /api/v1/plugins/-/ui?placement=launcher_section&launcher=work+cc&machine=mac+1",
      "GET /api/v1/plugins/-/ui?placement=plugin_page",
      "POST /api/v1/plugins/a%20b/-/ui/home {}",
      'POST /api/v1/plugins/p/-/ui/panel {"context":{"launcher":"work"},"state":"tab=1"}',
      'POST /api/v1/plugins/p/-/ui/home {"state":"tab=1","action":"save","values":{"note":"x"}}',
      'POST /api/v1/plugins/p/-/ui/home {"action":"refresh"}',
      'POST /api/v1/plugins/p/-/ui/unsupported {"context":{"launcher":"other"}}',
    ]);
  });
});
