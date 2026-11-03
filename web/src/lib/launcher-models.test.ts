import { afterEach, describe, expect, it, vi } from "vitest";
import {
  defaultDetail, effortChoices, fitEffort, followedBinding, launcherAgent, launcherModels, modelDetail, modelRows, pickerLauncher, startPicks,
  type AgentModel, type LauncherModels,
} from "./launcher-models";

const all = ["low", "medium", "high", "xhigh", "max"];
const claude: LauncherModels = { agent: "claude", models: [
  { id: "default", name: "Default (recommended)", description: "Use the default model", resolved: "claude-opus-5-5", default: true, efforts: all },
  { id: "opus", name: "Opus 5.5", description: "Best for everyday complex work", resolved: "claude-opus-5-5", efforts: all },
  { id: "haiku", name: "Haiku 5.5", resolved: "claude-haiku-5-5" },
  { id: "gateway/glm-5.3", name: "glm-5.3", description: "From gateway", efforts: ["low", "medium", "high"] },
] };
const codex: LauncherModels = { agent: "codex", models: [
  { id: "gpt-5.5", name: "GPT-5.5", default: true, efforts: ["low", "medium", "high"], default_effort: "medium" },
  { id: "gpt-5.5-codex", name: "GPT-5.5-Codex", description: "Tuned for long agentic coding", efforts: ["low", "medium", "high"] },
] };
const ids = (rows: AgentModel[]) => rows.map((m) => m.id);

afterEach(() => vi.unstubAllGlobals());

describe("哪个启动器的列表", () => {
  it("the chosen launcher; following 模型配置, the one it runs", () => {
    expect(pickerLauncher("codex", {})).toBe("codex");
    expect(pickerLauncher("", followedBinding("", { provider: "launcher.claude", model: "m" }))).toBe("claude");
    expect(pickerLauncher("", followedBinding("", { provider: "legacy.ai" }))).toBe("");
    expect(pickerLauncher("", followedBinding("", undefined))).toBe("");
  });

  it("keeps 模型配置's model only while its launcher is the one that runs", () => {
    const effective = { provider: "launcher.claude", model: "model-chat", effort: "high" };
    expect(followedBinding("", effective)).toBe(effective);
    expect(followedBinding("claude", effective)).toBe(effective);
    expect(followedBinding("codex", effective)).toEqual({});
  });

  it("knows the agent from the list, else from the launcher list", () => {
    const launchers = [{ name: "claude" }, { name: "codex", agent: "codex" }, { name: "wrapped", agent_type: "traex" }];
    expect(launcherAgent("codex", launchers, null)).toBe("codex");
    expect(launcherAgent("wrapped", launchers)).toBe("traex");
    expect(launcherAgent("claude", launchers)).toBe("claude");
    expect(launcherAgent("unknown", launchers)).toBe("claude");
    expect(launcherAgent("claude", launchers, { agent: "codex", models: [] })).toBe("codex");
  });
});

describe("列表", () => {
  it("「默认」 says what it runs; rows add the id or concrete model only when the name doesn't say it", () => {
    expect(defaultDetail(claude.models, {})).toBe("claude-opus-5-5");
    expect(defaultDetail(codex.models, {})).toBe("gpt-5.5");
    expect(defaultDetail(claude.models, { provider: "launcher.claude", model: "model-chat" })).toBe("model-chat");
    expect(defaultDetail([{ id: "default", name: "Default", default: true }], {})).toBe("");
    expect(modelDetail(claude.models[1])).toBe("claude-opus-5-5");
    expect(modelDetail(claude.models[3])).toBe("gateway/glm-5.3");
    expect(modelDetail(codex.models[1])).toBe("");
  });

  it("puts 「默认」 first and searches name, id, concrete model and description by every word", () => {
    expect(modelRows(claude.models, "")).toEqual({ showDefault: true, rows: claude.models.slice(1) });
    expect(ids(modelRows(claude.models, "GATEWAY").rows)).toEqual(["gateway/glm-5.3"]);
    expect(ids(modelRows(claude.models, "claude 5-5").rows)).toEqual(["opus", "haiku"]);
    expect(modelRows(claude.models, "opus")).toMatchObject({ showDefault: true });
    expect(modelRows(claude.models, "haiku")).toMatchObject({ showDefault: false });
    expect(modelRows(claude.models, "默认")).toEqual({ showDefault: true, rows: [] });
    expect(modelRows(claude.models, "chat", { model: "model-chat" }).showDefault).toBe(true);
    expect(modelRows(codex.models, "agentic").rows.map((m) => m.name)).toEqual(["GPT-5.5-Codex"]);
  });

  it("asks the launcher (again with refresh); an older hub has no list", async () => {
    vi.stubGlobal("localStorage", { getItem: () => "mock" });
    const fetcher = vi.fn()
      .mockResolvedValueOnce({ ok: true, status: 200, text: async () => JSON.stringify({ agent: "traex" }) })
      .mockResolvedValueOnce({ ok: true, status: 200, text: async () => JSON.stringify(claude) })
      .mockResolvedValueOnce({ ok: false, status: 404, text: async () => "404 page not found" })
      .mockResolvedValueOnce({ ok: false, status: 502, text: async () => JSON.stringify({ error: "claude: command not found" }) });
    vi.stubGlobal("fetch", fetcher);
    expect(await launcherModels("desk top", "traex")).toEqual({ agent: "traex", models: [] });
    expect(await launcherModels("desktop", "gateway/x", { refresh: true })).toEqual(claude);
    expect(await launcherModels("desktop", "claude")).toBeNull();
    await expect(launcherModels("desktop", "claude")).rejects.toMatchObject({ status: 502, message: "claude: command not found" });
    expect(fetcher.mock.calls.map((c) => c[0]).slice(0, 2)).toEqual([
      "/api/v1/machines/desk%20top/launchers/traex/models", "/api/v1/machines/desktop/launchers/gateway%2Fx/models?refresh=1",
    ]);
  });
});

describe("思考强度", () => {
  it("offers the picked model's efforts, with none picked the one that runs, all without a list", () => {
    expect(effortChoices(claude, "")).toEqual(all);
    expect(effortChoices(claude, "gateway/glm-5.3")).toEqual(["low", "medium", "high"]);
    expect(effortChoices(claude, "haiku")).toEqual([]);
    expect(effortChoices(claude, "", { model: "haiku" })).toEqual([]);
    expect(effortChoices(claude, "", { model: "elsewhere" })).toEqual(all);
    // 模型配置's model listed only under another id (an alias): not known, so not the default entry's (none here).
    const plainDefault = { agent: "claude", models: [{ id: "default", name: "Default", default: true }, { id: "sonnet", name: "Sonnet", resolved: "claude-sonnet-5-5", efforts: ["low", "high"] }] };
    expect(effortChoices(plainDefault, "", { model: "claude-sonnet-5-5" })).toEqual(all);
    expect(effortChoices(plainDefault, "")).toEqual([]);
    expect(effortChoices(null, "")).toEqual(all);
    expect(effortChoices({ agent: "traex", models: [] }, "")).toEqual(all);
  });

  it("goes back to 跟随启动器 when the pick doesn't take it", () => {
    expect(fitEffort("max", effortChoices(claude, "gateway/glm-5.3"))).toBe("");
    expect(fitEffort("high", effortChoices(claude, "gateway/glm-5.3"))).toBe("high");
    expect(fitEffort("", all)).toBe("");
  });
});

describe("启动时发什么", () => {
  it("sends a picked model unless it is the default or the launcher fixes its own", () => {
    expect(startPicks({ agent: "claude", list: claude, model: "opus", effort: "" })).toEqual({ model: "opus" });
    expect(startPicks({ agent: "claude", list: claude, model: "default", effort: "" })).toEqual({});
    expect(startPicks({ agent: "claude", list: claude, model: "", effort: "" })).toEqual({});
    expect(startPicks({ agent: "claude", list: { ...claude, locked: "固定了模型" }, model: "opus", effort: "high" })).toEqual({ effort: "high" });
    expect(startPicks({ agent: "claude", list: null, model: "opus", effort: "" })).toEqual({});
  });

  it("sends an effort only to Claude Code and only one the model takes", () => {
    expect(startPicks({ agent: "claude", list: claude, model: "gateway/glm-5.3", effort: "high" })).toEqual({ model: "gateway/glm-5.3", effort: "high" });
    expect(startPicks({ agent: "claude", list: claude, model: "haiku", effort: "high" })).toEqual({ model: "haiku" });
    expect(startPicks({ agent: "claude", list: null, model: "", effort: "max" })).toEqual({ effort: "max" });
    expect(startPicks({ agent: "codex", list: codex, model: "gpt-5.5-codex", effort: "high" })).toEqual({ model: "gpt-5.5-codex" });
  });
});
