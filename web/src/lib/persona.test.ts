import { describe, expect, it } from "vitest";
import { avatarFile, dotFile, isBuiltinPersona, personaFileURL, personaProblem, personaState, usablePersonas, withChosenReady, type PersonaInfo } from "./persona";

const pack = (patch: Partial<PersonaInfo["manifest"]> = {}, id = "doudou"): PersonaInfo => ({
  id,
  rev: "abc123",
  bytes: 1000,
  manifest: {
    format: 1,
    id,
    name: "豆豆",
    avatar: { sizes: { "64": "avatar/64.png", "256": "avatar/256.png", "128": "avatar/128.png" } },
    dot: { size: 144, states: { idle: { file: "dot/idle.png", fps: 8 }, working: { file: "dot/working.png" } } },
    ...patch,
  },
});

describe("persona packages", () => {
  it("takes what the hub takes", () => {
    expect(personaProblem(pack())).toBeNull();
    expect(personaProblem(pack({ format: 2 }))).toBe("format 2");
    expect(personaProblem(pack({}, "jarvis"))).toMatch(/^id/);
    expect(personaProblem(pack({}, "jarvis-circuit"))).toMatch(/^id/);
    expect(personaProblem(pack({}, "persona"))).toMatch(/^id/);
    expect(isBuiltinPersona("persona") && isBuiltinPersona("jarvis") && !isBuiltinPersona("doudou")).toBe(true);
    expect(personaProblem({ ...pack(), id: "other" })).toMatch(/^id/);
    expect(personaProblem(pack({ id: "Bad_ID" }, "Bad_ID"))).toMatch(/^id/);
    expect(personaProblem(pack({ name: " " }))).toBe("no name");
    expect(personaProblem(pack({ avatar: { sizes: {} } }))).toBe("no avatar");
    expect(personaProblem(pack({ dot: { states: { working: { file: "dot/w.png" } } } }))).toBe("no idle dot");
    expect(personaProblem(pack({ dot: { states: { idle: { file: "../x.png" } } } }))).toMatch(/^file/);
    expect(personaProblem(pack({ dot: { states: { idle: { file: ".hidden/x.png" } } } }))).toMatch(/^file/);
    expect(personaProblem(pack({ avatar: { sizes: { "64": "/abs.png" } } }))).toMatch(/^file/);
    expect(usablePersonas([pack(), pack({ format: 9 }, "two")]).map((p) => p.id)).toEqual(["doudou"]);
    expect(usablePersonas(null)).toEqual([]);
  });

  it("picks the smallest avatar that is big enough, else the largest", () => {
    const m = pack().manifest;
    expect(avatarFile(m, 40)).toBe("avatar/64.png");
    expect(avatarFile(m, 100)).toBe("avatar/128.png");
    expect(avatarFile(m, 128)).toBe("avatar/128.png");
    expect(avatarFile(m, 1000)).toBe("avatar/256.png");
  });

  it("falls back to idle's frames for a state the package lacks", () => {
    const m = pack().manifest;
    expect(dotFile(m, "working")).toBe("dot/working.png");
    expect(dotFile(m, "needs_you")).toBe("dot/idle.png");
  });

  it("puts the token and the rev in a file's address, each path part escaped", () => {
    expect(personaFileURL({ id: "doudou", rev: "r 1" }, "dot/idle.png", "jd_x&y")).toBe("/api/v1/personas/doudou/dot/idle.png?rev=r%201&token=jd_x%26y");
  });
});

describe("a tiered look moving to another tier", () => {
  const before = [{ ...pack(), rev: "rev-egg" }, pack({}, "other")];
  const after = [{ ...pack({ about: "NWVERSE 等级" }), rev: "rev-frog" }, pack({}, "other")];

  it("shows the new tier once its files are in", () => {
    expect(withChosenReady(after, before, "doudou", true)).toBe(after);
  });

  it("keeps the tier on screen while the new one has not come", () => {
    const shown = withChosenReady(after, before, "doudou", false);
    expect(shown[0].rev).toBe("rev-egg");
    expect(shown[1]).toBe(after[1]);
  });

  it("reads the words for the card", () => {
    expect(after[0].manifest.about).toBe("NWVERSE 等级");
    expect(personaProblem(after[0])).toBeNull();
  });
});

describe("persona state", () => {
  it("is offline while the hub or the computer is out of reach", () => {
    expect(personaState({ online: false, state: "working" })).toBe("offline");
    expect(personaState({ online: true, machineOnline: false, state: "needs_you" })).toBe("offline");
  });
  it("waits on you before anything else", () => {
    expect(personaState({ online: true, state: "needs_you" })).toBe("needs_you");
    expect(personaState({ online: true, state: "working", interactions: 1 })).toBe("needs_you");
  });
  it("thinks until it writes or calls a tool", () => {
    expect(personaState({ online: true, state: "working" })).toBe("thinking");
    expect(personaState({ online: true, state: "working", last: { role: "user", kind: "text" } })).toBe("thinking");
    expect(personaState({ online: true, state: "working", last: { role: "assistant", kind: "thinking" } })).toBe("thinking");
    expect(personaState({ online: true, state: "working", last: { role: "assistant", kind: "tool_use" } })).toBe("working");
    expect(personaState({ online: true, state: "working", last: { role: "assistant", kind: "text" } })).toBe("working");
  });
  it("is idle otherwise", () => {
    expect(personaState({ online: true })).toBe("idle");
    expect(personaState({ online: true, state: "unread" })).toBe("idle");
  });
});
