// Installing in steps (#3277): what a step says, which operation a page shows, and that a pushed operation replaces an
// older copy but never a newer one.
import { describe, expect, it, vi } from "vitest";
import { forget, latestFor, opLine, operations, shownSteps, stepDetail, stepFraction, stepLabel, stepsDone, track, type ExtensionOperation } from "./extension-operations";

vi.mock("./extensions", async (original) => ({ ...(await original<typeof import("./extensions")>()), changed: vi.fn((r) => r) }));

const op = (over: Partial<ExtensionOperation> = {}): ExtensionOperation => ({
  id: "op1",
  kind: "install",
  extension: "lark-connector",
  market: "m1",
  state: "running",
  step: "download",
  steps: [
    { id: "download", state: "running", bytes: 4 << 20, total: 16 << 20 },
    { id: "verify", state: "pending" },
    { id: "unpack", state: "pending" },
    { id: "check", state: "pending" },
    { id: "consent", state: "pending" },
    { id: "install", state: "pending" },
    { id: "start", state: "pending" },
    { id: "nodes", state: "skipped" },
  ],
  retryable: false,
  started_at: "2026-11-06T00:00:00Z",
  updated_at: "2026-11-06T00:00:01Z",
  ...over,
});

describe("extension operations", () => {
  it("says what a download has done, and how far", () => {
    const d = op().steps[0];
    expect(stepDetail(d)).toBe("4.0 MB / 16.0 MB");
    expect(stepFraction(d)).toBe(0.25);
    expect(stepDetail({ id: "download", state: "done", bytes: 1 << 20, cached: true })).toBe("用本机缓存");
    expect(stepFraction({ id: "download", state: "running" })).toBeUndefined(); // size unknown: no bar
    expect(stepDetail({ id: "nodes", state: "running", machines: { done: 1, total: 2 } })).toBe("1 / 2 台");
  });

  it("names the steps, an unknown one by its id", () => {
    expect(stepLabel("download")).toBe("下载");
    expect(stepLabel("install", "upgrade")).toBe("升级");
    expect(stepLabel("warm_up")).toBe("warm_up");
  });

  it("leaves out the skipped steps and counts the done ones", () => {
    const o = op({ steps: op().steps.map((s, i) => (i < 2 ? { ...s, state: "done" } : s)) });
    expect(shownSteps(o).map((s) => s.id)).not.toContain("nodes");
    expect(stepsDone(o)).toEqual({ done: 2, total: 7 });
  });

  it("says how it ended or where it is", () => {
    expect(opLine(op())).toBe("下载…");
    expect(opLine(op({ state: "failed", step: "download" }))).toBe("卡在「下载」");
    expect(opLine(op({ state: "needs_consent" }))).toBe("等你确认权限");
    expect(opLine(op({ state: "succeeded", kind: "uninstall" }))).toBe("已卸载");
  });

  it("keeps the newest copy of each and finds the newest of an extension", () => {
    track(op());
    track(op({ updated_at: "2026-11-06T00:00:00Z", step: "verify" })); // older: ignored
    expect(operations()[0].step).toBe("download");
    track(op({ id: "op2", started_at: "2026-11-06T00:01:00Z", updated_at: "2026-11-06T00:01:00Z" }));
    expect(latestFor(operations(), "lark-connector", "m1")?.id).toBe("op2");
    expect(latestFor(operations(), "lark-connector", "other")).toBeUndefined();
    forget("op2");
    expect(latestFor(operations(), "lark-connector")?.id).toBe("op1");
  });
});
