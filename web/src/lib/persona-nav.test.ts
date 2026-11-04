import { describe, expect, it } from "vitest";
import { NAV_PAGES, otherConversations, pluginNavKey, sidebarActiveKey } from "./persona-nav";
import { href, parse } from "./router";
import type { Session } from "./types";

const conv = (id: string, updated: string) => ({ machine_id: "hub", session_id: id, updated_at: updated }) as Session;

describe("Persona is the front door (#1239)", () => {
  it("opens the main conversation at / and at the old /j and /persona links", () => {
    expect(parse("/")).toEqual({ name: "jarvis" });
    expect(parse("/j")).toEqual({ name: "jarvis", sid: undefined });
    expect(parse("/persona")).toEqual({ name: "jarvis", sid: undefined });
    expect(href({ name: "jarvis" })).toBe("/");
  });

  it("keeps one conversation's link and moves the one-off new conversation to /j/new", () => {
    expect(parse("/j/abc")).toEqual({ name: "jarvis", sid: "abc" });
    expect(href({ name: "jarvis", sid: "abc" })).toBe("/j/abc");
    expect(parse("/j/new")).toEqual({ name: "persona-new" });
    expect(parse("/persona/new")).toEqual({ name: "persona-new" });
    expect(href({ name: "persona-new" })).toBe("/j/new");
  });

  it("moves 总览 to /overview and keeps unknown paths on it", () => {
    expect(parse("/overview")).toEqual({ name: "home" });
    expect(href({ name: "home" })).toBe("/overview");
    expect(parse("/no-such-page")).toEqual({ name: "home" });
  });

  it("keeps the other old links", () => {
    expect(parse("/s/mac/abc")).toEqual({ name: "session", mid: "mac", sid: "abc" });
    expect(parse("/history")).toEqual({ name: "history" });
    expect(parse("/inbox/x")).toEqual({ name: "inbox", id: "x" });
    expect(parse("/machines/mac")).toEqual({ name: "machine", mid: "mac" });
  });

  it("lights the Persona row for the main conversation however it is reached", () => {
    expect(sidebarActiveKey({ name: "jarvis" }, "main")).toBe("persona");
    expect(sidebarActiveKey({ name: "jarvis", sid: "main" }, "main")).toBe("persona");
    expect(sidebarActiveKey({ name: "jarvis", sid: "other" }, "main")).toBe("j:other");
    expect(sidebarActiveKey({ name: "jarvis" }, undefined)).toBe("persona");
    expect(sidebarActiveKey({ name: "persona-new" }, "main")).toBe("j:new");
    expect(sidebarActiveKey({ name: "home" }, "main")).toBe("home");
    expect(sidebarActiveKey({ name: "machine", mid: "mac" }, "main")).toBe("machines");
    expect(sidebarActiveKey({ name: "session", mid: "mac", sid: "s" }, "main")).toBe("s:mac/s");
  });

  it("lists the other conversations newest first without the main one", () => {
    const list = [conv("main", "2026-11-04T10:00:00Z"), conv("a", "2026-11-03T10:00:00Z"), conv("b", "2026-11-04T09:00:00Z")];
    expect(otherConversations(list, "main").map((c) => c.session_id)).toEqual(["b", "a"]);
    expect(otherConversations(list, undefined).map((c) => c.session_id)).toEqual(["main", "b", "a"]);
    expect(otherConversations(list, "main", 1).map((c) => c.session_id)).toEqual(["b"]);
  });

  it("orders the pages under Persona like the app", () => {
    expect(NAV_PAGES).toEqual(["home", "inbox", "messages", "workspace-board", "todos", "routines", "kb", "machines", "settings"]);
  });
});

describe("a plugin's page (docs/plugins.md「UI 插槽」 nav_page)", () => {
  it("has its own link and lights its own sidebar row", () => {
    expect(parse("/plugin/example-gateway/usage")).toEqual({ name: "plugin", plugin: "example-gateway", ui: "usage" });
    expect(href({ name: "plugin", plugin: "a b", ui: "c/d" })).toBe("/plugin/a%20b/c%2Fd");
    expect(parse(href({ name: "plugin", plugin: "a b", ui: "c/d" }))).toEqual({ name: "plugin", plugin: "a b", ui: "c/d" });
    expect(parse("/plugin/only-one")).toEqual({ name: "home" });
    expect(sidebarActiveKey({ name: "plugin", plugin: "g", ui: "usage" })).toBe(pluginNavKey("g", "usage"));
    expect(pluginNavKey("g", "usage")).toBe("p:g/usage");
  });
});
