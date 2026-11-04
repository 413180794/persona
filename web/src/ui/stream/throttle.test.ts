import { describe, expect, it } from "vitest";
import { createTextThrottle, throttleInterval, THROTTLE_BASE_MS, THROTTLE_MAX_MS, type ThrottleClock } from "./throttle";

describe("throttleInterval", () => {
  it("grows with the text and stops at 400 ms", () => {
    expect(throttleInterval("x")).toBe(THROTTLE_BASE_MS);
    expect(throttleInterval("x".repeat(12_000))).toBe(100);
    expect(throttleInterval("x".repeat(12_001))).toBe(125);
    expect(throttleInterval("x".repeat(32_001))).toBe(175);
    expect(throttleInterval("x".repeat(400_000))).toBe(THROTTLE_MAX_MS);
  });
});

function fakeClock() {
  let now = 0;
  let id = 0;
  const timers = new Map<number, { at: number; f: () => void }>();
  const clock: ThrottleClock = {
    now: () => now,
    setTimeout: (f, ms) => (timers.set(++id, { at: now + ms, f }), id),
    clearTimeout: (i) => void timers.delete(i),
  };
  const advance = (ms: number) => {
    now += ms;
    for (const [i, t] of [...timers]) {
      if (t.at <= now) {
        timers.delete(i);
        t.f();
      }
    }
  };
  return { clock, advance, pending: () => timers.size };
}

describe("createTextThrottle", () => {
  it("emits the first value at once, then at most once per interval, the latest one", () => {
    const out: string[] = [];
    const c = fakeClock();
    const t = createTextThrottle((s) => out.push(s), c.clock);
    t.push("a");
    expect(out).toEqual(["a"]);
    c.advance(30);
    t.push("ab");
    c.advance(30);
    t.push("abc");
    expect(out).toEqual(["a"]);
    c.advance(40); // 100 ms after "a"
    expect(out).toEqual(["a", "abc"]);
    c.advance(100);
    expect(out).toEqual(["a", "abc"]);
  });

  it("never drops the last value", () => {
    const out: string[] = [];
    const c = fakeClock();
    const t = createTextThrottle((s) => out.push(s), c.clock);
    t.push("a");
    c.advance(10);
    t.push("ab");
    c.advance(500);
    expect(out.at(-1)).toBe("ab");
  });

  it("flush emits at once and drops what was pending; the next stream starts at once", () => {
    const out: string[] = [];
    const c = fakeClock();
    const t = createTextThrottle((s) => out.push(s), c.clock);
    t.push("a");
    c.advance(10);
    t.push("ab");
    t.flush("");
    expect(out).toEqual(["a", ""]);
    expect(c.pending()).toBe(0);
    c.advance(10);
    t.push("new");
    expect(out.at(-1)).toBe("new");
  });

  it("cancel drops what was pending, and the next stream starts at once", () => {
    const out: string[] = [];
    const c = fakeClock();
    const t = createTextThrottle((s) => out.push(s), c.clock);
    t.push("a");
    t.push("ab");
    t.cancel();
    c.advance(500);
    expect(out).toEqual(["a"]);
    t.push("x");
    c.advance(1);
    t.cancel();
    t.push("y");
    expect(out).toEqual(["a", "x", "y"]);
  });
});
