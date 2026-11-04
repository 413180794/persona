import { describe, expect, it } from "vitest";
import { buildMachineTree, mostUrgent, paneRoute, resolveTab, summaryState, tabOfSessionLink, tabPagePane, tabSessions } from "./herdr-tree";
import type { AgentState, Machine, Session } from "./types";

const mac: Machine = { id: "mac", name: "Mac", online: true };
const box: Machine = { id: "box", name: "Box", online: true };
const away: Machine = { id: "away", name: "Away", online: false };

let n = 0;
function pane(o: Partial<Session> & { space?: string; slot?: number; p?: number; st?: AgentState }): Session {
  const { space, slot, p, st = "idle", ...rest } = o;
  const sidx = space ? Number(space.replace(/\D/g, "")) || 1 : undefined;
  return {
    machine_id: "mac",
    session_id: `s${++n}`,
    title: `pane ${n}`,
    cwd: "/code",
    status: "idle",
    state: st,
    ...(space ? { space_kind: "herdr", space_id: space, space_name: `ws-${space}`, space_index: sidx } : {}),
    ...(space && slot !== undefined ? { slot_id: `${space}:t${slot}`, slot_index: slot, slot_name: String(slot), tab_id: `${space}:t${slot}` } : {}),
    ...(space && p !== undefined ? { pane_id: `${space}:p${p}` } : {}),
    ...rest,
  };
}

describe("buildMachineTree", () => {
  it("orders machines as given, spaces by index, tabs by slot index, panes by pane number then id", () => {
    const list = [
      pane({ space: "w2", slot: 1, p: 1 }),
      pane({ space: "w1", slot: 2, p: 10, session_id: "b" }),
      pane({ space: "w1", slot: 2, p: 2, session_id: "z" }),
      pane({ space: "w1", slot: 2, p: 2, session_id: "a" }),
      pane({ space: "w1", slot: 1, p: 1 }),
      pane({ machine_id: "box", space: "w1", slot: 1, p: 1 }),
    ];
    const tree = buildMachineTree([box, mac], list);
    expect(tree.map((m) => m.machine.id)).toEqual(["box", "mac"]);
    const m = tree[1]!;
    expect(m.spaces.map((s) => s.name)).toEqual(["ws-w1", "ws-w2"]);
    expect(m.spaces[0]!.tabs.map((t) => t.label)).toEqual(["#1", "#2"]);
    expect(m.spaces[0]!.tabs[1]!.sessions.map((s) => s.session_id)).toEqual(["a", "z", "b"]);
  });

  it("orders spaces by herdr's index before their names", () => {
    const tree = buildMachineTree([mac], [pane({ space: "w3", slot: 1, space_name: "alpha", space_index: 2 }), pane({ space: "w4", slot: 1, space_name: "zeta", space_index: 1 })]);
    expect(tree[0]!.spaces.map((s) => s.name)).toEqual(["zeta", "alpha"]);
  });

  it("orders spaces of equal index by name", () => {
    const tree = buildMachineTree([mac], [pane({ space: "w3", slot: 1, space_name: "zeta", space_index: 1 }), pane({ space: "w4", slot: 1, space_name: "alpha", space_index: 1 })]);
    expect(tree[0]!.spaces.map((s) => s.name)).toEqual(["alpha", "zeta"]);
  });

  it("lists a host without tabs right under its space, and sessions without a space under「其他」", () => {
    const bot = pane({ space_kind: "botmux", space_id: "dir:/code", space_name: "code", pane_id: "botmux:x" });
    const loose = pane({});
    const tree = buildMachineTree([mac], [bot, loose]);
    const sp = tree[0]!.spaces[0]!;
    expect(sp.tabs).toEqual([]);
    expect(sp.loose).toEqual([bot]);
    expect(tree[0]!.other).toEqual([loose]);
  });

  it("keeps two hosts' spaces with the same id apart", () => {
    const tree = buildMachineTree([mac], [pane({ space: "w1", slot: 1 }), pane({ space: "w1", slot: 1, space_kind: "orca" })]);
    expect(tree[0]!.spaces).toHaveLength(2);
  });

  it("sums up each level as its most urgent state: needs you > working > unread > seen", () => {
    const list = [
      pane({ space: "w1", slot: 1, p: 1, st: "unread" }),
      pane({ space: "w1", slot: 1, p: 2, st: "working" }),
      pane({ space: "w2", slot: 1, p: 1, st: "idle" }),
      pane({ space: "w3", slot: 1, p: 1, st: "needs_you" }),
      pane({ space: "w3", slot: 1, p: 2, st: "needs_you" }),
    ];
    const m = buildMachineTree([mac], list)[0]!;
    const tab = m.spaces[0]!.tabs[0]!;
    expect(tab.state).toBe("working");
    expect(tab.lead).toBe(list[1]);
    expect(m.spaces.map((s) => s.state)).toEqual(["working", "idle", "needs_you"]);
    expect(m.state).toBe("needs_you");
    expect(m.needs).toBe(2);
    expect(summaryState([pane({ st: "idle" }), pane({ st: "unread" })])).toBe("unread");
    expect(mostUrgent([])).toBeUndefined();
  });

  it("leaves out assistant and offline sessions, and lists an offline machine empty", () => {
    const list = [pane({ space: "w1", slot: 1, assistant: true }), pane({ space: "w1", slot: 1, st: "offline" }), pane({ machine_id: "away", space: "w1", slot: 1, st: "working" })];
    const tree = buildMachineTree([mac, away], list);
    expect(tree.map((m) => [m.machine.id, m.sessions.length])).toEqual([["mac", 0], ["away", 0]]);
    expect(buildMachineTree([mac, away], list, { hideEmpty: true })).toEqual([]);
  });

  it("puts pinned spaces first on their machine", () => {
    const tree = buildMachineTree([mac], [pane({ space: "w1", slot: 1 }), pane({ space: "w2", slot: 1 })], { pins: { spaces: ["mac/ws-w2"], sessions: [] } });
    expect(tree[0]!.spaces.map((s) => [s.name, s.pinned])).toEqual([["ws-w2", true], ["ws-w1", false]]);
  });

  it("keys folds by machine and space name, so they survive a herdr restart", () => {
    const before = buildMachineTree([mac], [pane({ space: "w1", slot: 1, space_name: "app" })])[0]!;
    const after = buildMachineTree([mac], [pane({ space: "w7", slot: 1, space_name: "app" })])[0]!;
    expect(after.spaces[0]!.key).not.toBe(before.spaces[0]!.key);
    expect(after.spaces[0]!.foldKey).toBe(before.spaces[0]!.foldKey);
    expect(after.foldKey).toBe(before.foldKey);
  });
});

describe("tabSessions", () => {
  it("is the live panes of the same machine, space and slot, in pane order", () => {
    const a = pane({ space: "w1", slot: 1, p: 2 });
    const b = pane({ space: "w1", slot: 1, p: 1 });
    const gone = pane({ space: "w1", slot: 1, p: 3, st: "offline" });
    const other = [pane({ space: "w1", slot: 2, p: 1 }), pane({ machine_id: "box", space: "w1", slot: 1, p: 1 }), pane({ space: "w1", slot: 1, p: 4, space_kind: "orca" })];
    expect(tabSessions([a, b, gone, ...other], a)).toEqual([b, a]);
  });

  it("is just the session without a slot", () => {
    const s = pane({ space: "w1" });
    expect(tabSessions([s, pane({ space: "w1" })], s)).toEqual([s]);
  });
});

describe("resolveTab and paneRoute", () => {
  const a = pane({ space: "w1", slot: 1, p: 1, st: "idle" });
  const b = pane({ space: "w1", slot: 1, p: 2, st: "needs_you" });
  const c = pane({ space: "w2", slot: 1, p: 1 });
  const all = [a, b, c];

  it("opens the asked pane, else the tab's most urgent one", () => {
    expect(resolveTab(all, { mid: "mac", space: "w1", slot: "w1:t1", sid: a.session_id })).toBe(a);
    expect(resolveTab(all, { mid: "mac", space: "w1", slot: "w1:t1" })).toBe(b);
  });

  it("falls back to the session when the tab's ids changed, and to nothing", () => {
    expect(resolveTab(all, { mid: "mac", space: "w9", slot: "w9:t1", sid: c.session_id })).toBe(c);
    expect(resolveTab(all, { mid: "mac", space: "w9", slot: "w9:t1", sid: "nope" })).toBeUndefined();
    expect(resolveTab(all, { mid: "box", space: "w1", slot: "w1:t1" })).toBeUndefined();
  });

  it("links a pane with a tab to the tab, the rest to the session", () => {
    expect(paneRoute(a)).toEqual({ name: "tab", mid: "mac", space: "w1", slot: "w1:t1", sid: a.session_id });
    const s = pane({});
    expect(paneRoute(s)).toEqual({ name: "session", mid: "mac", sid: s.session_id });
  });
});

describe("tabOfSessionLink: an old /s/ link of a pane in a tab is that tab's /t/", () => {
  const a = pane({ space: "w1", slot: 1, p: 1 });
  const loose = pane({});
  it("names the tab and the pane", () => {
    expect(tabOfSessionLink([a, loose], { mid: "mac", sid: a.session_id })).toEqual({ name: "tab", mid: "mac", space: "w1", slot: "w1:t1", sid: a.session_id });
  });
  it("leaves a session without a tab, an unknown one and another machine's", () => {
    expect(tabOfSessionLink([a, loose], { mid: "mac", sid: loose.session_id })).toBeUndefined();
    expect(tabOfSessionLink([a], { mid: "mac", sid: "nope" })).toBeUndefined();
    expect(tabOfSessionLink([a], { mid: "box", sid: a.session_id })).toBeUndefined();
  });
});

describe("tabPagePane", () => {
  const r = { mid: "mac", sid: "gone" };
  it("waits on the link's session only while the list is not in", () => {
    expect(tabPagePane(undefined, r, false)).toEqual({ mid: "mac", sid: "gone" });
  });
  it("shows nothing once the list is in and the tab and its session are gone (the page says the tab is gone)", () => {
    expect(tabPagePane(undefined, r, true)).toBeUndefined();
    expect(tabPagePane(undefined, { mid: "mac" }, false)).toBeUndefined();
  });
  it("opens the tab's pane when it is there", () => {
    expect(tabPagePane({ machine_id: "mac", session_id: "a1" } as never, r, true)).toEqual({ mid: "mac", sid: "a1" });
  });
});
