// @vitest-environment jsdom
// A group's fold follows "Collapse all" / "Expand all" of its own block only (#3027): the Agents list menu leaves the other blocks.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { AGENTS_FOLD, setAgentsFolded, setAllFolded } from "./sidebar-api";
import { FoldScope, useFolded } from "./use-folded";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

function Group({ k, scope }: { k: string; scope?: string }) {
  const [folded] = useFolded(k, false, { scope });
  return <span data-k={k}>{folded ? "folded" : "open"}</span>;
}

let host: HTMLDivElement;
let root: Root;
const state = (k: string) => host.querySelector(`[data-k="${k}"]`)!.textContent;

beforeEach(() => {
  localStorage.clear();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  act(() => root.render(
    <>
      <Group k="jarvis" />
      <Group k="recent" scope={AGENTS_FOLD} />
      <FoldScope.Provider value={AGENTS_FOLD}>
        <Group k="space:mac/a" />
      </FoldScope.Provider>
    </>,
  ));
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

describe("folding one block", () => {
  it("folds and opens the Agents block's groups, not the others", () => {
    act(() => setAgentsFolded(true));
    expect([state("jarvis"), state("recent"), state("space:mac/a")]).toEqual(["open", "folded", "folded"]);
    expect(localStorage.getItem("fold:jarvis")).toBeNull();
    act(() => setAllFolded(true));
    expect(state("jarvis")).toBe("folded");
    act(() => setAgentsFolded(false));
    expect([state("jarvis"), state("recent"), state("space:mac/a")]).toEqual(["folded", "open", "open"]);
  });
});
