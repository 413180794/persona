import { describe, expect, it } from "vitest";
import { firstAndCurrent, NAV_PAGES, otherConversations, extensionNavKey, sidebarActiveKey } from "./persona-nav";
import { href, parse, routePath } from "./router";
import type { Session } from "./types";

const conv = (id: string, updated: string) => ({ machine_id: "hub", session_id: id, updated_at: updated }) as Session;

describe("Persona is the front door (#1239)", () => {
  it("opens the main conversation at / and at the old /persona and /persona links", () => {
    expect(parse("/")).toEqual({ name: "persona" });
    expect(parse("/persona")).toEqual({ name: "persona", sid: undefined });
    expect(parse("/persona")).toEqual({ name: "persona", sid: undefined });
    expect(href({ name: "persona" })).toBe("/");
  });

  it("keeps one conversation's link and moves the one-off new conversation to /persona/new", () => {
    expect(parse("/persona/abc")).toEqual({ name: "persona", sid: "abc" });
    expect(href({ name: "persona", sid: "abc" })).toBe("/persona/abc");
    expect(parse("/persona/new")).toEqual({ name: "persona-new" });
    expect(parse("/persona/new")).toEqual({ name: "persona-new" });
    expect(href({ name: "persona-new" })).toBe("/persona/new");
  });

  it("moves 总览 to /overview and keeps unknown paths on it", () => {
    expect(parse("/overview")).toEqual({ name: "home" });
    expect(href({ name: "home" })).toBe("/overview");
    expect(parse("/no-such-page")).toEqual({ name: "home" });
  });

  it("keeps the other old links", () => {
    expect(parse("/s/mac/abc")).toEqual({ name: "session", mid: "mac", sid: "abc" });
    expect(parse("/history")).toEqual({ name: "history" });
    expect(parse("/history/inbox")).toEqual({ name: "history", origin: "inbox" });
    expect(routePath({ name: "history", origin: "task" })).toBe("/history/task");
    expect(parse("/inbox/x")).toEqual({ name: "inbox", id: "x" });
    expect(parse("/machines/mac")).toEqual({ name: "machine", mid: "mac" });
  });

  it("lights the Persona row for the main conversation however it is reached", () => {
    expect(sidebarActiveKey({ name: "persona" }, "main")).toBe("persona");
    expect(sidebarActiveKey({ name: "persona", sid: "main" }, "main")).toBe("persona");
    expect(sidebarActiveKey({ name: "persona", sid: "other" }, "main")).toBe("j:other");
    expect(sidebarActiveKey({ name: "persona" }, undefined)).toBe("persona");
    expect(sidebarActiveKey({ name: "persona-new" }, "main")).toBe("j:new");
    expect(sidebarActiveKey({ name: "history" }, "main")).toBe("history");
    expect(sidebarActiveKey({ name: "history", origin: "inbox" }, "main")).toBe("auto:inbox");
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

  it("keeps the open conversation listed when it is older than the recent ones (#2283)", () => {
    const list = [conv("main", "2026-11-03"), conv("a", "2026-11-01"), conv("b", "2026-11-02"), conv("c", "2026-10-30")];
    expect(otherConversations(list, "main", 1, "c").map((c) => c.session_id)).toEqual(["b", "c"]);
    // already among the recent ones, or not a conversation at all: the list is as before
    expect(otherConversations(list, "main", 2, "b").map((c) => c.session_id)).toEqual(["b", "a"]);
    expect(otherConversations(list, "main", 1, "main").map((c) => c.session_id)).toEqual(["b"]);
    expect(otherConversations(list, "main", 1, "gone").map((c) => c.session_id)).toEqual(["b"]);
  });

  it("shortens a list to its first n and the open item after them (#2283)", () => {
    const isFour = (x: number) => x === 4;
    expect(firstAndCurrent([1, 2, 3, 4, 5], 2, isFour)).toEqual([1, 2, 4]);
    expect(firstAndCurrent([1, 2, 3, 4, 5], 5, isFour)).toEqual([1, 2, 3, 4, 5]);
    expect(firstAndCurrent([1, 2, 3], 2, isFour)).toEqual([1, 2]);
  });

  it("orders the pages under Persona like the app", () => {
    expect(NAV_PAGES).toEqual(["home", "inbox", "health", "messages", "workspace-board", "todos", "routines", "kb", "machines", "terminal", "settings"]);
  });
});

describe("an extension's page (docs/extensions.md「UI 插槽」 nav_page)", () => {
  it("has its own link and lights its own sidebar row", () => {
    expect(parse("/extension/example-gateway/usage")).toEqual({ name: "extension", extension: "example-gateway", ui: "usage" });
    expect(href({ name: "extension", extension: "a b", ui: "c/d" })).toBe("/extension/a%20b/c%2Fd");
    expect(parse(href({ name: "extension", extension: "a b", ui: "c/d" }))).toEqual({ name: "extension", extension: "a b", ui: "c/d" });
    expect(parse("/extension/only-one")).toEqual({ name: "home" });
    expect(sidebarActiveKey({ name: "extension", extension: "g", ui: "usage" })).toBe(extensionNavKey("g", "usage"));
    expect(extensionNavKey("g", "usage")).toBe("p:g/usage");
  });
});
