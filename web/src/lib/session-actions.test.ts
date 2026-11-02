import { describe, expect, it } from "vitest";
import { effortTitle, hasModes, isMuted, modeTitle, renameBody, SWITCHABLE_MODES, toggleMute } from "./session-actions";
import type { Session } from "./types";

const s = (over: Partial<Session> = {}): Session => ({ machine_id: "mac", session_id: "s1", title: "修登录", cwd: "/home/me/app", status: "idle", space_name: "app", slot_id: "p1", slot_name: "login", ...over });
const none = { spaces: [] as string[], sessions: [] as string[], allowed: [] as string[] };

describe("mutes", () => {
  it("mutes and unmutes an agent on its own", () => {
    const on = toggleMute(s(), none);
    expect(on.sessions).toEqual(["mac/s1"]);
    expect(isMuted(s(), on)).toBe(true);
    const off = toggleMute(s(), on);
    expect(off.sessions).toEqual([]);
    expect(isMuted(s(), off)).toBe(false);
  });

  it("lets one agent of a muted space notify again, and mutes it back", () => {
    const space = { ...none, spaces: ["mac/app"] };
    expect(isMuted(s(), space)).toBe(true);
    const allowed = toggleMute(s(), space);
    expect(allowed.allowed).toEqual(["mac/s1"]);
    expect(isMuted(s(), allowed)).toBe(false);
    const back = toggleMute(s(), allowed);
    expect(back.allowed).toEqual([]);
    expect(back.sessions).toEqual([]);
    expect(isMuted(s(), back)).toBe(true);
  });

  it("keeps everything else as it was", () => {
    const m = { spaces: ["mac/other"], sessions: ["mac/s9"], allowed: ["mac/s8"] };
    const on = toggleMute(s(), m);
    expect(on).toEqual({ spaces: ["mac/other"], sessions: ["mac/s9", "mac/s1"], allowed: ["mac/s8"] });
  });
});

describe("modes", () => {
  it("labels the known ones and keeps unknown ids", () => {
    expect(modeTitle("plan")).toBe("只做计划");
    expect(modeTitle("weird")).toBe("weird");
    expect(modeTitle(undefined)).toBe("");
    expect(effortTitle("xhigh")).toBe("很高");
  });

  it("never offers 全部放行 on a running session", () => {
    expect(SWITCHABLE_MODES.some((m) => m.id === "bypassPermissions")).toBe(false);
  });

  it("only Claude Code agents have the chips", () => {
    expect(hasModes(s())).toBe(true);
    expect(hasModes(s({ agent: "claude" }))).toBe(true);
    expect(hasModes(s({ agent: "codex" }))).toBe(false);
    expect(hasModes(s({ assistant: true }))).toBe(false);
    expect(hasModes(s({ source_kind: "runtime" }))).toBe(false);
    expect(hasModes(undefined)).toBe(false);
  });
});

describe("rename", () => {
  it("sends only what changed", () => {
    expect(renameBody(s(), "login", "修登录")).toBeNull();
    expect(renameBody(s(), "auth", "修登录")).toEqual({ tab_name: "auth" });
    expect(renameBody(s(), "login", "修登录页")).toEqual({ title: "修登录页" });
    expect(renameBody(s(), " auth ", " 新名 ")).toEqual({ tab_name: "auth", title: "新名" });
  });

  it("never renames a tab the agent has not got", () => {
    expect(renameBody(s({ slot_id: undefined }), "auth", "x")).toEqual({ title: "x" });
    expect(renameBody(s({ assistant: true }), "auth", "x")).toEqual({ title: "x" });
  });

  it("ignores empty names", () => {
    expect(renameBody(s(), "", "")).toBeNull();
  });
});
