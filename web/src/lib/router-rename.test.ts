import { describe, expect, it } from "vitest";
import { href, parse } from "./router";

describe("product rename route compatibility", () => {
  it("accepts Persona links without changing the old persisted route identity", () => {
    expect(parse("/persona")).toEqual({ name: "jarvis", sid: undefined });
    expect(parse("/persona/session%20id")).toEqual({ name: "jarvis", sid: "session id" });
    expect(parse("/j/session%20id")).toEqual({ name: "jarvis", sid: "session id" });
    expect(href(parse("/persona/session%20id"))).toBe("/j/session%20id");
  });
});
