import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./store", () => {
  const state = {
    outbox: {} as Record<string, import("./outbox").OutboxEntry[]>,
    assistant: null as null | { machine_id: string; main?: string; conversations: unknown[] },
  };
  return {
    getState: () => state,
    setOutbox: (key: string, list: import("./outbox").OutboxEntry[]) => {
      if (list.length) state.outbox[key] = list;
      else delete state.outbox[key];
    },
    refreshAssistant: vi.fn(async () => {}),
  };
});
vi.mock("./router", () => ({ navigate: vi.fn() }));

import { arrived, askPersona, discardOutbox, noteSent, outboxMessage, retryOutbox, sendToSession, settle, settleOutbox, SENT_GRACE_MS, type OutboxEntry } from "./outbox";
import { pickLauncher } from "./assistant-chat";
import { navigate } from "./router";
import { getState } from "./store";

function response(body: unknown, status = 200) {
  return { ok: status < 400, status, text: async () => JSON.stringify(body) };
}
const outbox = () => getState().outbox as Record<string, OutboxEntry[]>;
const entry = (patch: Partial<OutboxEntry> = {}): OutboxEntry => ({ id: "e1", text: "你好", images: [], at: 1000, state: "sent", ...patch });

beforeEach(() => {
  vi.stubGlobal("localStorage", { getItem: () => "test" });
  for (const k of Object.keys(outbox())) delete outbox()[k];
  (getState() as { assistant: unknown }).assistant = null;
  vi.clearAllMocks();
});
afterEach(() => vi.unstubAllGlobals());

describe("which bubbles stay", () => {
  it("goes once the transcript says the same, with as many images", () => {
    expect(arrived(entry(), [{ text: "你好", images: [] }])).toBe(true);
    expect(arrived(entry({ images: [{ mime: "image/png", data: "x" }] }), [{ text: "你好", images: [] }])).toBe(false);
    expect(arrived(entry({ text: '<jarvis-context kind="context" title="t" url="">x</jarvis-context>\n\n说说' }), [{ text: "说说", images: [] }])).toBe(true);
  });

  it("keeps failed ones, and sent ones only for a while", () => {
    const users = [{ text: "别的", images: [] }];
    const list = [entry({ id: "a" }), entry({ id: "b", state: "failed" }), entry({ id: "c", state: "sending" }), entry({ id: "d", at: 1000 - SENT_GRACE_MS })];
    expect(settle(list, users, 1000).map((e) => e.id)).toEqual(["a", "b", "c"]);
    const copies = ["u1", "u2", "u3"].map((id) => ({ id, text: "你好", images: [] }));
    expect(settle(list, copies, 1000).map((e) => e.id)).toEqual(["b"]);
  });

  it("takes one bubble per copy, never an older message's copy, and a copy only once", () => {
    const list = [entry({ id: "a", at: 1_000_000 }), entry({ id: "b", at: 1_001_000 })];
    const older = { id: "u0", text: "你好", images: [], ts: new Date(1_000_000 - 600_000).toISOString() };
    const copy = { id: "u1", text: "你好", images: [], ts: new Date(1_000_500).toISOString() };
    expect(settle(list, [older], 1_002_000).map((e) => e.id)).toEqual(["a", "b"]);
    const claimed = new Set<string>();
    expect(settle(list, [older, copy], 1_002_000, claimed).map((e) => e.id)).toEqual(["b"]);
    expect(settle(list.slice(1), [older, copy], 1_003_000, claimed).map((e) => e.id)).toEqual(["b"]);
  });

  it("shows contexts as cards and images as images", () => {
    const m = outboxMessage(entry({ text: '<jarvis-context kind="context" title="项目" url="">档案</jarvis-context>\n\n问题', images: [{ mime: "image/png", data: "AQID" }] }));
    expect(m.text).toBe("问题");
    expect(m.contexts).toEqual([{ kind: "context", title: "项目", url: "", text: "档案" }]);
    expect(m.images[0]).toMatchObject({ kind: "image", role: "user", image: { mime: "image/png", data: "AQID" } });
  });
});

describe("sending from another page", () => {
  it("shows the message at once and marks it sent", async () => {
    let resolve!: (v: unknown) => void;
    const fetcher = vi.fn().mockReturnValueOnce(new Promise((r) => (resolve = r)));
    vi.stubGlobal("fetch", fetcher);
    const job = sendToSession("m", "s", { text: "看一下" }, { launcher: "codex" });
    expect(outbox()["m/s"]).toHaveLength(1);
    expect(outbox()["m/s"][0].state).toBe("sending");
    const body = JSON.parse(fetcher.mock.calls[0][1].body);
    expect(fetcher.mock.calls[0][0]).toBe("/api/v1/sessions/m/s/messages");
    expect(body).toMatchObject({ text: "看一下", launcher: "codex", client_msg_id: outbox()["m/s"][0].id });
    resolve(response({ ok: true }));
    expect(await job).toBe(true);
    expect(outbox()["m/s"][0].state).toBe("sent");
    settleOutbox("m", "s", [{ text: "看一下", images: [] }]);
    expect(outbox()["m/s"]).toBeUndefined();
  });

  it("keeps a failure with why, retries with the same id, and can drop it", async () => {
    const fetcher = vi.fn()
      .mockResolvedValueOnce(response({ code: "input_draft", error: "draft", clearable: "old" }, 409))
      .mockResolvedValueOnce(response({ ok: true }));
    vi.stubGlobal("fetch", fetcher);
    expect(await sendToSession("m", "s", { text: "看一下" })).toBe(false);
    const failed = outbox()["m/s"][0];
    expect(failed).toMatchObject({ state: "failed", error: "电脑上的输入框里有没发出去的字", clearable: "old" });
    expect(await retryOutbox("m", "s", failed.id, failed.clearable)).toBe(true);
    const again = JSON.parse(fetcher.mock.calls[1][1].body);
    expect(again.client_msg_id).toBe(failed.id);
    expect(again.clear_draft).toBe("old");
    discardOutbox("m", "s", failed.id);
    expect(outbox()["m/s"]).toBeUndefined();
  });

  it("retries as a new message when the hub could not tell whether it went in", async () => {
    const fetcher = vi.fn().mockResolvedValueOnce(response({ error: "timeout", delivered: "unknown" }, 504)).mockResolvedValueOnce(response({ ok: true }));
    vi.stubGlobal("fetch", fetcher);
    await sendToSession("m", "s", { text: "x" });
    const id = outbox()["m/s"][0].id;
    await retryOutbox("m", "s", id);
    expect(JSON.parse(fetcher.mock.calls[1][1].body).client_msg_id).not.toBe(id);
  });

  it("notes a prompt that started a conversation as sent", () => {
    noteSent("m", "new", { text: "开始", images: [{ mime: "image/png", data: "x" }] });
    expect(outbox()["m/new"][0]).toMatchObject({ state: "sent", text: "开始" });
  });
});

describe("问 Persona", () => {
  it("opens the main conversation at once and sends there, with its picked launcher", async () => {
    (getState() as { assistant: unknown }).assistant = { machine_id: "hub", main: "main1", conversations: [] };
    pickLauncher("hub/main1", "codex");
    const fetcher = vi.fn().mockResolvedValueOnce(response({ ok: true }));
    vi.stubGlobal("fetch", fetcher);
    await askPersona("说说进展");
    expect(navigate).toHaveBeenCalledWith({ name: "jarvis", sid: "main1" });
    expect(outbox()["hub/main1"][0].text).toBe("说说进展");
    expect(JSON.parse(fetcher.mock.calls[0][1].body)).toMatchObject({ text: "说说进展", launcher: "codex" });
    pickLauncher("hub/main1", undefined);
  });

  it("starts the main conversation when there is none, and shows the prompt there", async () => {
    const fetcher = vi.fn().mockResolvedValueOnce(response({ session: { machine_id: "hub", session_id: "new1" } }));
    vi.stubGlobal("fetch", fetcher);
    await askPersona("说说进展");
    expect(fetcher.mock.calls[0][0]).toBe("/api/v1/assistant/conversations");
    expect(JSON.parse(fetcher.mock.calls[0][1].body)).toEqual({ prompt: "说说进展", main: true });
    expect(outbox()["hub/new1"][0]).toMatchObject({ state: "sent", text: "说说进展" });
    expect(navigate).toHaveBeenCalledWith({ name: "jarvis", sid: "new1" });
  });
});
