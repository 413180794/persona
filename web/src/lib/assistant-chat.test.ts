import { describe, expect, it } from "vitest";
import { assistantInfoText, conversationKey, effectiveLauncher, NEW_CONVERSATION, pickedLauncher, pickLauncher, runsAssistant, shortModel } from "./assistant-chat";

describe("the assistant's launchers", () => {
  it("runs on Claude Code or Codex, not TRAEX or other agents", () => {
    expect(runsAssistant({ name: "claude" })).toBe(true);
    expect(runsAssistant({ name: "claude-fast", agent: "claude" })).toBe(true);
    expect(runsAssistant({ name: "codex", agent: "codex" })).toBe(true);
    expect(runsAssistant({ name: "traex", base_url: "(traex)" })).toBe(false);
    expect(runsAssistant({ name: "dsh", agent: "dsh" })).toBe(false);
  });

  it("follows 设置 on the main conversation, the conversation's own elsewhere, a pick over both", () => {
    expect(effectiveLauncher({ isMain: true, session: "old", config: "claude-fast" })).toBe("claude-fast");
    expect(effectiveLauncher({ isMain: true, picked: "codex", session: "old", config: "claude-fast" })).toBe("codex");
    expect(effectiveLauncher({ session: "old", config: "claude-fast" })).toBe("old");
    expect(effectiveLauncher({ picked: "codex", session: "old", config: "claude-fast" })).toBe("codex");
    expect(effectiveLauncher({ config: "claude-fast" })).toBe("claude-fast");
    expect(effectiveLauncher({})).toBe("claude");
    expect(effectiveLauncher({ isNew: true, session: "x", config: "c" })).toBe("c");
    expect(effectiveLauncher({ isNew: true, picked: "p", config: "c" })).toBe("p");
  });

  it("keeps a pick per conversation until it is cleared", () => {
    const k = conversationKey("hub", "s1");
    expect(pickedLauncher(k)).toBeUndefined();
    pickLauncher(k, "codex");
    pickLauncher(NEW_CONVERSATION, "claude-fast");
    expect(pickedLauncher(k)).toBe("codex");
    expect(pickedLauncher(conversationKey("hub", "s2"))).toBeUndefined();
    pickLauncher(k, undefined);
    expect(pickedLauncher(k)).toBeUndefined();
    expect(pickedLauncher(NEW_CONVERSATION)).toBe("claude-fast");
  });

  it("shortens a model to its last part", () => {
    expect(shortModel("vendor/model-x")).toBe("model-x");
    expect(shortModel(undefined)).toBe("");
  });
});

describe("拷贝信息", () => {
  it("says which conversation, where and how to go on with it", () => {
    const t = assistantInfoText({ title: "周报", isMain: true, sessionId: "s1", machine: "服务器", machineId: "srv", cwd: "~/.jarvis/assistant", launcher: "claude", model: "model-x" });
    expect(t.split("\n")).toEqual([
      "Persona 助手的对话 主对话 「周报」",
      "会话 ID：s1",
      "所在机器：服务器（srv）",
      "工作目录：~/.jarvis/assistant",
      "启动器：claude · 模型：model-x",
      "在那台机器上接着聊：cd ~/.jarvis/assistant && claude --resume s1",
    ]);
    const plain = assistantInfoText({ isMain: false, sessionId: "s2", machine: "srv", machineId: "srv", cwd: "/a", launcher: "codex" });
    expect(plain.split("\n").slice(0, 3)).toEqual(["Persona 助手的对话", "会话 ID：s2", "所在机器：srv"]);
    expect(plain).toContain("启动器：codex\n");
  });
});
