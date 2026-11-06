// @vitest-environment jsdom
// An extension's page that is the first to start the extension-operation listener (a reload or a link straight onto
// it): the hub's list of operations still holds ones that ended before the page opened (it keeps them for a while).
// Those are not acted on; an operation that succeeds after the page opened still is.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { HubExtensionDetail } from "../../lib/extensions";

const hub = vi.hoisted(() => ({
  listeners: new Map<string, Set<(m: Record<string, unknown>) => void>>(),
  ops: [] as unknown[],
  resolveList: null as null | (() => void),
  detailReads: 0,
}));
vi.mock("../../lib/api", async (original) => ({
  ...(await original<typeof import("../../lib/api")>()),
  api: (path: string) =>
    path.endsWith("/-/operations")
      ? new Promise((resolve) => (hub.resolveList = () => resolve({ operations: hub.ops })))
      : Promise.reject(new Error(`unexpected ${path}`)),
}));
vi.mock("../../lib/extensions", async (original) => ({
  ...(await original<typeof import("../../lib/extensions")>()),
  extensionDetail: () => {
    hub.detailReads++;
    return Promise.resolve({
      id: "example-ext", name: "Example", kind: "process", status: "running", source: "installed", enabled: true,
    } as HubExtensionDetail);
  },
}));
vi.mock("../../lib/store", async (original) => {
  const real = await original<typeof import("../../lib/store")>();
  return {
    ...real,
    getState: () => ({ ...real.getState(), features: ["extension_operations"] }),
    onHubEvent: (type: string, l: (m: Record<string, unknown>) => void) => {
      if (!hub.listeners.has(type)) hub.listeners.set(type, new Set());
      hub.listeners.get(type)!.add(l);
      return () => void hub.listeners.get(type)?.delete(l);
    },
  };
});
vi.mock("../../ui/atoms", async (original) => ({ ...(await original<typeof import("../../ui/atoms")>()), toast: vi.fn() }));
import { Extensions } from "./Extensions";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 8; i++) await act(async () => { await Promise.resolve(); });
};
const op = (id: string, kind: "install" | "upgrade" | "uninstall", minutesAgo: number) => {
  const at = new Date(Date.now() - minutesAgo * 60_000).toISOString();
  return { id, kind, extension: "example-ext", state: "succeeded", steps: [], retryable: false, started_at: at, updated_at: at };
};

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

describe("an extension's page that starts the operation listener", () => {
  it("does not act on operations that ended before it opened, and still acts on one pushed after", async () => {
    // reinstalled outside an operation after an uninstall that ended five minutes ago, upgraded before that
    hub.ops = [op("op-old-uninstall", "uninstall", 5), op("op-old-upgrade", "upgrade", 8)];
    const onOpen = vi.fn();
    act(() => root.render(<Extensions open="example-ext" onOpen={onOpen} />));
    await flush();
    expect(hub.detailReads).toBe(1);

    // a push that arrives while the list is on its way is new
    const pushed = { ...op("op-live", "upgrade", 0), updated_at: new Date(Date.now() + 1000).toISOString() };
    await act(async () => hub.listeners.get("extension_operation")?.forEach((l) => l({ type: "extension_operation", operation: pushed })));
    await flush();
    expect(hub.detailReads).toBe(2);

    await act(async () => hub.resolveList!());
    await flush();
    expect(onOpen).not.toHaveBeenCalled();
    expect(hub.detailReads).toBe(2);

    const later = { ...op("op-later", "upgrade", 0), updated_at: new Date(Date.now() + 2000).toISOString() };
    await act(async () => hub.listeners.get("extension_operation")?.forEach((l) => l({ type: "extension_operation", operation: later })));
    await flush();
    expect(hub.detailReads).toBe(3);
    expect(onOpen).not.toHaveBeenCalled();
  });
});
