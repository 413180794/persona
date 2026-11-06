// @vitest-environment jsdom
// usePanes: `busy` must clear when every machine goes offline mid-read, and an older panes reply must not replace a newer one.
import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { PanesResponse } from "./terminals";
import type { Machine } from "./types";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

interface Deferred {
  path: string;
  resolve: (v: unknown) => void;
  reject: (e: unknown) => void;
}
const calls: Deferred[] = [];

vi.mock("./api", () => ({
  api: (path: string) =>
    new Promise((resolve, reject) => {
      calls.push({ path, resolve, reject });
    }),
  enc: encodeURIComponent,
  errorText: (e: unknown) => String(e),
}));

const { usePanes } = await import("./terminal-panes");

type State = ReturnType<typeof usePanes>;
let state: State;
function Probe({ machines }: { machines: Machine[] }) {
  state = usePanes(machines);
  return null;
}

const machine = (id: string, online: boolean) => ({ id, online }) as Machine;
const panes = (tag: string) => ({ tag }) as unknown as PanesResponse;

describe("usePanes", () => {
  let root: ReturnType<typeof createRoot>;
  beforeEach(() => {
    calls.length = 0;
    root = createRoot(document.createElement("div"));
  });
  afterEach(() => act(() => root.unmount()));

  it("clears busy when the last online machine goes offline before its read returns", async () => {
    act(() => root.render(<Probe machines={[machine("m1", true)]} />));
    expect(state.busy).toBe(true);
    expect(calls).toHaveLength(1);
    act(() => root.render(<Probe machines={[machine("m1", false)]} />));
    await act(async () => calls[0].resolve(panes("late")));
    expect(state.busy).toBe(false);
  });

  it("keeps the newer reply when an older read for the same machine lands after it", async () => {
    act(() => root.render(<Probe machines={[machine("m1", true)]} />));
    let refreshed!: Promise<void>;
    act(() => {
      refreshed = state.refresh();
    });
    expect(calls).toHaveLength(2);
    await act(async () => {
      calls[1].resolve(panes("new"));
      await refreshed;
    });
    expect(state.listing.m1?.data).toEqual(panes("new"));
    expect(state.busy).toBe(false);
    await act(async () => calls[0].resolve(panes("old")));
    expect(state.listing.m1?.data).toEqual(panes("new"));
    // an older failed read does not put an error over the newer list either
    act(() => void state.reload(["m1"]));
    act(() => void state.reload(["m1"]));
    await act(async () => calls[3].resolve(panes("newest")));
    await act(async () => calls[2].reject(new Error("gone")));
    expect(state.listing.m1).toEqual({ data: panes("newest") });
  });

  it("shows an older reply that lands first, then the newer one", async () => {
    act(() => root.render(<Probe machines={[machine("m1", true)]} />));
    act(() => void state.reload(["m1"]));
    await act(async () => calls[0].resolve(panes("a")));
    expect(state.listing.m1?.data).toEqual(panes("a"));
    expect(state.busy).toBe(false);
    await act(async () => calls[1].resolve(panes("b")));
    expect(state.listing.m1?.data).toEqual(panes("b"));
  });
});
