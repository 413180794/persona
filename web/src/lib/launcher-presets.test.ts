import { afterEach, describe, expect, it, vi } from "vitest";
import { launcherNameProblem, loadPresets, presetBody, presetChoices, presetDefaultModel, presetSubtitle, type LauncherPreset } from "./launcher-presets";

const team: LauncherPreset = {
  id: "example-gateway/team", plugin: "example-gateway", name: "示例网关 · 团队", kind: "launcher", base_url: "https://gateway.example.com/v1",
  model: "example-pro", models: [{ id: "example-mini", name: "Example Mini" }],
};
const api: LauncherPreset = { id: "example-gateway/api", plugin: "example-gateway", name: "示例 API", kind: "api", api: "responses", base_url: "not a url", models: [{ id: "m1" }, { id: "m2" }] };

afterEach(() => vi.unstubAllGlobals());

describe("launcher presets", () => {
  it("defaults to the declared model, else the first listed", () => {
    expect(presetDefaultModel(team)).toBe("example-pro");
    expect(presetDefaultModel(api)).toBe("m1");
    expect(presetDefaultModel({ ...api, models: [] })).toBe("");
  });

  it("offers the default first when it is not listed", () => {
    expect(presetChoices(team).map((m) => m.id)).toEqual(["example-pro", "example-mini"]);
    expect(presetChoices(api).map((m) => m.id)).toEqual(["m1", "m2"]);
  });

  it("reads the kind and the host", () => {
    expect(presetSubtitle(team)).toBe("Claude Code 启动器 · gateway.example.com");
    expect(presetSubtitle(api)).toBe("直接 API");
  });

  it("checks names like the hub", () => {
    expect(launcherNameProblem("")).toBe("");
    expect(launcherNameProblem("jv_team")).toBe("");
    expect(launcherNameProblem("9x")).toMatch(/字母/);
    expect(launcherNameProblem("presets")).toBe("不能叫 presets");
  });

  it("sends the model only when another one was picked", () => {
    expect(presetBody(team, " jv_team ", " sk-x ", "example-pro")).toEqual({ name: "jv_team", api_key: "sk-x", preset: "example-gateway/team" });
    expect(presetBody(team, "jv_team", "k", "example-mini")).toEqual({ name: "jv_team", api_key: "k", preset: "example-gateway/team", model: "example-mini" });
  });

  it("an older hub without presets answers none", async () => {
    vi.stubGlobal("localStorage", { getItem: () => "jd_test" });
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ error: "not found" }), { status: 404, headers: { "Content-Type": "application/json" } })));
    expect(await loadPresets()).toEqual([]);
  });
});
