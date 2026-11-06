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
      <Group k="persona" />
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
    expect([state("persona"), state("recent"), state("space:mac/a")]).toEqual(["open", "folded", "folded"]);
    expect(localStorage.getItem("fold:persona")).toBeNull();
    act(() => setAllFolded(true));
    expect(state("persona")).toBe("folded");
    act(() => setAgentsFolded(false));
    expect([state("persona"), state("recent"), state("space:mac/a")]).toEqual(["folded", "open", "open"]);
  });
});

// A mounted row whose key changes (a re-grouping, a renamed space) takes the new key's fold, not the old one's.
function Probe({ k, initial = false, holds = false }: { k: string; initial?: boolean; holds?: boolean }) {
  const [folded, toggle] = useFolded(k, initial, { holds });
  return <button data-k="probe" onClick={toggle}>{folded ? "folded" : "open"}</button>;
}

describe("changing the key", () => {
  const probe = () => host.querySelector<HTMLButtonElement>('[data-k="probe"]')!;

  it("reads the new key's stored fold and toggles it under the new key", () => {
    localStorage.setItem("fold:a", "1");
    localStorage.setItem("fold:b", "0");
    act(() => root.render(<Probe k="a" />));
    expect(probe().textContent).toBe("folded");
    act(() => root.render(<Probe k="b" />));
    expect(probe().textContent).toBe("open");
    act(() => probe().click());
    expect(probe().textContent).toBe("folded");
    expect(localStorage.getItem("fold:b")).toBe("1");
    expect(localStorage.getItem("fold:a")).toBe("1");
  });

  it("takes the new key's default when nothing is stored for it", () => {
    act(() => root.render(<Probe k="a" initial={false} />));
    expect(probe().textContent).toBe("open");
    act(() => root.render(<Probe k="b" initial={true} />));
    expect(probe().textContent).toBe("folded");
  });

  it("does not carry the old key's held-open state over", () => {
    localStorage.setItem("fold:a", "1");
    localStorage.setItem("fold:b", "1");
    act(() => root.render(<Probe k="a" holds />));
    expect(probe().textContent).toBe("open");
    act(() => root.render(<Probe k="a" holds={false} />));
    expect(probe().textContent).toBe("open");
    act(() => root.render(<Probe k="b" holds={false} />));
    expect(probe().textContent).toBe("folded");
  });
});
