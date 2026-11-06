// Listing the operations again (on opening, after a reconnect, #4574): what the hub pushed or this page started while
// the list was on its way is not undone by the list, which the hub read before; of two lists only the later one counts.
import { beforeEach, describe, expect, it, vi } from "vitest";

const answers: ((v: unknown) => void)[] = [];
vi.mock("./api", async (orig) => ({ ...(await orig<typeof import("./api")>()), api: vi.fn(() => new Promise((resolve) => answers.push(resolve))) }));
vi.mock("./extensions", async (orig) => ({ ...(await orig<typeof import("./extensions")>()), changed: vi.fn((r) => r) }));
vi.mock("./store", async (orig) => ({ ...(await orig<typeof import("./store")>()), hubHas: () => true }));

import { forget, operations, reloadOperations, track, type ExtensionOperation } from "./extension-operations";

const op = (over: Partial<ExtensionOperation> = {}): ExtensionOperation => ({
  id: "op1", kind: "install", extension: "larkext", state: "running", step: "download", steps: [], retryable: false,
  started_at: "2026-11-06T00:00:00Z", updated_at: "2026-11-06T00:00:01Z", ...over,
});
const list = (n: number, ...ops: ExtensionOperation[]) => answers[n]({ operations: ops });
const state = (id: string) => operations().find((o) => o.id === id)?.state;

beforeEach(() => {
  answers.length = 0;
  for (const o of operations()) forget(o.id);
});

describe("reloadOperations", () => {
  it("keeps a push that came while the list was out", async () => {
    track(op());
    const p = reloadOperations();
    track(op({ state: "succeeded", updated_at: "2026-11-06T00:00:05Z" }));
    list(0, op());
    await p;
    expect(state("op1")).toBe("succeeded");
  });

  it("keeps an operation started while the list was out", async () => {
    const p = reloadOperations();
    track(op({ id: "op2" }));
    list(0);
    await p;
    expect(state("op2")).toBe("running");
  });

  it("takes the list's copy when it is the newer one, and drops what the hub no longer has", async () => {
    track(op());
    track(op({ id: "old" }));
    const p = reloadOperations();
    track(op({ updated_at: "2026-11-06T00:00:02Z", step: "verify" }));
    list(0, op({ state: "failed", updated_at: "2026-11-06T00:00:09Z" }));
    await p;
    expect(state("op1")).toBe("failed");
    expect(state("old")).toBeUndefined();
  });

  it("uses only the later of two lists", async () => {
    const first = reloadOperations();
    const second = reloadOperations();
    list(1, op({ state: "succeeded", updated_at: "2026-11-06T00:00:05Z" }));
    await second;
    list(0, op());
    await first;
    expect(state("op1")).toBe("succeeded");
  });

  it("uses the earlier list when the later one fails", async () => {
    const first = reloadOperations();
    const second = reloadOperations();
    list(0, op({ state: "needs_consent", updated_at: "2026-11-06T00:00:03Z" }));
    await first;
    answers[1](Promise.reject(new Error("hub away")));
    await second;
    expect(state("op1")).toBe("needs_consent");
  });

  it("keeps a push for the earlier list too, when two are out", async () => {
    track(op());
    const first = reloadOperations();
    track(op({ state: "succeeded", updated_at: "2026-11-06T00:00:05Z" }));
    const second = reloadOperations();
    list(0, op());
    await first;
    list(1, op({ state: "succeeded", updated_at: "2026-11-06T00:00:05Z" }));
    await second;
    expect(state("op1")).toBe("succeeded");
  });
});
