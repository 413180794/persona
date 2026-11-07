import { describe, expect, it } from "vitest";
import { avatarFile, dotFile, isBuiltinPersona, normalizePersonaChoice, personaFileURL, personaProblem, personaState, RETIRED_PERSONAS, usablePersonas, withChosenReady, type PersonaInfo } from "./persona";

const pack = (patch: Partial<PersonaInfo["manifest"]> = {}, id = "doudou"): PersonaInfo => ({
  id,
  rev: "abc123",
  bytes: 1000,
  manifest: {
    format: 1,
    id,
    name: "Sample look",
    avatar: { sizes: { "64": "avatar/64.png", "256": "avatar/256.png", "128": "avatar/128.png" } },
    dot: { size: 144, states: { idle: { file: "dot/idle.png", fps: 8 }, working: { file: "dot/working.png" } } },
    ...patch,
  },
});

describe("persona packages", () => {
  it("takes what the hub takes", () => {
    expect(personaProblem(pack())).toBeNull();
    expect(personaProblem(pack({ format: 2 }))).toBe("format 2");
    expect(personaProblem(pack({}, "persona"))).toMatch(/^id/);
    expect(isBuiltinPersona("persona")).toBe(true);
    expect(isBuiltinPersona("doudou")).toBe(false);
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

  it.each(RETIRED_PERSONAS)("reserves retired ID %s without treating it as a drawable look", (id) => {
    expect(isBuiltinPersona(id)).toBe(false);
    expect(normalizePersonaChoice(id)).toBe("persona");
    expect(personaProblem(pack({}, id))).toBe(`id ${id}`);
    expect(usablePersonas([pack({}, id), pack({}, "nwverse"), pack({}, "new-look")]).map((p) => p.id)).toEqual(["nwverse", "new-look"]);
  });

  it("keeps generic extension choices and defaults empty selections", () => {
    expect(normalizePersonaChoice("new-look")).toBe("new-look");
    expect(normalizePersonaChoice("")).toBe("persona");
    expect(normalizePersonaChoice("persona")).toBe("persona");
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
  const after = [{ ...pack({ about: "NWVERSE level" }), rev: "rev-frog" }, pack({}, "other")];

  it("shows the new tier once its files are in", () => {
    expect(withChosenReady(after, before, "doudou", true)).toBe(after);
  });

  it("keeps the tier on screen while the new one has not come", () => {
    const shown = withChosenReady(after, before, "doudou", false);
    expect(shown[0].rev).toBe("rev-egg");
    expect(shown[1]).toBe(after[1]);
  });

  it("reads the words for the card", () => {
    expect(after[0].manifest.about).toBe("NWVERSE level");
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

describe("looks (#3200)", () => {
  it("draws an extension's SVG, its dark one on a dark page, and dark PNGs over the light SVG", async () => {
    const { avatarFile } = await import("./persona");
    const m = pack({ avatar: { sizes: { "128": "a/128.png" }, svg: "a/a.svg", dark: { svg: "a/dark.svg" } } }).manifest;
    expect(avatarFile(m, 64)).toBe("a/a.svg");
    expect(avatarFile(m, 64, true)).toBe("a/dark.svg");
    const pngDark = pack({ avatar: { sizes: { "128": "a/128.png" }, svg: "a/a.svg", dark: { sizes: { "128": "a/d128.png" } } } }).manifest;
    expect(avatarFile(pngDark, 64, true)).toBe("a/d128.png");
    expect(avatarFile(pack().manifest, 64, true)).toBe("avatar/64.png");
  });

  it("names a look in the interface language, else English, else its own", async () => {
    const { localized } = await import("./persona");
    expect(localized("极光", { en: "Aurora", ja: "オーロラ" }, "ja")).toBe("オーロラ");
    expect(localized("极光", { en: "Aurora" }, "ja")).toBe("Aurora");
    expect(localized("极光", { en: "Aurora" }, "zh-Hans")).toBe("极光");
    expect(localized("极光", undefined, "en")).toBe("极光");
  });

  it("takes the packages of the looks, not the built-in ones", async () => {
    const { lookPackages, isBuiltinPersona } = await import("./persona");
    const got = lookPackages([
      { id: "persona", source: "builtin", name: "Mochi", app_icon: "persona" },
      { id: "doudou", source: "extension", name: "Sample look", app_icon: "unsupported-icon", rev: "abc123", manifest: pack().manifest },
    ]);
    expect(got.map((p) => p.id)).toEqual(["doudou"]);
    expect(isBuiltinPersona("nwverse")).toBe(false);
  });
});
