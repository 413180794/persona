// How often streaming text is rendered (cindy's streaming throttle, paced like Claude Desktop's, #5602): at most once
// per interval; the interval is the page's pace (33 ms while it is in front, 100 ms while the window is not focused,
// 200 ms while the tab is hidden) or longer for a long text (re-parsing a long reply costs more), and the last value is
// never dropped.
import { useEffect, useRef, useState } from "react";

/** Where the page is: in front, in a window without focus, or in a hidden tab. */
export type Pace = "front" | "unfocused" | "hidden";

export const PACE_MS: Record<Pace, number> = { front: 33, unfocused: 100, hidden: 200 };
export const THROTTLE_BASE_MS = PACE_MS.front;
export const THROTTLE_MAX_MS = 400;

/** The interval for a text this long, at a pace. Length only: this runs on every update, before any coalescing. */
export function throttleInterval(text: string, pace: Pace = "front"): number {
  const n = text.length;
  const base = PACE_MS[pace];
  if (n > 320_000) return THROTTLE_MAX_MS;
  if (n > 160_000) return Math.max(base, 325);
  if (n > 80_000) return Math.max(base, 250);
  if (n > 32_000) return Math.max(base, 175);
  if (n > 12_000) return Math.max(base, 125);
  if (n > 4_000) return Math.max(base, 66);
  return base;
}

/** The page's pace now. */
export function pagePace(): Pace {
  if (typeof document === "undefined") return "front";
  if (document.visibilityState === "hidden") return "hidden";
  return typeof document.hasFocus === "function" && !document.hasFocus() ? "unfocused" : "front";
}

export interface TextThrottle {
  /** A new value: emitted now if the interval has passed since the last emit, else when it has (the latest one). */
  push(text: string): void;
  /** Emit this value now and drop any pending one (the stream ended, or was replaced). */
  flush(text: string): void;
  /** Drop what is pending; the next value goes out at once. */
  cancel(): void;
}

export interface ThrottleClock {
  now: () => number;
  /** The page's pace (pagePace); absent: in front. */
  pace?: () => Pace;
  setTimeout: (f: () => void, ms: number) => number;
  clearTimeout: (id: number) => void;
}

const browserClock: ThrottleClock = {
  now: () => performance.now(),
  pace: pagePace,
  setTimeout: (f, ms) => window.setTimeout(f, ms),
  clearTimeout: (id) => window.clearTimeout(id),
};

export function createTextThrottle(emit: (text: string) => void, clock: ThrottleClock = browserClock): TextThrottle {
  let last = -Infinity; // the first value of a stream goes out at once
  let latest = "";
  let timer: number | undefined;
  const stop = () => {
    if (timer !== undefined) clock.clearTimeout(timer);
    timer = undefined;
  };
  const out = (text: string) => {
    last = clock.now();
    emit(text);
  };
  return {
    push(text) {
      latest = text;
      const wait = throttleInterval(text, clock.pace?.() ?? "front") - (clock.now() - last);
      if (wait <= 0) {
        stop();
        out(text);
        return;
      }
      // re-armed at every value: a longer text's interval applies at once
      stop();
      timer = clock.setTimeout(() => {
        timer = undefined;
        out(latest);
      }, wait);
    },
    flush(text) {
      stop();
      latest = text;
      last = -Infinity;
      emit(text);
    },
    cancel() {
      stop();
      last = -Infinity;
    },
  };
}

/** `value`, rendered at most once per interval while `enabled`; the latest value at once when it isn't. */
export function useThrottledText(value: string, enabled: boolean): string {
  const [shown, setShown] = useState(value);
  const throttle = useRef<TextThrottle | null>(null);
  throttle.current ??= createTextThrottle(setShown);
  // nothing emitted in this stream yet: `shown` may be the last stream's (a text that isn't streaming costs no state)
  const fresh = useRef(true);
  useEffect(() => {
    if (enabled) throttle.current!.push(value);
    else throttle.current!.cancel();
    fresh.current = !enabled;
  }, [value, enabled]);
  useEffect(() => () => throttle.current?.cancel(), []);
  return enabled && !fresh.current ? shown : value;
}
