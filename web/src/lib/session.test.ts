import { afterEach, describe, expect, it, vi } from "vitest";
import { api, ApiError, attachSession } from "./api";
import { resumeCommand, sessionCan, sessionMenuActions, sourceSubtitle } from "./format";
import type { Session, SessionCapabilities } from "./types";

const legacy: Session = { machine_id: "m", session_id: "raw-thread", title: "", cwd: "/tmp/project", status: "working" };
const operations: (keyof SessionCapabilities)[] = ["read", "send", "interrupt", "approve", "terminal"];
const history: Session = { ...legacy, source_kind: "codex_history", capabilities: { read: true }, read_only_reason: "外部进程正在使用此会话" };
const live: Session = { ...legacy, source_kind: "codex_app_server", source_thread_id: "real-thread", source_instance_id: "instance", parent_thread_id: "parent", capabilities: { read: true, send: true, attach: true } };

afterEach(() => vi.unstubAllGlobals());

describe("session capability contract", () => {
  it("keeps legacy behavior without guessing from IDs or missing metadata", () => {
    for (const op of operations) expect(sessionCan(legacy, op)).toBe(true);
    expect(sessionCan({ ...legacy, session_id: "aabbccddeeff" }, "send")).toBe(true);
    expect(sessionCan({ ...legacy, source_kind: "" }, "send")).toBe(true);
    expect(sessionCan(undefined, "send")).toBe(false);
    expect(sessionCan(legacy, "attach")).toBe(false);
    expect(sourceSubtitle(legacy)).toBe("");
  });

  it("denies new sources without capabilities and denies omitted flags", () => {
    for (const op of operations) expect(sessionCan({ ...live, capabilities: undefined }, op)).toBe(false);
    expect(sessionCan({ ...live, capabilities: undefined }, "attach")).toBe(false);
    expect(sessionCan(history, "read")).toBe(true);
    for (const op of operations.filter((op) => op !== "read")) expect(sessionCan(history, op)).toBe(false);
    expect(sourceSubtitle(history)).toContain("仅查看：外部进程正在使用此会话");
  });

  it("offers attach but not terminal, close or interrupt until explicitly granted", () => {
    expect(sessionMenuActions(history)).toEqual([]);
    expect(sessionMenuActions(live)).toEqual(["attach"]);
    expect(sessionMenuActions({ ...live, attached: true })).toEqual([]);
    expect(sessionMenuActions({ ...live, capabilities: { ...live.capabilities, interrupt: true } })).toEqual(["attach", "stop"]);
    expect(sessionMenuActions({ ...live, status: "idle", capabilities: { ...live.capabilities, interrupt: true } })).toEqual(["attach"]);
    expect(sessionMenuActions(legacy)).toEqual(["stop", "resume", "close"]);
    expect(resumeCommand(live)).toBe("cd /tmp/project && codex resume real-thread");
  });

  it("names a runtime's session by its agent_label and offers only what it declared", () => {
    const dsh: Session = { ...legacy, agent: "dsh", agent_label: "DSH", source_kind: "runtime", source_instance_id: "dsh",
      capabilities: { read: true, send: true, interrupt: true, approve: true } };
    expect(sourceSubtitle(dsh)).toBe("DSH");
    expect(sessionMenuActions(dsh)).toEqual(["stop"]);
    expect(sourceSubtitle({ ...dsh, capabilities: { read: true }, read_only_reason: "DSH 没有开放从手机发消息" })).toBe("DSH · 仅查看：DSH 没有开放从手机发消息");
  });
});

describe("attach API", () => {
  it("posts once to the encoded session attach route", async () => {
    vi.stubGlobal("localStorage", { getItem: () => "device-token" });
    const fetch = vi.fn().mockResolvedValue(new Response("{}", { status: 200 }));
    vi.stubGlobal("fetch", fetch);
    await attachSession("machine/id", "opaque-id");
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fetch.mock.calls[0][0]).toBe("/api/v1/sessions/machine%2Fid/opaque-id/attach");
    expect(fetch.mock.calls[0][1].method).toBe("POST");
  });

  it("preserves unknown delivery failures without automatically resending", async () => {
    vi.stubGlobal("localStorage", { getItem: () => "device-token" });
    const fetch = vi.fn().mockResolvedValue(new Response('{"error":"may have arrived","delivered":"unknown"}', { status: 502 }));
    vi.stubGlobal("fetch", fetch);
    const error = await api("/sessions/m/opaque-id/messages", { method: "POST", json: { text: "继续", client_msg_id: "message-id" } }).catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).body.delivered).toBe("unknown");
    expect(fetch).toHaveBeenCalledTimes(1);
  });
});
