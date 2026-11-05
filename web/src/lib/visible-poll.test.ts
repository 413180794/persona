import { afterEach, describe, expect, it, vi } from "vitest";
import { latestRequest, visiblePoll } from "./visible-poll";

afterEach(() => vi.useRealTimers());

describe("visible declaration availability", () => {
  it("refreshes immediately and every 30 seconds only while visible, cleaning timers and listeners", () => {
    vi.useFakeTimers();
    let visible = true, change: (() => void) | undefined;
    const refresh = vi.fn();
    const stop = visiblePoll(refresh, 30_000, { visible: () => visible, subscribe: (fn) => { change = fn; return () => { change = undefined; }; } });
    expect(refresh).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(29_999); expect(refresh).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(1); expect(refresh).toHaveBeenCalledTimes(2);
    visible = false; change!(); expect(vi.getTimerCount()).toBe(0);
    vi.advanceTimersByTime(90_000); expect(refresh).toHaveBeenCalledTimes(2);
    visible = true; change!(); expect(refresh).toHaveBeenCalledTimes(3);
    stop(); expect(change).toBeUndefined(); expect(vi.getTimerCount()).toBe(0);
    vi.advanceTimersByTime(90_000); expect(refresh).toHaveBeenCalledTimes(3);
  });
  it("does not schedule background checks for the default theme", () => {
    vi.useFakeTimers(); const refresh = vi.fn();
    const stop = visiblePoll(refresh, 0, { visible: () => true, subscribe: () => () => {} });
    expect(refresh).toHaveBeenCalledTimes(1); expect(vi.getTimerCount()).toBe(0); stop();
  });
  it("a late old success cannot restore a disabled theme, and disposal prevents updates and fetches", async () => {
    let old: (v: string[]) => void = () => {};
    const apply = vi.fn(), failed = vi.fn(); const request = latestRequest<string[]>(apply, failed);
    const pending = request.run(() => new Promise((resolve) => { old = resolve; }));
    await request.run(async () => []);
    old(["disabled-theme"]); await pending;
    expect(apply.mock.calls).toEqual([[[]]]);
    let late: (v: string[]) => void = () => {};
    const end = request.run(() => new Promise((resolve) => { late = resolve; }));
    request.dispose(); late(["theme"]); await end;
    const fetch = vi.fn(async () => ["theme"]); await request.run(fetch);
    expect(apply.mock.calls).toEqual([[[]]]); expect(fetch).not.toHaveBeenCalled();
  });
  it("ignores a late old error as well", async () => {
    let reject: (e: Error) => void = () => {};
    const failed = vi.fn(), apply = vi.fn(); const request = latestRequest<string[]>(apply, failed);
    const pending = request.run(() => new Promise((_, r) => { reject = r; }));
    await request.run(async () => ["current"]); reject(new Error("old")); await pending;
    expect(failed).not.toHaveBeenCalled(); expect(apply).toHaveBeenCalledWith(["current"]);
  });
});
