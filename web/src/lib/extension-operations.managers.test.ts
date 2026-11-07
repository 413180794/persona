// Several managers' installs (docs/assistant/managers.md "Clients"): an operation is its manager's. A push says whose, one this
// page starts is the manager it named, and every later request about it (its preview, retry, cancel) names that one;
// the hub's list is the default's, so it leaves the others' as their pushes left them.
import { beforeEach, describe, expect, it, vi } from "vitest";

const sent: { path: string; init: { method?: string; headers?: Record<string, string> } }[] = [];
const answers: ((v: unknown) => void)[] = [];
vi.mock("./api", async (orig) => ({
  ...(await orig<typeof import("./api")>()),
  api: vi.fn((path: string, init: { method?: string; headers?: Record<string, string> } = {}) => {
    sent.push({ path, init });
    return new Promise((resolve) => answers.push(resolve));
  }),
}));
vi.mock("./extensions", async (orig) => ({ ...(await orig<typeof import("./extensions")>()), changed: vi.fn((r) => r) }));
vi.mock("./store", async (orig) => ({ ...(await orig<typeof import("./store")>()), hubHas: () => true }));

import { cancelOperation, forget, getOperation, latestFor, operations, reloadOperations, retryOperation, startFromMarket, track, type ExtensionOperation } from "./extension-operations";
import { resetManagerChoice, setRosterDefault } from "./manager-choice";

const op = (over: Partial<ExtensionOperation> = {}): ExtensionOperation => ({
  id: "op1", kind: "upgrade", extension: "voice", market: "official", state: "running", step: "download", steps: [], retryable: true,
  started_at: "2026-11-06T00:00:00Z", updated_at: "2026-11-06T00:00:01Z", ...over,
});
const header = (i: number) => sent[i].init.headers?.["X-Persona-Manager"];

beforeEach(() => {
  sent.length = 0;
  answers.length = 0;
  for (const o of operations()) forget(o);
  resetManagerChoice();
  setRosterDefault("assistant");
});

describe("another manager's operations", () => {
  it("keeps two managers' operations of one extension apart, the same id too", () => {
    track(op(), "assistant");
    track(op({ state: "needs_consent" }), "m-1");
    expect(operations()).toHaveLength(2);
    expect(latestFor(operations(), "voice", "official")?.state).toBe("running");
    expect(latestFor(operations(), "voice", "official", "m-1")?.state).toBe("needs_consent");
    expect(latestFor(operations(), "voice", "official", "m-1")?.manager).toBe("m-1");
  });

  it("names the operation's manager on what is asked about it", async () => {
    const started = startFromMarket("official", "voice", "m-1");
    expect(sent[0].path).toMatch(/\/-\/catalog\/preview$/);
    expect(header(0)).toBe("m-1");
    answers[0]({ operation: op() });
    expect((await started).manager).toBe("m-1");
    void getOperation("op1", "m-1");
    void retryOperation("op1", "m-1");
    void cancelOperation("op1", "m-1");
    expect([header(1), header(2), header(3)]).toEqual(["m-1", "m-1", "m-1"]);
    void getOperation("op1");
    expect(header(4)).toBeUndefined();
  });

  it("the hub's list (the default's) leaves the other managers' operations as they were", async () => {
    track(op({ id: "theirs" }), "m-1");
    track(op({ id: "ours" }), "assistant");
    const p = reloadOperations();
    answers[0]({ operations: [] });
    await p;
    expect(operations().map((o) => o.id)).toEqual(["theirs"]);
  });
});
