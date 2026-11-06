// @vitest-environment jsdom
// A failed poll keeps the last list on screen, so the page says the refresh failed; the next good poll clears it.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
});

const fake = vi.hoisted(() => ({ fail: false, calls: [] as string[] }));
vi.mock("./api", async (importOriginal) => {
  const api = await importOriginal<typeof import("./api")>();
  const answer = (path: string, ok: unknown) => {
    fake.calls.push(path);
    return fake.fail ? Promise.reject(new api.AdminError(502, "hub is not answering", {})) : Promise.resolve(ok);
  };
  return {
    ...api,
    adminApi: (path: string) =>
      answer(
        path,
        path === "/peers"
          ? { peers: [{ hub_id: "hub-far", name: "far-hub", url: "https://far.example", state: "linked" }], discovered: [] }
          : path === "/machines"
            ? { machines: [{ id: "box-one", name: "box-one", kind: "link", online: true, paths: [] }], tls: true }
            : { hubs: [] },
      ),
    getMesh: () => Promise.resolve({ hubs: [] }),
    getExtensions: () => answer("/extensions", { extensions: [], can_install: true }),
    getMetricsOverview: () => Promise.resolve({ extensions: [] }),
  };
});

const { Peers } = await import("./pages/Peers");
const { Machines } = await import("./pages/Machines");
const { Extensions } = await import("./pages/Extensions");

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 5; i++) await act(async () => { await Promise.resolve(); });
};
const tick = async (ms: number) => {
  act(() => vi.advanceTimersByTime(ms));
  await flush();
};

beforeEach(() => {
  fake.fail = false;
  fake.calls = [];
  Object.defineProperty(document, "visibilityState", { configurable: true, get: () => "visible" });
  vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.useRealTimers();
});

describe.each([
  { name: "Peers", Page: Peers, path: "/peers", every: 10_000, shown: "far-hub" },
  { name: "Machines", Page: Machines, path: "/machines", every: 15_000, shown: "box-one" },
  { name: "Extensions", Page: Extensions, path: "/extensions", every: 15_000, shown: "" },
])("$name", ({ Page, path, every, shown }) => {
  it("says a failed poll while the last list stays, and clears it on the next good poll", async () => {
    act(() => root.render(<Page />));
    await flush();
    expect(fake.calls).toContain(path);
    const listed = host.textContent ?? "";
    expect(listed).toContain(shown);
    expect(listed).not.toContain("hub is not answering");

    fake.fail = true;
    const before = fake.calls.filter((p) => p === path).length;
    await tick(every);
    expect(fake.calls.filter((p) => p === path).length).toBe(before + 1);
    expect(host.textContent).toContain("hub is not answering");
    expect(host.textContent).toContain(shown); // the last list is still there

    fake.fail = false;
    await tick(every);
    expect(host.textContent).not.toContain("hub is not answering");
    expect(host.textContent).toContain(shown);
  });
});
