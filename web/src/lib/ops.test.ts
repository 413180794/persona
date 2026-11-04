import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./api";
import { apiOp, isTransient, OP_HEADER, OP_RETRY_MS, untilDelivered, type RetryClock } from "./ops";

function response(body: unknown, status = 200, json = true) {
  return { ok: status < 400, status, text: async () => (json ? JSON.stringify(body) : String(body)) };
}

/** A clock the test moves: every wait passes at once and is recorded. */
function fakeClock(): RetryClock & { waits: number[] } {
  let t = 0;
  const waits: number[] = [];
  return {
    waits,
    now: () => t,
    wait: async (ms) => {
      waits.push(ms);
      t += ms;
    },
  };
}

beforeEach(() => vi.stubGlobal("localStorage", { getItem: () => "tok" }));
afterEach(() => vi.unstubAllGlobals());

describe("what counts as a gap", () => {
  it("a failed fetch and a proxy's 502/503/504 are gaps; the hub's own answers are not", () => {
    expect(isTransient(new TypeError("Failed to fetch"))).toBe(true);
    expect(isTransient(new ApiError(502, "HTTP 502", { error: "<html>" }, false))).toBe(true);
    expect(isTransient(new ApiError(503, "HTTP 503", {}, true))).toBe(true);
    expect(isTransient(new ApiError(502, "offline", { code: "node_unreachable" }))).toBe(false);
    expect(isTransient(new ApiError(504, "timeout", { error: "timeout", delivered: "unknown" }))).toBe(false);
    expect(isTransient(new ApiError(500, "boom", {}, false))).toBe(false);
    expect(isTransient(new ApiError(409, "draft", { code: "input_draft" }))).toBe(false);
    expect(isTransient(new DOMException("aborted", "AbortError"))).toBe(false);
  });
});

describe("a write through a gap", () => {
  it("is sent again with the same op id until it goes", async () => {
    const fetcher = vi.fn()
      .mockRejectedValueOnce(new TypeError("Failed to fetch"))
      .mockResolvedValueOnce(response("Bad Gateway", 502, false))
      .mockResolvedValueOnce(response({ ok: true }));
    vi.stubGlobal("fetch", fetcher);
    const clock = fakeClock();
    await expect(apiOp("/interactions/i1/resolve", { method: "POST", opId: "op-1", json: { answer: "yes" } }, clock)).resolves.toEqual({ ok: true });
    expect(fetcher).toHaveBeenCalledTimes(3);
    const ids = fetcher.mock.calls.map((c) => (c[1].headers as Headers).get(OP_HEADER));
    expect(ids).toEqual(["op-1", "op-1", "op-1"]);
    expect(fetcher.mock.calls.every((c) => c[1].body === JSON.stringify({ answer: "yes" }))).toBe(true);
    expect(clock.waits).toEqual([500, 1000]);
  });

  it("makes one op id when none is given and keeps it for its retries", async () => {
    const fetcher = vi.fn().mockRejectedValueOnce(new TypeError("Failed to fetch")).mockResolvedValueOnce(response({}));
    vi.stubGlobal("fetch", fetcher);
    await apiOp("/sessions/m/s/interrupt", { method: "POST" }, fakeClock());
    const [a, b] = fetcher.mock.calls.map((c) => (c[1].headers as Headers).get(OP_HEADER));
    expect(a).toMatch(/^[0-9a-f-]{36}$/);
    expect(b).toBe(a);
  });

  it("gives up after a minute with the last error", async () => {
    const fetcher = vi.fn().mockRejectedValue(new TypeError("Failed to fetch"));
    vi.stubGlobal("fetch", fetcher);
    const clock = fakeClock();
    await expect(apiOp("/x", { method: "POST", opId: "op-2" }, clock)).rejects.toBeInstanceOf(TypeError);
    expect(clock.now()).toBe(OP_RETRY_MS);
    expect(clock.waits.slice(0, 4)).toEqual([500, 1000, 2000, 4000]);
    expect(new Set(fetcher.mock.calls.map((c) => (c[1].headers as Headers).get(OP_HEADER)))).toEqual(new Set(["op-2"]));
  });

  it("does not retry an answer from the hub", async () => {
    const run = vi.fn().mockRejectedValue(new ApiError(409, "draft", { code: "input_draft" }));
    const clock = fakeClock();
    await expect(untilDelivered(run, clock)).rejects.toBeInstanceOf(ApiError);
    expect(run).toHaveBeenCalledTimes(1);
    expect(clock.waits).toEqual([]);
  });
});
