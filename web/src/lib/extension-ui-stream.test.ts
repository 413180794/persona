import { describe, expect, it, vi, afterEach } from "vitest";
import snapshotTrace from "../../../schema/extension/examples/UIStreamSnapshot.keyed.json?raw";
import deltaTrace from "../../../schema/extension/examples/UIStreamDelta.operations.json?raw";
import expectedTrace from "../../../schema/extension/examples/UIStreamSnapshot.after-operations.json?raw";
import emptyNative from "../../../schema/extension/examples/UIStreamSnapshot.required-empty.json?raw";
import {
  ExtensionUIStreamPages, ExtensionUIStreamSession, reduceUIStream, streamPageKey, STREAM_LIMITS,
  type StreamPageOptions,
} from "./extension-ui-stream";
import type { Component, UIStreamDelta, UIStreamRequest, UIStreamSnapshot } from "./extension-wire";

const snap = (items: Component[] = []): UIStreamSnapshot => ({ protocol: 1, session: "session-a", epoch: "epoch-a", version: 1, root_key: "root", view: { title: "Example", sections: [{ key: "main", items }, { key: "other", items: [] }] } });
const delta = (operations: UIStreamDelta["operations"], base = 1): UIStreamDelta => ({ protocol: 1, session: "session-a", epoch: "epoch-a", base_version: base, version: base + (operations.length ? 1 : 0), operations });
const start = (s = snap()) => reduceUIStream(null, "snapshot", s).snapshot!;
const button = (key = "button", action = "callback"): Component => ({ type: "button", key, title: "Run", action: { id: action } });
const field: Component = { type: "field", key: "field", kind: "text", label: "Draft", name: "text", value: "initial" };
const tick = async () => { for (let i = 0; i < 8; i++) await Promise.resolve(); };
function setup(send?: (r: UIStreamRequest) => Promise<void>) {
  const sent: UIStreamRequest[] = [];
  let n = 0;
  const options: StreamPageOptions = {
    identity: { owner: "owner-a", hubGroup: "group-a", extension: "example", contribution: "page", navigation: "entry-a" },
    context: { platform: "web", ui_version: 5, stream: { protocol: 1, components: ["button", "field", "text", "divider", "segmented", "composer", "progress"] } },
    transport: { send: async (r) => { sent.push(r); await send?.(r); } }, id: () => `id-${++n}`,
  };
  const session = new ExtensionUIStreamSession(options);
  return { session, sent, options };
}
function opened(send?: (r: UIStreamRequest) => Promise<void>, s = snap([button(), field])) {
  const test = setup(send); test.session.open(); expect(test.session.receiveSnapshot(s)).toBe(true); return test;
}

describe("native stream atomic reducer", () => {
  it("compares same-version snapshots structurally while retaining optional-field presence", () => {
    const current = start(snap([button()]));
    const reordered = { view: current.view, root_key: current.root_key, version: current.version, epoch: current.epoch, session: current.session, protocol: current.protocol };
    expect(reduceUIStream(current, "snapshot", reordered)).toEqual({ snapshot: current, recover: false });
    const changed = structuredClone(current); changed.view.state = "";
    expect(reduceUIStream(current, "snapshot", changed)).toMatchObject({ snapshot: current, recover: true });
  });
  it.each([
    [["A", "B"], ["A", "B", "C"]],
    [["A", "B"], ["B", "A"]],
  ])("rejects chart series with inconsistent label ordering atomically (%j)", (first, second) => {
    const chart: Component = { type: "chart", key: "chart", kind: "line", series: [first, second].map((labels, i) => ({ name: `Series ${i}`, points: labels.map((label) => ({ label, value: 1 })) })) };
    const current = start(snap([button()]));
    expect(reduceUIStream(null, "snapshot", snap([chart])).recover).toBe(true);
    expect(reduceUIStream(current, "delta", delta([{ op: "remove", key: "button" }, { op: "insert", parent: "main", item: chart }]))).toMatchObject({ snapshot: current, recover: true });
  });
  it("preserves raw optional false/zero/empty values and omission through snapshot and replace", () => {
    const original = snap([{ type: "field", key: "field", kind: "choice", name: "choice", label: "Choice", value: "", choices: [] }, { type: "progress", key: "progress", label: "Progress", ratio: 0, detail: "" }, { type: "button", key: "empty", title: "Empty", action: { id: "empty", submit: false, pop: false, values: {} } }]);
    const before = start(original);
    expect(JSON.parse(JSON.stringify(before))).toEqual(original);
    const next = reduceUIStream(before, "delta", JSON.stringify(delta([{ op: "replace", key: "field", item: { type: "field", key: "field", kind: "text", name: "choice", label: "Choice" } }])));
    expect(next.recover).toBe(false);
    expect(Object.hasOwn(next.snapshot!.view.sections[0].items[0], "value")).toBe(false);
    expect(next.snapshot!.view.sections[0].items[1]).toBe(before.view.sections[0].items[1]);
  });
  it("moves entire keyed section subtrees both ways and rejects duplicate inserted subtrees", () => {
    const current = start(snap([button()]));
    const next = reduceUIStream(current, "delta", delta([{ op: "move", key: "other", parent: "root", before: "main" }, { op: "move", key: "other", parent: "root" }]));
    expect(next.recover).toBe(false);
    expect(next.snapshot!.view.sections).toEqual(current.view.sections);
    expect(reduceUIStream(current, "delta", delta([{ op: "insert", parent: "root", section: { key: "new", items: [button()] } }]))).toMatchObject({ snapshot: current, recover: true });
  });
  it.each([null, [], "not JSON", '{"protocol":1}', { ...snap(), unknown: false }])("rejects malformed frames without mutating canonical state (%j)", (input) => {
    const current = start();
    expect(reduceUIStream(current, "snapshot", input)).toMatchObject({ snapshot: current, recover: true });
    expect(reduceUIStream(current, "delta", input)).toMatchObject({ snapshot: current, recover: true });
  });
  it("consumes the shared snapshot → operations → expected trace without changing fixtures", () => {
    const before = reduceUIStream(null, "snapshot", snapshotTrace);
    expect(before.recover).toBe(false);
    const after = reduceUIStream(before.snapshot, "delta", deltaTrace);
    expect(after).toEqual({ snapshot: JSON.parse(expectedTrace), recover: false });
  });
  it("keeps schema-required empty native fields through the production JSON codec", () => {
    const result = reduceUIStream(null, "snapshot", emptyNative);
    expect(result.recover).toBe(false);
    expect(JSON.parse(JSON.stringify(result.snapshot))).toEqual(JSON.parse(emptyNative));
  });
  it("inserts, reorders in both directions, moves across sections, removes and clears omitted metadata", () => {
    let s = start(snap([button("a"), button("b"), button("c")]));
    s = reduceUIStream(s, "delta", delta([{ op: "move", key: "a", parent: "main", before: "c" }])).snapshot!;
    expect(s.view.sections[0].items.map((i) => i.key)).toEqual(["b", "a", "c"]);
    s = reduceUIStream(s, "delta", delta([{ op: "move", key: "c", parent: "main", before: "b" }, { op: "move", key: "a", parent: "other" }], 2)).snapshot!;
    expect(s.view.sections[0].items.map((i) => i.key)).toEqual(["c", "b"]);
    expect(s.view.sections[1].items.map((i) => i.key)).toEqual(["a"]);
    s = reduceUIStream(s, "delta", delta([{ op: "replace", key: "root", view: { sections: [] } }], 3)).snapshot!;
    expect(s.view).toEqual({ sections: [] });
  });
  it("retains unrelated native object identity and optional false/zero/empty values", () => {
    const s = start(snap([button(), { type: "progress", key: "p", label: "Count", ratio: 0 }]));
    const next = reduceUIStream(s, "delta", delta([{ op: "replace", key: "p", item: { type: "progress", key: "p", label: "Count", ratio: 0, detail: "" } }])).snapshot!;
    expect(next.view.sections[0].items[0]).toBe(s.view.sections[0].items[0]);
    expect(next.view.sections[1]).toBe(s.view.sections[1]);
    expect(next.view.sections[0].items[1]).toEqual({ type: "progress", key: "p", label: "Count", ratio: 0, detail: "" });
    const tiles = start(snap([{ type: "grid", key: "grid", tiles: [{ key: "tile", title: "Tile", active: false, value: "" }] }]));
    expect(JSON.parse(JSON.stringify(tiles)).view.sections[0].items[0].tiles[0].active).toBe(false);
  });
  it.each([
    { op: "execute", key: "a" },
    { op: "remove", key: "root" },
    { op: "remove", key: "absent" },
    { op: "move", key: "a", parent: "a" },
    { op: "move", key: "a", parent: "root" },
    { op: "move", key: "main", parent: "other" },
    { op: "move", key: "a", parent: "main", before: "a" },
    { op: "move", key: "a", parent: "main", before: "other" },
    { op: "insert", parent: "main", before: "missing", item: button("x") },
    { op: "insert", parent: "main", before: "", item: button("x") },
    { op: "insert", parent: "main", before: null, item: button("x") },
    { op: "replace", key: "a", item: button("different") },
    { op: "replace", key: "a", section: { key: "a", items: [] } },
    { op: "replace", key: "a", item: button("a"), section: { key: "a", items: [] } },
    { op: "replace", key: "a", item: { type: "text", key: "a", text: false } },
    { op: "replace", key: "a", item: { type: "text", key: "a", text: "ok", execute: "code" } },
  ])("rolls back every operation on malformed input or invalid topology (%j)", (op) => {
    const s = start(snap([button("a")]));
    const d = { ...delta([]), version: 2, operations: [{ op: "insert", parent: "main", item: button("first") }, op] };
    const result = reduceUIStream(s, "delta", d);
    expect(result.recover).toBe(true); expect(result.snapshot).toBe(s);
  });
  it("enforces a single namespace including root, sections, components and tiles", () => {
    for (const name of ["root", "main", "grid"]) {
      expect(reduceUIStream(null, "snapshot", snap([{ type: "grid", key: "grid", tiles: [{ key: name, title: "Tile" }] }])).recover).toBe(true);
    }
    expect(reduceUIStream(null, "snapshot", snap([button("main")])).recover).toBe(true);
    expect(reduceUIStream(null, "snapshot", snap([{ type: "grid", key: "grid", tiles: [{ title: "Missing key" }] }])).recover).toBe(true);
    const s = start(snap([{ type: "grid", key: "grid", tiles: [{ key: "tile", title: "Tile" }] }]));
    for (const op of [{ op: "remove", key: "tile" }, { op: "move", key: "tile", parent: "main" }, { op: "replace", key: "tile", item: button("tile") }]) {
      expect(reduceUIStream(s, "delta", { ...delta([]), version: 2, operations: [op] }).snapshot).toBe(s);
    }
  });
  it("rejects extension HTML while leaving assistant readers outside this reducer", () => {
    expect(reduceUIStream(null, "snapshot", snap([{ type: "html", key: "html", html: "<p>Forbidden</p>" }])).recover).toBe(true);
  });
  it("rejects duplicates, stale epochs, version gaps and invalid empty-batch version changes", () => {
    const s = start(snap([button()]));
    const d = delta([{ op: "remove", key: "button" }]);
    const next = reduceUIStream(s, "delta", d).snapshot!;
    expect(reduceUIStream(next, "delta", d)).toMatchObject({ recover: true, snapshot: next });
    for (const patch of [{ epoch: "old" }, { session: "other" }, { base_version: 2 }, { version: 1 }, { version: 0 }, { version: 1.5 }, { version: Number.MAX_SAFE_INTEGER + 1 }]) {
      expect(reduceUIStream(s, "delta", { ...d, ...patch }).recover).toBe(true);
    }
    expect(reduceUIStream(s, "delta", delta([])).recover).toBe(false);
    expect(reduceUIStream(s, "delta", { ...delta([]), version: 2 })).toMatchObject({ recover: false, snapshot: { version: 2 } });
    expect(reduceUIStream(s, "delta", { ...delta([]), version: 3 }).recover).toBe(true);
    expect(reduceUIStream(s, "delta", { ...d, version: 3 }).recover).toBe(true);
    expect(reduceUIStream(null, "delta", d).recover).toBe(true);
  });
  it("validates original numeric literals before JSON rounding, including escaped property names", () => {
    for (const raw of ["9007199254740990.9", "9007199254740991.1", "9007199254740991.00000000001", "1e99999", "0.99999999999999999"]) {
      const text = JSON.stringify(snap()).replace('"version":1', `"ver\\u0073ion":${raw}`);
      expect(reduceUIStream(null, "snapshot", text).recover).toBe(true);
    }
    for (const raw of ["1.0", "10e-1", "9007199254740991"]) {
      expect(reduceUIStream(null, "snapshot", JSON.stringify(snap()).replace('"version":1', `"version":${raw}`)).recover).toBe(false);
    }
  });
  it("bounds frames, operations, logical nodes, nesting and opaque state before mutation", () => {
    const s = start();
    const oversized = snap([{ type: "text", key: "large", text: "x".repeat(STREAM_LIMITS.frame) }]);
    expect(reduceUIStream(s, "snapshot", oversized).recover).toBe(true);
    expect(reduceUIStream(s, "delta", delta(Array.from({ length: 257 }, () => ({ op: "remove", key: "main" })))).recover).toBe(true);
    expect(reduceUIStream(null, "snapshot", snap(Array.from({ length: 298 }, (_, i) => ({ type: "divider", key: `n-${i}` })))).recover).toBe(true);
    expect(reduceUIStream(null, "snapshot", snap(Array.from({ length: 297 }, (_, i) => ({ type: "divider", key: `n-${i}` })))).recover).toBe(false);
    const nested = `{"unexpected":${"[".repeat(17)}0${"]".repeat(17)}}`;
    expect(reduceUIStream(s, "snapshot", nested).snapshot).toBe(s);
    expect(reduceUIStream(null, "snapshot", { ...snap(), view: { sections: [], state: "😀".repeat(5000) } }).recover).toBe(true);
    expect(reduceUIStream(s, "delta", { ...delta([]), operations: undefined }).recover).toBe(true);
  });
});

describe("injected native page sessions (not real Deno E2E)", () => {
  afterEach(() => vi.useRealTimers());
  it("deduplicates automatic More observations across recovery but not changed definitions or epochs", () => {
    const initial = snap([button()]); initial.view.sections[0].more = { action: { id: "more" } };
    const { session } = opened(undefined, initial);
    expect(session.observeMore("main")).toBe(true);
    expect(session.observeMore("main")).toBe(false);
    session.setConnected(false); session.setConnected(true);
    expect(session.observeMore("main")).toBe(false);
    expect(session.receiveSnapshot(initial)).toBe(true);
    expect(session.observeMore("main")).toBe(false);
    const next = structuredClone(initial); next.version = 2; next.view.sections[0].more = { title: "Next page", action: { id: "more" } };
    expect(session.receiveSnapshot(next)).toBe(true);
    expect(session.observeMore("main")).toBe(true);
    session.setConnected(false); session.setConnected(true);
    expect(session.receiveSnapshot({ ...next, epoch: "epoch-b", version: 1 })).toBe(true);
    expect(session.observeMore("main")).toBe(true);
    session.close(); expect(session.observeMore("main")).toBe(false);
  });
  it("opens from an explicit refresh before any snapshot or open request", async () => {
    const { session, sent } = setup(); session.refresh(); await tick();
    expect(sent.map((r) => r.request)).toEqual(["open"]);
    expect(session.receiveSnapshot(snap())).toBe(true);
  });
  it("uses the shared UUID fallback on plaintext LAN origins", async () => {
    const original = crypto;
    vi.stubGlobal("crypto", { getRandomValues: original.getRandomValues.bind(original) });
    try {
      const { options, sent } = setup();
      const session = new ExtensionUIStreamSession({ ...options, id: undefined });
      session.open(); await tick();
      expect(sent[0]).toMatchObject({ request: "open", request_id: expect.stringMatching(/^[a-f0-9-]{36}$/) });
      session.close();
    } finally { vi.unstubAllGlobals(); }
  });
  it.each(["epoch-a", "epoch-b"])("ignores lifecycle failures after a fresh authoritative snapshot (%s)", async (epoch) => {
    let reject!: (e: Error) => void;
    const { session } = opened(async (r) => {
      if (r.request === "resume") await new Promise<void>((_, no) => { reject = no; });
    });
    await tick(); session.setConnected(false); session.setConnected(true); await tick();
    expect(session.receiveSnapshot({ ...snap([button(), field]), epoch })).toBe(true);
    reject(new Error("Retired resume failed")); await tick();
    expect(session.getState()).toMatchObject({ phase: "ready", error: undefined });
  });
  it("does not let a retired epoch's late send failure invalidate a recovered epoch", async () => {
    let reject!: (e: Error) => void;
    const { session, sent } = opened(async (r) => {
      if (r.request === "event" && r.interaction.epoch === "epoch-a") await new Promise<void>((_, no) => { reject = no; });
    });
    const old = session.event("button", "callback", "press", null).catch((e) => e);
    await tick(); session.setConnected(false); session.setConnected(true);
    expect(session.receiveSnapshot({ ...snap([button(), field]), epoch: "epoch-b" })).toBe(true);
    const fresh = session.event("button", "callback", "press", 0).catch((e) => e);
    reject(new Error("Old delivery failed")); await old; await fresh; await tick();
    expect(session.getState()).toMatchObject({ phase: "ready", snapshot: { epoch: "epoch-b" } });
    expect(sent.filter((r) => r.request === "snapshot")).toHaveLength(0);
    expect(sent.filter((r) => r.request === "event")).toHaveLength(2);
  });
  it("opens once and requires a full snapshot before accepting deltas or callbacks", async () => {
    const { session, sent } = setup();
    expect(session.receiveDelta(delta([]))).toBe(false);
    expect(session.receiveSnapshot(snap())).toBe(false);
    session.open(); session.open(); await tick();
    expect(sent.map((r) => r.request)).toEqual(["open"]);
    expect(sent[0]).toMatchObject({ state: "", context: { stream: { protocol: 1 } } });
    await expect(session.event("button", "callback", "press", null)).rejects.toThrow("not ready");
    expect(session.receiveSnapshot(snap([button()]))).toBe(true);
  });
  it("serializes callbacks and retains IDs/value on explicit retry only", async () => {
    let release!: () => void; let failFirst = true;
    const { session, sent } = opened(async (r) => {
      if (r.request === "event" && failFirst) { await new Promise<void>((r) => { release = r; }); throw new Error("connection lost"); }
    });
    const first = session.event("button", "callback", "press", false).catch((e) => e);
    const second = session.event("button", "callback", "press", 0).catch((e) => e);
    await tick(); expect(sent.filter((r) => r.request === "event")).toHaveLength(1);
    const request = sent.find((r) => r.request === "event")!;
    release(); await tick(); await first; await second;
    expect(sent.filter((r) => r.request === "event")).toHaveLength(1);
    expect(sent.some((r) => r.request === "snapshot")).toBe(true);
    expect(session.receiveSnapshot(snap([button(), field]))).toBe(true);
    failFirst = false;
    if (request.request !== "event") throw new Error("Expected event");
    await session.retryEvent(request.interaction.event_id);
    const replay = sent.filter((r) => r.request === "event")[1];
    expect(replay).toMatchObject({ interaction: request.interaction });
    expect(replay.request_id).not.toBe(request.request_id);
  });
  it("rejects foreign node callbacks and wrong event slots even when callback IDs repeat", async () => {
    const s = snap([button("a", "shared"), button("b", "other"), { type: "segmented", key: "seg", name: "filter", choices: [{ value: "a", title: "A" }, { value: "b", title: "B" }], value: "a", action: { id: "shared" } }]);
    const { session, sent } = opened(undefined, s);
    await expect(session.event("b", "shared", "press", null)).rejects.toThrow("callback");
    await expect(session.event("a", "shared", "change", 0)).rejects.toThrow("callback");
    await session.event("seg", "shared", "change", "a");
    expect(sent.filter((r) => r.request === "event")).toHaveLength(1);
  });
  it("preserves keyed drafts/local state on text updates, reorders and snapshot recovery, removes deleted keys", () => {
    const { session } = opened();
    session.setDraft("field", "unsent draft"); session.setLocal("button", { expanded: true, scroll: 120 });
    expect(session.receiveDelta(delta([{ op: "insert", parent: "main", before: "field", item: { type: "text", key: "text", text: "Live" } }, { op: "move", key: "field", parent: "other" }]))).toBe(true);
    expect(session.getDraft("field")).toBe("unsent draft"); expect(session.getLocal("button")).toEqual({ expanded: true, scroll: 120 });
    session.setConnected(false); session.setConnected(true);
    const recovered = { ...snap([button(), field]), version: 3 };
    expect(session.receiveSnapshot(recovered)).toBe(true);
    expect(session.getDraft("field")).toBe("unsent draft");
    expect(session.receiveDelta(delta([{ op: "remove", key: "field" }, { op: "remove", key: "button" }], 3))).toBe(true);
    expect(session.getDraft("field")).toBeUndefined(); expect(session.getLocal("button")).toBeUndefined();
  });
  it("recovers from duplicate/gap/unknown/rollback frames without repeating a side effect", async () => {
    const { session, sent } = opened();
    await session.event("button", "callback", "press", null);
    const previous = session.getState().snapshot;
    expect(session.receiveDelta({ ...delta([]), version: 2, operations: [{ op: "remove", key: "field" }, { op: "unknown" }] })).toBe(false);
    expect(session.getState().snapshot).toBe(previous);
    expect(session.receiveDelta(delta([]))).toBe(false);
    await tick();
    expect(sent.filter((r) => r.request === "snapshot")).toHaveLength(1);
    expect(sent.filter((r) => r.request === "event")).toHaveLength(1);
  });
  it("suspends hidden sends, resumes and reconnects with a fresh snapshot, rejects stale epochs", async () => {
    const { session, sent } = opened(); await tick();
    session.setVisible(false);
    expect(session.receiveDelta(delta([]))).toBe(false);
    await expect(session.event("button", "callback", "press", "")).rejects.toThrow("not ready");
    expect(session.receiveSnapshot(snap())).toBe(false);
    session.setVisible(true); await tick();
    expect(sent.map((r) => r.request)).toEqual(["open", "suspend", "resume"]);
    expect(session.receiveDelta(delta([]))).toBe(false);
    expect(session.receiveSnapshot({ ...snap([button(), field]), epoch: "epoch-b" })).toBe(true);
    expect(session.receiveSnapshot(snap([button(), field]))).toBe(false);
    session.setConnected(false); session.setConnected(true); await tick();
    expect(session.receiveDelta({ ...delta([]), epoch: "epoch-b" })).toBe(false);
    expect(session.receiveSnapshot({ ...snap([button(), field]), epoch: "epoch-b" })).toBe(true);
    expect(session.receiveDelta({ ...delta([]), epoch: "epoch-a" })).toBe(false);
  });
  it("bounds pending callbacks and request bytes, never submits secret fields", async () => {
    let release!: () => void;
    const { session, sent } = opened(async (r) => { if (r.request === "event") await new Promise<void>((r) => { release = r; }); });
    await tick();
    const promises = Array.from({ length: 64 }, () => session.event("button", "callback", "press", null).catch((e) => e));
    await expect(session.event("button", "callback", "press", null)).rejects.toThrow("limit");
    await tick(); expect(sent.filter((r) => r.request === "event")).toHaveLength(1);
    release(); await Promise.all(promises);
    const test = opened(undefined, snap([button(), { type: "field", key: "secret", name: "credential", label: "Secret", kind: "secret" }]));
    await expect(test.session.event("button", "callback", "press", { credential: "never" })).rejects.toThrow("Secret");
    await expect(test.session.event("button", "callback", "press", Array.from({ length: 64 }, () => "x".repeat(4096)))).rejects.toThrow("byte limit");
  });
  it.each([null, false, 0, "", [], {}].map((value) => ({ value })))("preserves interaction value $value", async ({ value }) => {
    const { session, sent } = opened(); await session.event("button", "callback", "press", value);
    expect(sent.find((r) => r.request === "event")).toMatchObject({ interaction: { value } });
  });
  it("isolates owner, group, extension, contribution and navigation identities", () => {
    const { options } = setup(); const pages = new ExtensionUIStreamPages();
    expect(pages.available(undefined)).toBeUndefined();
    const a = pages.available(options)!;
    expect(pages.available({ ...options })).toBe(a);
    for (const field of ["owner", "hubGroup", "extension", "contribution", "navigation"] as const) {
      const identity = { ...options.identity, [field]: "other" };
      const b = pages.available({ ...options, identity })!;
      expect(b).not.toBe(a); expect(b.pageKey).not.toBe(a.pageKey);
      b.open(); b.receiveSnapshot(snap([button(), fieldComponent()]));
      expect(b.getDraft("field")).toBeUndefined();
    }
    expect(streamPageKey({ ...options.identity, owner: "a/b", hubGroup: "c" })).not.toBe(streamPageKey({ ...options.identity, owner: "a", hubGroup: "b/c" }));
    pages.closeAll(); expect(a.getState().phase).toBe("closed");
  });
  it("bounds stalled lifecycle work, coalesces overflow into read-only recovery and times out sends", async () => {
    vi.useFakeTimers();
    const { session, sent } = opened(() => new Promise<void>(() => {}));
    for (let i = 0; i < 20; i++) { session.setVisible(false); session.setVisible(true); }
    expect(sent.length).toBeLessThanOrEqual(8);
    expect(session.getState().pending).toBeLessThanOrEqual(8);
    await vi.advanceTimersByTimeAsync(STREAM_LIMITS.delivery + 1);
    expect(sent.some((r) => r.request === "snapshot")).toBe(true);
    session.close();
    expect(session.getState().pending).toBe(0);
    await vi.advanceTimersByTimeAsync(STREAM_LIMITS.delivery + 1);
  });
  it("cancels pending promises on close and ignores a late transport completion", async () => {
    let finish!: () => void;
    const { session, sent } = opened(async (r) => { if (r.request === "event") await new Promise<void>((resolve) => { finish = resolve; }); });
    const event = session.event("button", "callback", "press", null);
    const rejected = expect(event).rejects.toThrow("closed");
    session.close(); await rejected;
    expect(session.getState()).toMatchObject({ phase: "closed", pending: 0, snapshot: null });
    finish(); await tick();
    expect(session.getState().phase).toBe("closed");
    expect(sent.filter((r) => r.request === "event")).toHaveLength(1);
  });
  it("never sends queued callbacks after the page becomes hidden", async () => {
    let finish!: () => void;
    const { session, sent } = opened(async (r) => { if (r.request === "event") await new Promise<void>((resolve) => { finish = resolve; }); });
    const first = session.event("button", "callback", "press", null);
    const next = session.event("button", "callback", "press", null);
    const rejected = expect(next).rejects.toThrow("Visibility");
    session.setVisible(false); await rejected;
    finish(); await first; await tick();
    expect(sent.filter((r) => r.request === "event")).toHaveLength(1);
  });
  it("preserves arbitrary scalar map zero fields rather than treating them as envelope versions", async () => {
    const { session, sent } = opened();
    await session.event("button", "callback", "press", { version: 0, base_version: false, empty: "" });
    session.setLocal("button", { version: 0 });
    expect(session.getLocal("button")).toEqual({ version: 0 });
    expect(sent.find((r) => r.request === "event")).toMatchObject({ interaction: { value: { version: 0, base_version: false, empty: "" } } });
  });
  it("discards drafts on an incompatible same-key field or secret-kind replacement", () => {
    const { session } = opened(); session.setDraft("field", "Do not migrate this draft");
    expect(session.receiveDelta(delta([{ op: "replace", key: "field", item: { type: "field", key: "field", name: "new", kind: "secret", label: "Secret" } }]))).toBe(true);
    expect(session.getDraft("field")).toBeUndefined();
  });
  it("does not treat a conflicting same-version snapshot as a new authoritative tree", () => {
    const { session } = opened();
    const old = session.getState().snapshot;
    expect(session.receiveSnapshot(snap([button()]))).toBe(false);
    expect(session.getState().snapshot).toBe(old);
  });
  it("closes idempotently and ignores late frames", async () => {
    const { session, sent } = opened(); session.close(); session.close(); await tick();
    expect(sent.filter((r) => r.request === "close")).toHaveLength(1);
    expect(session.receiveSnapshot(snap())).toBe(false); expect(session.receiveDelta(delta([]))).toBe(false);
    expect(session.getState().display).toBeNull();
  });
});
function fieldComponent(): Component { return { ...field }; }
