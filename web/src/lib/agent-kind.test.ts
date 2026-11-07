import { describe, expect, it } from "vitest";
import { agentId, agentLetter, agentName, isMarked, MARKED_AGENTS, sessionAgent } from "./agent-kind";

describe("agentId", () => {
  it("reads nothing as Claude Code and folds the other names of a program into one", () => {
    expect(["", undefined, null, "claude", "Claude-Code", " claudecode "].map(agentId)).toEqual(Array(6).fill("claude"));
    expect(["codex", "Codex-CLI"].map(agentId)).toEqual(["codex", "codex"]);
    expect(["gemini-cli", "cursor-agent", "pi-web", "OpenCode"].map(agentId)).toEqual(["gemini", "cursor", "pi", "opencode"]);
  });

  it("keeps any other agent as it is", () => {
    expect(["traex", "dsh", "amp"].map(agentId)).toEqual(["traex", "dsh", "amp"]);
    expect(["traex", "dsh", "amp"].some(isMarked)).toBe(false);
  });
});

describe("agentName", () => {
  it("names the known agents by their product", () => {
    expect(MARKED_AGENTS.map((a) => agentName(a))).toEqual(["Claude Code", "Codex", "OpenCode", "Pi", "Gemini CLI", "Cursor", "TRAE"]);
    expect(agentName("traex")).toBe("TRAEX");
    expect(agentName("")).toBe("Claude Code");
  });

  it("calls an agent the app does not know by its runtime's label, else as reported", () => {
    expect(agentName("dsh", "DSH")).toBe("DSH");
    expect(agentName("amp")).toBe("amp");
  });
});

describe("sessionAgent", () => {
  it("is Codex for a thread read off the app-server or its history, whatever it reports", () => {
    expect(sessionAgent({ source_kind: "codex_app_server" }).agent).toBe("codex");
    expect(sessionAgent({ agent: "claude", source_kind: "codex_history" }).agent).toBe("codex");
    expect(sessionAgent({ agent: "opencode", agent_label: "x" })).toEqual({ agent: "opencode", label: "x", surface: undefined });
    expect(sessionAgent({ agent: "claude", agent_surface: "cli" }).surface).toBe("cli");
    expect(sessionAgent({ agent: "codex", agent_surface: "desktop" }).surface).toBe("desktop");
    expect(sessionAgent({ agent: "codex", agent_surface: "sdk" }).surface).toBeUndefined();
  });
});

describe("agentLetter", () => {
  it("is the first letter or digit, upper case", () => {
    expect(["dsh", "  amp", "-x", "3d", "小车"].map(agentLetter)).toEqual(["D", "A", "X", "3", "小"]);
    expect(agentLetter("")).toBe("?");
  });
});
