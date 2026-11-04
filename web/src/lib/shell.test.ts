import { describe, expect, it } from "vitest";
import { href, parse } from "./router";
import { unreachable } from "./shell";

describe("unreachable: 离线 instead of loading forever", () => {
  it("shows once the socket gives up before anything loaded, and stays through a retry", () => {
    expect(unreachable(false, "connecting", false)).toBe(false);
    expect(unreachable(false, "offline", false)).toBe(true);
    expect(unreachable(true, "connecting", false)).toBe(true);
    expect(unreachable(true, "online", false)).toBe(false);
  });
  it("never once loaded: a later drop keeps the page (the small banner says so)", () => {
    expect(unreachable(false, "offline", true)).toBe(false);
    expect(unreachable(true, "offline", true)).toBe(false);
  });
});

describe("the phone's Agents page route", () => {
  it("is /agents, apart from a personal agent's /a/<id>", () => {
    expect(parse("/agents")).toEqual({ name: "agent-list" });
    expect(href({ name: "agent-list" })).toBe("/agents");
    expect(parse("/a")).toEqual({ name: "agents", id: undefined });
    expect(parse("/a/x1")).toEqual({ name: "agents", id: "x1" });
  });
});
