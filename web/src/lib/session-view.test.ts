import { describe, expect, it } from "vitest";
import { canShowTerminal, otherView, readView, rememberView, resolveView, terminalTarget } from "./session-view";
import type { Session } from "./types";

function memory() {
  const m = new Map<string, string>();
  return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v) };
}

function agent(o: Partial<Session> = {}): Session {
  return { machine_id: "m1", session_id: "a1", title: "t", cwd: "/code", status: "idle", state: "idle", ...o } as Session;
}

describe("which view an agent's page draws", () => {
  it("takes the address's, then the one remembered, then the conversation", () => {
    expect(resolveView("term", "chat", true)).toBe("term");
    expect(resolveView(undefined, "term", true)).toBe("term");
    expect(resolveView(undefined, undefined, true)).toBe("chat");
  });
  it("is the conversation when there is no terminal to show, whatever was asked", () => {
    expect(resolveView("term", "term", false)).toBe("chat");
  });
  it("flips", () => {
    expect(otherView("term")).toBe("chat");
    expect(otherView("chat")).toBe("term");
  });
});

describe("the view remembered per agent", () => {
  it("is kept for each agent apart", () => {
    const s = memory();
    rememberView("m1", "a1", "term", s);
    rememberView("m1", "a2", "chat", s);
    expect(readView("m1", "a1", s)).toBe("term");
    expect(readView("m1", "a2", s)).toBe("chat");
    expect(readView("m2", "a1", s)).toBeUndefined();
    rememberView("m1", "a1", "chat", s);
    expect(readView("m1", "a1", s)).toBe("chat");
  });
  it("ignores a store that holds garbage", () => {
    const s = memory();
    s.setItem("persona.sessionView", "{not json");
    expect(readView("m1", "a1", s)).toBeUndefined();
    s.setItem("persona.sessionView", JSON.stringify({ "m1/a1": "shell", "m1/a2": "term" }));
    expect(readView("m1", "a1", s)).toBeUndefined();
    expect(readView("m1", "a2", s)).toBe("term");
  });
  it("lets the oldest go past 300 agents", () => {
    const s = memory();
    for (let i = 0; i < 305; i++) rememberView("m", `a${i}`, "term", s);
    expect(readView("m", "a0", s)).toBeUndefined();
    expect(readView("m", "a304", s)).toBe("term");
  });
  it("survives a store that refuses writes", () => {
    expect(() => rememberView("m", "a", "term", { getItem: () => null, setItem: () => { throw new Error("full"); } })).not.toThrow();
  });
});

describe("where a terminal from a list of panes opens", () => {
  it("an agent's pane: its own page, in the terminal view", () => {
    expect(terminalTarget("m1", "a1", [agent()])).toEqual({ name: "session", mid: "m1", sid: "a1", view: "term" });
    expect(terminalTarget("m1", "a1", [agent({ space_id: "w1", slot_id: "w1:t1" })])).toEqual({ name: "tab", mid: "m1", space: "w1", slot: "w1:t1", sid: "a1", view: "term" });
  });
  it("a plain shell, or an agent it does not know: the 终端 page", () => {
    expect(terminalTarget("m1", "pane:w1:p2", [agent()])).toEqual({ name: "terminal", mid: "m1", id: "pane:w1:p2" });
    expect(terminalTarget("m1", "zzz", [agent()])).toEqual({ name: "terminal", mid: "m1", id: "zzz" });
  });
  it("an agent with no terminal to open stays on the 终端 page", () => {
    expect(terminalTarget("m1", "a1", [agent({ state: "offline" })])).toEqual({ name: "terminal", mid: "m1", id: "a1" });
  });
});

describe("whether a terminal can be shown", () => {
  it("needs a live pane its source lets a terminal open for", () => {
    expect(canShowTerminal(agent())).toBe(true);
    expect(canShowTerminal(agent({ state: "offline" }))).toBe(false);
    expect(canShowTerminal(agent({ source_kind: "codex_history", capabilities: {} as never }))).toBe(false);
    expect(canShowTerminal(undefined)).toBe(false);
  });
});
