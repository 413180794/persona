import { describe, expect, it } from "vitest";
import { href, parse } from "./router";
import { linkRoute } from "./links";
import { BUILTIN_PERSONA, isBuiltinPersona } from "./persona";
import { BUILTIN_LOOKS } from "./extension-wire";

describe("Persona routing and rolling-upgrade builtin IDs", () => {
  it("reads and generates only the canonical assistant route", () => {
    expect(parse("/persona")).toEqual({ name: "persona", sid: undefined });
    expect(parse("/persona/session%20id")).toEqual({ name: "persona", sid: "session id" });
    expect(href(parse("/persona/session%20id"))).toBe("/persona/session%20id");
    expect(href({ name: "persona-new" })).toBe("/persona/new");
  });
  it("recognizes previous short URLs but generates canonical routes", () => {
    expect(parse("/j/session%20id")).toEqual({ name: "persona", sid: "session id" });
    expect(href(parse("/j/session%20id"))).toBe("/persona/session%20id");
    expect(parse("/j/new").name).toBe("persona-new");
    expect(linkRoute("https://hub.example.com/j/session", false, "https://hub.example.com").kind).toBe("conversation");
    expect(linkRoute("https://hub.example.com/persona/session", false, "https://hub.example.com")).toEqual({ kind: "conversation", sid: "session" });
  });
  it("draws only the default locally and agrees with the generated schema", () => {
    expect(BUILTIN_PERSONA).toBe("persona");
    expect(BUILTIN_LOOKS).toEqual([BUILTIN_PERSONA]);
    expect(new Set(BUILTIN_LOOKS).size).toBe(1);
    for (const id of BUILTIN_LOOKS) expect(isBuiltinPersona(id)).toBe(true);
  });
});
