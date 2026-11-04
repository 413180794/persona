// Which work groups, cards and thoughts the reader opened (cindy's useExpandedBlockMemory), kept for the page's life:
// a group that goes from live to done, or remounts when a page of history lands above it, stays as the reader left it.
import { useCallback, useState } from "react";

const opened = new Map<string, boolean>();
/** Folded ranges (seq … through_seq) the reader opened, by chat: the groups their steps land in start open, whatever
 *  key they get once the range is unfolded (a plan card taken out of it moves the first step). */
const foldRanges = new Map<string, [number, number][]>();

export function openFoldRange(scope: string, from: number, through: number) {
  foldRanges.set(scope, [...(foldRanges.get(scope) ?? []), [from, through]]);
}

export function inOpenedFold(scope: string, seq?: number): boolean {
  return seq !== undefined && (foldRanges.get(scope) ?? []).some(([a, b]) => seq >= a && seq <= b);
}

export function useExpanded(key: string, initial = false): [boolean, (next: boolean | ((v: boolean) => boolean)) => void] {
  const [, bump] = useState(0);
  const value = opened.get(key) ?? initial;
  const set = useCallback(
    (next: boolean | ((v: boolean) => boolean)) => {
      const v = typeof next === "function" ? next(opened.get(key) ?? initial) : next;
      opened.set(key, v);
      bump((n) => n + 1);
    },
    [key, initial],
  );
  return [value, set];
}
