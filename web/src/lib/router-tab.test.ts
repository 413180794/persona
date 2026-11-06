import { describe, expect, it } from "vitest";
import { href, parse, type Route } from "./router";

describe("tab route (/t/<mid>/<space>/<slot>[/<sid>])", () => {
  it("round-trips, with and without the pane", () => {
    const routes: Route[] = [
      { name: "tab", mid: "macbook", space: "w1", slot: "w1:t2", sid: "a1f3" },
      { name: "tab", mid: "my mac", space: "dir:/home/me/code", slot: "s/1", sid: undefined },
    ];
    for (const r of routes) expect(parse(href(r))).toEqual(r);
  });

  it("encodes every segment", () => {
    expect(href({ name: "tab", mid: "m", space: "dir:/a b", slot: "w1:t1" })).toBe("/t/m/dir%3A%2Fa%20b/w1%3At1");
  });

  it("needs machine, space and slot", () => {
    expect(parse("/t/m/w1").name).not.toBe("tab");
  });

  it("keeps the old session links", () => {
    expect(parse("/s/macbook/a1f3")).toEqual({ name: "session", mid: "macbook", sid: "a1f3" });
    expect(href({ name: "session", mid: "macbook", sid: "a1f3" })).toBe("/s/macbook/a1f3");
  });
});

describe("?view= of an agent's page", () => {
  it("is part of the route, for /s/ and /t/", () => {
    expect(parse("/s/m/a", "?view=term")).toEqual({ name: "session", mid: "m", sid: "a", view: "term" });
    expect(parse("/t/m/w1/w1%3At2/a", "?view=chat")).toEqual({ name: "tab", mid: "m", space: "w1", slot: "w1:t2", sid: "a", view: "chat" });
  });
  it("is absent without it, or with a value that is neither", () => {
    expect(parse("/s/m/a")).toEqual({ name: "session", mid: "m", sid: "a" });
    expect(parse("/s/m/a", "?view=shell")).toEqual({ name: "session", mid: "m", sid: "a" });
    expect(parse("/s/m/a", "?x=1&view=term")).toMatchObject({ view: "term" });
  });
  it("round-trips through href", () => {
    const routes: Route[] = [
      { name: "session", mid: "m", sid: "a", view: "term" },
      { name: "tab", mid: "m", space: "w1", slot: "w1:t1", sid: "a", view: "chat" },
    ];
    for (const r of routes) {
      const [path, query] = href(r).split("?");
      expect(parse(path!, `?${query}`)).toEqual(r);
    }
    expect(href({ name: "session", mid: "m", sid: "a" })).toBe("/s/m/a");
  });
});

describe("a malformed percent escape", () => {
  it("routes instead of throwing, keeping the undecodable segment as it is", () => {
    expect(parse("/s/m/%E0%A4%A")).toEqual({ name: "session", mid: "m", sid: "%E0%A4%A" });
    expect(parse("/s/my%20mac/%ZZ")).toEqual({ name: "session", mid: "my mac", sid: "%ZZ" });
    expect(parse("/persona/%ZZ")).toEqual({ name: "persona", sid: "%ZZ" });
  });
});
