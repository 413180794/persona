// @vitest-environment jsdom
// An extension page that answers 404 {code: "unsupported"} says "Nothing here" for that page only: going back to the
// page below, or another page opened in the same place, draws again. A pushed page that has nothing keeps its Back
// button. The hub is faked: every draw waits until the test answers it.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { t } from "../../i18n";
import { ApiError } from "../../lib/api";

type Asked = { ui: string; state?: string; answer: (view: unknown) => void };
const hub = vi.hoisted(() => ({ asked: [] as Asked[] }));

vi.mock("../../lib/extension-ui", async (original) => {
  const real = await original<typeof import("../../lib/extension-ui")>();
  return {
    ...real,
    renderUI: (_ext: string, ui: string, _ctx: unknown, state?: string) =>
      new Promise((resolve, reject) => hub.asked.push({
        ui, state, answer: (v) => (v instanceof Error ? reject(v) : resolve(real.readView(v))),
      })),
    actUI: () => Promise.reject(new Error("no press expected")),
  };
});
vi.mock("../../lib/store", async (original) => ({
  ...(await original<typeof import("../../lib/store")>()),
  onHubEvent: () => () => {},
}));
vi.mock("../../ui/atoms", async (original) => ({ ...(await original<typeof import("../../ui/atoms")>()), toast: vi.fn() }));
import { ExtensionUI } from "./ExtensionView";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.IS_REACT_ACT_ENVIRONMENT = true;
  g.matchMedia = (q: string) => ({ matches: q.includes("min-width"), addEventListener() {}, removeEventListener() {} });
});

/** A page saying `word`, with a row that pushes page b. */
const page = (word: string) => ({
  state: word,
  sections: [{ items: [{ type: "text", text: `body ${word}` }, { type: "row", title: "open b", action: { id: "open", push: { state: "b" } } }] }],
});
const unsupported = () => new ApiError(404, "unsupported", { code: "unsupported" });

let host: HTMLDivElement;
let root: Root;
const settle = async () => {
  for (let i = 0; i < 8; i++) await act(async () => { await Promise.resolve(); });
};
const answer = async (view: unknown) => {
  const a = hub.asked.shift();
  expect(a).toBeTruthy();
  await act(async () => a!.answer(view));
  await settle();
  return a!;
};
const shown = () => [...host.querySelectorAll("*")].filter((n) => n.children.length === 0 && /^body /.test(n.textContent ?? "")).map((n) => n.textContent);
const nothingHere = () => [...host.querySelectorAll("p")].some((p) => p.textContent === t("settings-extensionview.nothing_here"));
const button = (text: string) => [...host.querySelectorAll("button")].find((b) => b.textContent?.includes(text));
const draw = async (ui: string) => {
  act(() => root.render(<ExtensionUI extension="demo" ui={ui} history={false} title="Demo" />));
  await settle();
};

beforeEach(() => {
  hub.asked = [];
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

describe("ExtensionUI unsupported answer", () => {
  it("a pushed page with nothing keeps Back, and the page below draws again", async () => {
    await draw("main");
    await answer(page("a"));
    expect(shown()).toEqual(["body a"]);
    await act(async () => button("open b")!.click());
    await settle();
    expect((await answer(unsupported())).state).toBe("b");
    expect(nothingHere()).toBe(true);
    const back = button("Demo");
    expect(back).toBeTruthy();
    await act(async () => back!.click());
    await settle();
    expect((await answer(page("a"))).ui).toBe("main"); // page a drawn again
    expect(nothingHere()).toBe(false);
    expect(shown()).toEqual(["body a"]);
  });

  it("another page opened in the same place draws after an unsupported answer", async () => {
    await draw("main");
    await answer(unsupported());
    expect(nothingHere()).toBe(true);
    expect(button("Demo")).toBeUndefined(); // the first page has no page below
    await draw("other");
    expect((await answer(page("c"))).ui).toBe("other");
    expect(nothingHere()).toBe(false);
    expect(shown()).toEqual(["body c"]);
  });
});
