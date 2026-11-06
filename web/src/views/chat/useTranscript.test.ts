import { describe, expect, it } from "vitest";
import { createTextThrottle, type ThrottleClock } from "../../ui/stream/throttle";
import { applyDelta } from "./useTranscript";

describe("the assistant's deltas", () => {
  it("are throttled, and the end clears the reply at once without a held frame bringing it back", () => {
    let now = 0;
    const timers = new Map<number, () => void>();
    const clock: ThrottleClock = {
      now: () => now,
      setTimeout: (f) => (timers.set(timers.size + 1, f), timers.size),
      clearTimeout: (id) => void timers.delete(id),
    };
    const shown: string[] = [];
    const t = createTextThrottle((s) => shown.push(s), clock);
    applyDelta(t, "Hel");
    now = 20; // within the 33 ms interval
    applyDelta(t, "Hello");
    expect(shown).toEqual(["Hel"]); // inside the interval: held
    applyDelta(t, "");
    expect(shown).toEqual(["Hel", ""]);
    timers.forEach((f) => f()); // the held frame was dropped
    expect(shown).toEqual(["Hel", ""]);
  });
});
