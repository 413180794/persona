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
