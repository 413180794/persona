// @vitest-environment jsdom
// The roster's default (docs/assistant/managers.md "Clients"): a read that fails takes the hub's own assistant for now, so pages
// don't wait, and is read again a few seconds later, so a blip on a hub whose default is another manager is put right.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defaultKnown, defaultManager, resetManagerChoice } from "./manager-choice";
import { loadRoster, resetRoster, ROSTER_RETRIES, ROSTER_RETRY_MS } from "./managers";

const reply = (body: unknown, status = 200) => ({ ok: status < 400, status, text: async () => JSON.stringify(body), headers: new Headers() });
let answers: { status: number; body: unknown }[] = [];
let reads = 0;

async function settle() {
  for (let i = 0; i < 6; i++) await Promise.resolve();
}

beforeEach(() => {
  vi.useFakeTimers();
  resetManagerChoice();
  resetRoster();
  reads = 0;
  answers = [];
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (!url.endsWith("/managers")) return reply({ error: "not found" }, 404);
    reads++;
    const a = answers.shift() ?? { status: 503, body: { error: "unavailable" } };
    return reply(a.body, a.status);
  }));
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  resetManagerChoice();
  resetRoster();
});

describe("the roster's default", () => {
  it("a failed read takes the hub's own for now, then the read again puts the real default in place", async () => {
    answers = [{ status: 503, body: { error: "unavailable" } }, { status: 200, body: { default: "m-1", managers: [{ id: "m-1", name: "Work", online: true }] } }];
    await loadRoster();
    await settle();
    expect(defaultKnown()).toBe(true);
    expect(defaultManager()).toBe("assistant");
    await vi.advanceTimersByTimeAsync(ROSTER_RETRY_MS);
    await settle();
    expect(reads).toBe(2);
    expect(defaultManager()).toBe("m-1");
  });

  it("a hub without a roster is not asked again", async () => {
    answers = [{ status: 404, body: { error: "not found" } }];
    await loadRoster();
    await settle();
    expect(defaultKnown()).toBe(true);
    await vi.advanceTimersByTimeAsync(ROSTER_RETRY_MS * (ROSTER_RETRIES + 1));
    expect(reads).toBe(1);
  });

  it("a read that keeps failing is tried a few times, then left to the next hello", async () => {
    await loadRoster();
    await vi.advanceTimersByTimeAsync(ROSTER_RETRY_MS * (ROSTER_RETRIES + 3));
    await settle();
    expect(reads).toBe(ROSTER_RETRIES + 1);
  });
});
