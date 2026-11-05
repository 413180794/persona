import { describe, expect, it } from "vitest";
import { adjacentTab, machineAt, panelToggle, terminalChat } from "./shortcut-nav";
import type { Panel } from "./panel";
import type { Route } from "./router";
import type { AgentState, Machine, Session } from "./types";

const here = { mid: "m1", sid: "s1" };
const files: Panel = { kind: "files", mid: "m1", sid: "s1", cwd: "/code" };
const link: Panel = { kind: "link", url: "https://example.com" };

describe("⌘B, the right panel", () => {
  it("closes an open one", () => {
    expect(panelToggle(files, null, here)).toEqual({ close: true });
    expect(panelToggle(link, null, undefined)).toEqual({ close: true });
  });
  it("brings the one just closed back", () => {
    expect(panelToggle(null, files, here)).toEqual({ open: files });
    expect(panelToggle(null, link, undefined)).toEqual({ open: link });
  });
  it("does not bring back a conversation's panel on another conversation", () => {
    expect(panelToggle(null, files, { mid: "m1", sid: "s2" }, "/other")).toEqual({ open: { kind: "files", mid: "m1", sid: "s2", cwd: "/other" } });
    expect(panelToggle(null, files, undefined)).toBeNull();
  });
  it("opens the conversation's files when there was none (its terminal is the page's view, not a panel)", () => {
    expect(panelToggle(null, null, here, "/code")).toEqual({ open: files });
    expect(panelToggle(null, null, here)).toBeNull();
    expect(panelToggle(null, null, undefined, "/code")).toBeNull();
  });
});

describe("⌘J, terminal ⇄ chat", () => {
  const page: Route = { name: "session", mid: "m1", sid: "s1" };
  const tab: Route = { name: "tab", mid: "m1", space: "w1", slot: "w1:t1", sid: "s1" };
  it("switches an agent's page to the other view, in place", () => {
    expect(terminalChat(page, "chat", true)).toEqual({ go: { ...page, view: "term" } });
    expect(terminalChat(page, "term", true)).toEqual({ go: { ...page, view: "chat" } });
    expect(terminalChat(tab, "chat", true)).toEqual({ go: { ...tab, view: "term" } });
    expect(terminalChat(tab, "term", true)).toEqual({ go: { ...tab, view: "chat" } });
  });
  it("does nothing on an agent without a terminal to show", () => {
    expect(terminalChat(page, "chat", false)).toBeNull();
  });
  it("goes from an agent's terminal page to its conversation", () => {
    expect(terminalChat({ name: "terminal", mid: "m1", id: "agent-1" }, "chat", false)).toEqual({ go: { name: "session", mid: "m1", sid: "agent-1", view: "chat" } });
  });
  it("has no conversation for a plain shell, and nothing on other pages", () => {
    expect(terminalChat({ name: "terminal", mid: "m1", id: "pane:w1:p2" }, "chat", false)).toBeNull();
    expect(terminalChat({ name: "terminal" }, "chat", false)).toBeNull();
    expect(terminalChat({ name: "settings" }, "chat", false)).toBeNull();
  });
});

const m1: Machine = { id: "m1", name: "One", online: true };
const m2: Machine = { id: "m2", name: "Two", online: true };
let n = 0;
function agent(o: { space: string; slot: number; sid: string; mid?: string; st?: AgentState }): Session {
  return {
    machine_id: o.mid ?? "m1",
    session_id: o.sid,
    title: `agent ${++n}`,
    cwd: "/code",
    status: "idle",
    state: o.st ?? "idle",
    space_kind: "herdr",
    space_id: o.space,
    space_name: `ws-${o.space}`,
    space_index: Number(o.space.replace(/\D/g, "")),
    slot_id: `${o.space}:t${o.slot}`,
    slot_index: o.slot,
    slot_name: String(o.slot),
    tab_id: `${o.space}:t${o.slot}`,
    pane_id: `${o.space}:p${o.slot}`,
  } as Session;
}

describe("⌘⌥← / →, the tab next to this one on the machine", () => {
  const sessions = [
    agent({ space: "w1", slot: 1, sid: "a" }),
    agent({ space: "w1", slot: 2, sid: "b" }),
    agent({ space: "w2", slot: 1, sid: "c" }),
    agent({ space: "w1", slot: 1, sid: "other", mid: "m2" }),
  ];

  it("walks the tabs in the sidebar's order, across workspaces", () => {
    expect(adjacentTab([m1, m2], sessions, { mid: "m1", sid: "a" }, 1)).toEqual({ name: "tab", mid: "m1", space: "w1", slot: "w1:t2" });
    expect(adjacentTab([m1, m2], sessions, { mid: "m1", sid: "b" }, 1)).toEqual({ name: "tab", mid: "m1", space: "w2", slot: "w2:t1" });
    expect(adjacentTab([m1, m2], sessions, { mid: "m1", sid: "c" }, -1)).toEqual({ name: "tab", mid: "m1", space: "w1", slot: "w1:t2" });
  });
  it("wraps round at either end", () => {
    expect(adjacentTab([m1, m2], sessions, { mid: "m1", sid: "c" }, 1)).toEqual({ name: "tab", mid: "m1", space: "w1", slot: "w1:t1" });
    expect(adjacentTab([m1, m2], sessions, { mid: "m1", sid: "a" }, -1)).toEqual({ name: "tab", mid: "m1", space: "w2", slot: "w2:t1" });
  });
  it("stays on its own machine", () => {
    expect(adjacentTab([m1, m2], sessions, { mid: "m2", sid: "other" }, 1)).toBeUndefined();
  });
  it("has nowhere to go from nowhere or from a session it does not know", () => {
    expect(adjacentTab([m1, m2], sessions, undefined, 1)).toBeUndefined();
    expect(adjacentTab([m1, m2], sessions, { mid: "m1", sid: "gone" }, 1)).toBeUndefined();
    expect(adjacentTab([m1, m2], sessions, { mid: "nope", sid: "a" }, 1)).toBeUndefined();
  });
});

describe("⌘1–9, the machines", () => {
  it("counts them in order from 1", () => {
    expect(machineAt([m1, m2], 1)).toBe(m1);
    expect(machineAt([m1, m2], 2)).toBe(m2);
    expect(machineAt([m1, m2], 3)).toBeUndefined();
    expect(machineAt([m1, m2], 0)).toBeUndefined();
  });
});
