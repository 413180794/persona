import { describe, expect, it } from "vitest";
import { agentCommands, paneClaude } from "./composer-commands";
import { LOOP_TEXT } from "./loop-command";
import type { Session } from "./types";

const session = (o: Partial<Session>): Session => ({ machine_id: "m", session_id: "s", cwd: "/x", status: "idle", state: "idle", pane_id: "p1", ...o }) as Session;
const ALL = [{ text: "继续" }, { text: "/compact" }, { text: "/model " }, { text: LOOP_TEXT }, { text: "/clear" }];
const words = (s?: Session) => agentCommands(ALL, s).map((c) => c.text.trim());

// The "/" list by what the session can do (iOS canPickModel / canClear, Android canTypeClaudeCommands).
describe("agentCommands", () => {
  it("offers /model and /clear to an idle Claude in a pane", () => {
    expect(words(session({}))).toEqual(["继续", "/compact", "/model", LOOP_TEXT.trim(), "/clear"]);
  });
  it("keeps /clear away while it works or waits for you", () => {
    expect(words(session({ state: "working", status: "working" }))).toEqual(["继续", "/compact", "/model", LOOP_TEXT.trim()]);
    expect(words(session({ state: "needs_you", status: "blocked" }))).toEqual(["继续", "/compact", "/model", LOOP_TEXT.trim()]);
  });
  it("has neither for Codex, TRAEX, a session without a pane or a read-only source", () => {
    for (const s of [session({ agent: "codex" }), session({ agent: "traex" }), session({ pane_id: undefined }), session({ source_kind: "codex_app_server", capabilities: { send: true, terminal: true } })]) {
      const w = words(s);
      expect(w).not.toContain("/model");
      expect(w).not.toContain("/clear");
    }
  });
  it("drops /loop where the agent has none", () => {
    expect(words(session({ agent: "codex" }))).toEqual(["继续", "/compact"]);
  });
  it("has neither /model nor /clear for an offline Claude", () => {
    expect(words(session({ state: "offline", status: "offline" }))).not.toContain("/model");
  });
});

describe("paneClaude", () => {
  it("is Claude in a herdr pane that takes messages", () => {
    expect(paneClaude(session({}))).toBe(true);
    expect(paneClaude(session({ agent: "claude" }))).toBe(true);
    expect(paneClaude(session({ assistant: true }))).toBe(false);
    expect(paneClaude(undefined)).toBe(false);
  });
});
