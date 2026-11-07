import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  applyAction, applyBlock, applySnapshot, applyView, conversationCards, getCards, getDraft, resetCards, setHub, submitAction, updateDraft,
} from "./assistant-views";
import type { AssistantView, ViewAction } from "./extension-wire";

const view = (id: string, version: number, title = `v${version}`): AssistantView => ({
  id, conversation: "c1", version, view: { title, fallback: "fb", sections: [] } as unknown as AssistantView["view"], created_at: "2026-11-05T00:00:00Z", updated_at: "2026-11-05T00:00:00Z",
});
const action = (key: string, cid: string, revision: number, status = "pending"): ViewAction => ({ key, action: "b", client_msg_id: cid, status, revision, at: "2026-11-05T00:00:00Z" });

function respond(...bodies: { status: number; body: unknown }[]) {
  const fetcher = vi.fn(async () => {
    const r = bodies.shift() ?? { status: 500, body: { error: "no more" } };
    return new Response(JSON.stringify(r.body), { status: r.status, headers: { "Content-Type": "application/json" } });
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
const sentBody = (f: ReturnType<typeof respond>, i: number) => JSON.parse(String((f.mock.calls[i] as unknown as [string, RequestInit])[1].body));

beforeEach(() => {
  resetCards("hub-a");
  vi.stubGlobal("localStorage", { getItem: () => "t", setItem: () => {}, removeItem: () => {} });
});
afterEach(() => vi.unstubAllGlobals());

describe("活卡片和回答的顺序", () => {
  it("keeps the newest version; an older or equal one is dropped", () => {
    applyView("c1", view("view_1", 2, "新"));
    applyView("c1", view("view_1", 1, "旧"));
    applyView("c1", view("view_1", 2, "同版本"));
    expect(conversationCards("c1").views.view_1.view.title).toBe("新");
    applyView("c1", view("view_1", 3, "更新"));
    expect(conversationCards("c1").views.view_1.version).toBe(3);
  });

  it("an event naming another conversation does not land here", () => {
    applyView("c2", view("view_1", 1));
    expect(conversationCards("c2").views.view_1).toBeUndefined();
  });

  it("follows revisions, never status: a late pending does not undo sent", () => {
    applyAction("c1", action("k", "cid-1", 2, "sent"));
    applyAction("c1", action("k", "cid-1", 1, "pending"));
    expect(conversationCards("c1").actions.k.status).toBe("sent");
    applyAction("c1", action("k", "cid-1", 3, "failed"));
    expect(conversationCards("c1").actions.k.status).toBe("failed");
  });

  it("a snapshot adds what events missed and takes nothing away", () => {
    applyBlock("c1", "item-1/0");
    // the feed's shape (docs/protocol/protocol.md): view_actions keyed by card, {} when there are none
    applySnapshot("c1", { views: { view_1: view("view_1", 4) }, view_actions: { view_1: action("view_1", "x", 1) }, view_blocks: ["item-2/0"] });
    applySnapshot("c1", { views: {}, view_actions: {}, view_blocks: [] });
    const c = conversationCards("c1");
    expect(Object.keys(c.blocks).sort()).toEqual(["item-1/0", "item-2/0"]);
    expect(c.views.view_1.version).toBe(4);
    expect(c.actions.view_1.client_msg_id).toBe("x");
  });

  it("still takes the answers as a list", () => {
    applySnapshot("c1", { view_actions: [action("k", "y", 1)] });
    expect(conversationCards("c1").actions.k.client_msg_id).toBe("y");
  });
});

describe("多个 hub", () => {
  it("another hub's cards and drafts never show; the first hello adopts what was read before it", () => {
    resetCards("");
    applyBlock("c1", "i/0");
    updateDraft("c1", "i/0", (d) => ({ ...d, values: { a: "1" } }));
    setHub("hub-a");
    expect(conversationCards("c1").blocks["i/0"]).toBe(true);
    expect(getDraft("c1", "i/0")?.values).toEqual({ a: "1" });
    setHub("hub-b");
    expect(conversationCards("c1").blocks).toEqual({});
    expect(getDraft("c1", "i/0")).toBeUndefined();
    expect(getCards().hub).toBe("hub-b");
  });
});

describe("提交回答", () => {
  const req = { action: "b", page: "", values: { task: "写周报" }, expectedVersion: 3, confirmed: true };

  it("sends the version shown and the key escaped once", async () => {
    const f = respond({ status: 200, body: { action: action("view_1", "will-be-replaced", 1) } });
    expect(await submitAction("c 1", "item#1/0", req)).toBe("sent");
    expect((f.mock.calls[0] as unknown as [string])[0]).toBe("/api/v1/assistant/conversations/c%201/views/item%231%2F0/actions");
    const body = sentBody(f, 0);
    expect(body).toMatchObject({ action: "b", values: { task: "写周报" }, expected_version: 3, confirmed: true });
    expect(body.client_msg_id).toEqual(expect.any(String));
    expect(body).not.toHaveProperty("reply");
  });

  it("retries a failed submit with the same client_msg_id, once at a time", async () => {
    const f = respond({ status: 503, body: { error: "down" } }, { status: 200, body: { action: action("k", "x", 1) } });
    expect(await submitAction("c1", "k", req)).toBe("failed");
    expect(getDraft("c1", "k")?.sending?.inFlight).toBe(false);
    const first = sentBody(f, 0).client_msg_id;
    const again = submitAction("c1", "k", req);
    expect(await submitAction("c1", "k", req)).toBe("busy"); // one in flight: no second request
    expect(await again).toBe("sent");
    expect(f).toHaveBeenCalledTimes(2);
    expect(sentBody(f, 1).client_msg_id).toBe(first);
  });

  it("a different request (other values) is a new client_msg_id", async () => {
    const f = respond({ status: 503, body: { error: "down" } }, { status: 503, body: { error: "down" } });
    await submitAction("c1", "k", req);
    await submitAction("c1", "k", { ...req, values: { task: "别的" } });
    expect(sentBody(f, 0).client_msg_id).not.toBe(sentBody(f, 1).client_msg_id);
  });

  it("409 view_changed: keeps the draft, shows the newer card, asks again — never resends by itself", async () => {
    applyView("c1", view("view_1", 3));
    updateDraft("c1", "view_1", (d) => ({ ...d, values: { task: "草稿" }, stack: ["detail"] }));
    const f = respond({ status: 409, body: { error: "changed", code: "view_changed", version: 4, view: view("view_1", 4, "新版") } });
    expect(await submitAction("c1", "view_1", req)).toBe("changed");
    expect(f).toHaveBeenCalledTimes(1);
    const d = getDraft("c1", "view_1")!;
    expect(d.values).toEqual({ task: "草稿" });
    expect(d.stack).toEqual(["detail"]);
    expect(d.changed).toBe(true);
    expect(d.sending).toBeUndefined();
    expect(conversationCards("c1").views.view_1.view.title).toBe("新版");
  });

  it("409 already_answered: the other device's answer shows", async () => {
    respond({ status: 409, body: { error: "answered", code: "already_answered", action: action("k", "other", 2, "sent") } });
    expect(await submitAction("c1", "k", req)).toBe("answered_elsewhere");
    expect(conversationCards("c1").actions.k.client_msg_id).toBe("other");
    expect(getDraft("c1", "k")?.elsewhere).toBe(true);
  });

  it("an answered card is not submitted again", async () => {
    applyAction("c1", action("k", "x", 1));
    const f = respond();
    expect(await submitAction("c1", "k", req)).toBe("answered_elsewhere");
    expect(f).not.toHaveBeenCalled();
  });

  it("a request the hub refused (400) is not kept for 重试", async () => {
    respond({ status: 400, body: { error: "values.x: field is not allowed by this action" } });
    expect(await submitAction("c1", "k", req)).toBe("failed");
    expect(getDraft("c1", "k")).toMatchObject({ error: "values.x: field is not allowed by this action", sending: undefined });
  });

  it("the broadcast of our own answer ends the submit without marking it elsewhere", async () => {
    let release: (r: Response) => void = () => {};
    vi.stubGlobal("fetch", vi.fn(() => new Promise<Response>((r) => (release = r))));
    await Promise.resolve();
    const p = submitAction("c1", "k", req);
    const cid = getDraft("c1", "k")!.sending!.clientMsgId;
    applyAction("c1", action("k", cid, 1));
    expect(getDraft("c1", "k")?.elsewhere).toBe(false);
    release(new Response(JSON.stringify({ action: action("k", cid, 1) }), { status: 200 }));
    expect(await p).toBe("sent");
  });
});

describe("和公共 schema 的请求示例一致（#2604）", () => {
  const ex = (name: string) =>
    JSON.parse(Object.entries(import.meta.glob(["../../../schema/extension/examples/**/ViewActionInput.*.json", "../../../schema/extension/examples/**/ViewChangedError.*.json"], { query: "?raw", import: "default", eager: true }) as Record<string, string>).find(([p]) => p.endsWith(`/${name}.json`))![1]);

  it("a confirmed submit is ViewActionInput.confirmed, field for field", async () => {
    const f = respond({ status: 200, body: { action: action("view_1", "x", 1) } });
    const want = ex("ViewActionInput.confirmed");
    await submitAction("c1", "view_1", { action: want.action, page: "", values: {}, expectedVersion: want.expected_version, confirmed: true });
    const got = sentBody(f, 0);
    expect({ ...got, client_msg_id: want.client_msg_id }).toEqual(want);
  });

  it("never sends without a version the schema takes (minimum 1, an integer)", async () => {
    expect(ex("invalid/ViewActionInput.zero-version").expected_version).toBe(0);
    const f = respond();
    for (const v of [0, -1, 1.5, Number.NaN, Number.MAX_SAFE_INTEGER + 1]) {
      resetCards("hub-a");
      expect(await submitAction("c1", "k", { action: "b", page: "", values: {}, expectedVersion: v, confirmed: false })).toBe("failed");
    }
    expect(f).not.toHaveBeenCalled();
  });

  it("the ViewChangedError example redraws its live card, keeps the draft, sends nothing more", async () => {
    const err = ex("ViewChangedError.example");
    applyView("conversation-example", { ...err.view, version: 3 });
    updateDraft("conversation-example", "view_example", (d) => ({ ...d, values: { a: "草稿" } }));
    const f = respond({ status: 409, body: err });
    expect(await submitAction("conversation-example", "view_example", { action: "b", page: "", values: {}, expectedVersion: 3, confirmed: true })).toBe("changed");
    expect(conversationCards("conversation-example").views.view_example.version).toBe(4);
    expect(getDraft("conversation-example", "view_example")).toMatchObject({ values: { a: "草稿" }, changed: true });
    expect(f).toHaveBeenCalledTimes(1);
  });

  it("a view_changed for a text block does not put a live card under its key", async () => {
    respond({ status: 409, body: { error: "changed", code: "view_changed", version: 1, view: view("other", 9) } });
    expect(await submitAction("c1", "item-1/0", { action: "b", page: "", values: {}, expectedVersion: 1, confirmed: false })).toBe("changed");
    expect(conversationCards("c1").views).toEqual({});
  });
});
