import { describe, expect, it } from "vitest";
import { href, parse } from "./router";
import { linkRoute } from "./links";
import { BUILTIN_PERSONA, MARK_PERSONA, CIRCUIT_PERSONA, isBuiltinPersona } from "./persona";
import { BUILTIN_LOOKS } from "./extension-wire";

describe("Persona-only routing and builtin IDs", () => {
  it("reads and generates only the canonical assistant route", () => {
    expect(parse("/persona")).toEqual({ name: "persona", sid: undefined });
    expect(parse("/persona/session%20id")).toEqual({ name: "persona", sid: "session id" });
    expect(href(parse("/persona/session%20id"))).toBe("/persona/session%20id");
    expect(href({ name: "persona-new" })).toBe("/persona/new");
  });
  it("does not recognize a previous short URL as a conversation", () => {
    expect(parse("/j/session%20id").name).not.toBe("persona");
    expect(parse("/j/new").name).not.toBe("persona-new");
    expect(linkRoute("https://hub.example.com/j/session", false, "https://hub.example.com").kind).toBe("web");
    expect(linkRoute("https://hub.example.com/persona/session", false, "https://hub.example.com")).toEqual({ kind: "conversation", sid: "session" });
  });
  it("keeps Mochi and Visor distinct and agrees with the generated schema", () => {
    expect(BUILTIN_PERSONA).toBe("persona");
    expect(MARK_PERSONA).toBe("persona-visor");
    expect(BUILTIN_LOOKS).toEqual([BUILTIN_PERSONA, MARK_PERSONA, CIRCUIT_PERSONA]);
    expect(new Set(BUILTIN_LOOKS).size).toBe(3);
    for (const id of BUILTIN_LOOKS) expect(isBuiltinPersona(id)).toBe(true);
  });
});
