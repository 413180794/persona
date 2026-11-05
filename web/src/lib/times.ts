/** An RFC3339 time in milliseconds; 0 for none or one that does not parse. The hub's times are not fixed-width (Go
 * drops a fraction's trailing zeros: `…:05.12Z` and `…:05.123Z`) and may carry an offset (`+08:00`), so they are
 * compared as times, never as strings. */
export function timeMs(at?: string | null): number {
  const ms = at ? Date.parse(at) : NaN;
  return Number.isNaN(ms) ? 0 : ms;
}

/** Compares two times for a sort: the earlier first (swap the arguments for the latest first); none is the earliest. */
export function byTime(a?: string | null, b?: string | null): number {
  return timeMs(a) - timeMs(b);
}
