// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LINGER_MS, acquireFeed, liveFeeds, releaseFeed } from "./full-registry";

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  // every surface a test left behind goes, then the feeds
  for (let i = 0; i < 5 && liveFeeds().length; i++) {
    for (const f of liveFeeds()) releaseFeed(f.mid, f);
    vi.runAllTimers();
  }
  vi.useRealTimers();
});

describe("one full feed per machine", () => {
  it("is shared by the surfaces of a machine and not by those of another", () => {
    const a = acquireFeed("m1", "s1");
    const b = acquireFeed("m1", "s2");
    const c = acquireFeed("m2", "s9");
    expect(a.fresh).toBe(true);
    expect(b.fresh).toBe(false);
    expect(b.feed).toBe(a.feed);
    expect(c.fresh).toBe(true);
    expect(c.feed).not.toBe(a.feed);
    // it opens on the address of the first surface; the next ones retarget it when they claim it
    expect(a.feed.sid).toBe("s1");
  });

  it("outlives a route change: the old page goes and the new one comes in the same breath", () => {
    const old = acquireFeed("m1", "s1");
    releaseFeed("m1", old.feed); // the page unmounts…
    const next = acquireFeed("m1", "s2"); // …and the next mounts
    expect(next.feed).toBe(old.feed);
    expect(next.fresh).toBe(false);
    vi.advanceTimersByTime(LINGER_MS * 3);
    expect(liveFeeds()).toContain(old.feed); // the linger was cancelled by the new surface
  });

  it("is let go a moment after its last surface", () => {
    const a = acquireFeed("m1", "s1");
    const dispose = vi.spyOn(a.feed, "dispose");
    releaseFeed("m1", a.feed);
    vi.advanceTimersByTime(LINGER_MS - 1);
    expect(dispose).not.toHaveBeenCalled();
    vi.advanceTimersByTime(2);
    expect(dispose).toHaveBeenCalledTimes(1);
    expect(liveFeeds()).not.toContain(a.feed);
    expect(acquireFeed("m1", "s1").fresh).toBe(true); // a new surface starts a new feed
  });

  it("stays while any surface of the machine is still mounted (a kept terminal behind the one shown)", () => {
    const a = acquireFeed("m1", "s1");
    const b = acquireFeed("m1", "s2");
    releaseFeed("m1", a.feed);
    vi.advanceTimersByTime(LINGER_MS * 3);
    expect(liveFeeds()).toContain(b.feed);
    releaseFeed("m1", b.feed);
    vi.advanceTimersByTime(LINGER_MS + 1);
    expect(liveFeeds()).not.toContain(b.feed);
  });
});
