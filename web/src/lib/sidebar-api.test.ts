import { afterEach, describe, expect, it, vi } from "vitest";
import { cycleGrouping, GROUPING_KEY, groupingLabel, nextGrouping, onFoldAll, onGrouping, setAllFolded, stepIndex } from "./sidebar-api";

afterEach(() => vi.unstubAllGlobals());

describe("folding every group", () => {
  it("tells every listener, until it is taken back", () => {
    const seen: boolean[] = [];
    const off = onFoldAll((f) => seen.push(f));
    setAllFolded(true);
    setAllFolded(false);
    off();
    setAllFolded(true);
    expect(seen).toEqual([true, false]);
  });
});

describe("the sidebar's grouping", () => {
  it("goes round the groupings", () => {
    expect(nextGrouping("machine")).toBe("repo");
    expect(nextGrouping("todo")).toBe("machine");
  });
  it("skips the ones a hub does not offer", () => {
    expect(nextGrouping("state", ["machine", "repo", "space", "state"])).toBe("machine");
    expect(nextGrouping("todo", ["machine", "repo"])).toBe("repo");
  });
  it("keeps the choice in this browser, as the menu does, and tells the sidebar", () => {
    const store = new Map<string, string>([[GROUPING_KEY, "repo"]]);
    vi.stubGlobal("localStorage", { getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => void store.set(k, v) });
    const seen: string[] = [];
    const off = onGrouping((g) => seen.push(g));
    expect(cycleGrouping()).toBe("space");
    expect(store.get(GROUPING_KEY)).toBe("space");
    expect(seen).toEqual(["space"]);
    off();
    expect(groupingLabel("space")).toBe("按项目");
  });
  it("starts from by machine when nothing was chosen", () => {
    vi.stubGlobal("localStorage", { getItem: () => null, setItem: () => {} });
    expect(cycleGrouping()).toBe("repo");
  });
});

describe("walking the rows", () => {
  it("steps to the next and previous, wrapping round", () => {
    expect(stepIndex(5, 1, 1)).toBe(2);
    expect(stepIndex(5, 1, -1)).toBe(0);
    expect(stepIndex(5, 4, 1)).toBe(0);
    expect(stepIndex(5, 0, -1)).toBe(4);
  });
  it("starts at the first going down and the last going up when no row is open", () => {
    expect(stepIndex(5, -1, 1)).toBe(0);
    expect(stepIndex(5, -1, -1)).toBe(4);
  });
  it("has no row to step to in an empty list", () => {
    expect(stepIndex(0, -1, 1)).toBe(-1);
  });
});
