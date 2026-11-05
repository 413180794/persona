// @vitest-environment jsdom
// The look store against a hub that keeps the choice (GET /looks, PUT /looks/current, #3200): the hub's choice wins,
// this browser's old one moves up once when the hub has none, choosing goes to the hub, and another device's choice
// arrives as a "look" push.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LookCurrent, LookView } from "../../lib/persona";

const hub = vi.hoisted(() => ({
  looks: [] as unknown[],
  current: { id: null, effective: "persona" } as { id: string | null; effective: string },
  puts: [] as unknown[],
  listeners: new Map<string, (m: Record<string, unknown>) => void>(),
}));
vi.mock("../../lib/api", async (orig) => {
  const real = await orig<typeof import("../../lib/api")>();
  return {
    ...real,
    getToken: () => "",
    api: vi.fn((path: string, init?: { method?: string; json?: { id: string; if_unset?: boolean } }) => {
      if (path === "/looks") return Promise.resolve({ looks: hub.looks, current: hub.current });
      if (path === "/looks/current" && init?.method === "PUT") {
        hub.puts.push(init.json);
        const body = init.json!;
        if (!(body.if_unset && hub.current.id)) hub.current = { id: body.id, effective: body.id };
        return Promise.resolve(hub.current);
      }
      return Promise.reject(new real.ApiError(404, "not found", {}));
    }),
  };
});
vi.mock("../../lib/store", () => ({ onHubEvent: (type: string, l: (m: Record<string, unknown>) => void) => hub.listeners.set(type, l) }));
vi.stubGlobal("Image", class { onload: (() => void) | null = null; onerror: (() => void) | null = null; set src(_: string) { queueMicrotask(() => this.onerror?.()); } });

const aurora: LookView = {
  id: "aurora", source: "extension", name: "极光", names: { en: "Aurora" }, app_icon: "persona", extension: "example-looks", extension_name: "Example Looks",
  rev: "0123456789abcdef",
  manifest: {
    format: 1, id: "aurora", name: "极光", names: { en: "Aurora" },
    avatar: { sizes: { "128": "a/128.png" }, svg: "a/a.svg", dark: { svg: "a/dark.svg" } },
    dot: { states: { idle: { file: "a/idle.png" } } },
    source: { kind: "extension", extension: "example-looks", extension_name: "Example Looks" },
  },
};
const builtins: LookView[] = ["persona", "jarvis", "jarvis-circuit"].map((id) => ({ id, source: "builtin", name: id, app_icon: id }));

async function load() {
  vi.resetModules();
  return import("./Persona");
}

describe("the look the hub keeps", () => {
  beforeEach(() => {
    localStorage.clear();
    hub.looks = [...builtins, aurora];
    hub.current = { id: null, effective: "persona" };
    hub.puts = [];
  });
  afterEach(() => localStorage.clear());

  it("moves this browser's old choice up once when the hub has none", async () => {
    localStorage.setItem("jarvis.persona", "aurora");
    const p = await load();
    await p.refreshPersonas();
    expect(hub.puts).toEqual([{ id: "aurora", if_unset: true }]);
    expect(hub.current.id).toBe("aurora");
  });

  it("takes the hub's choice over this browser's", async () => {
    localStorage.setItem("jarvis.persona", "jarvis");
    hub.current = { id: "aurora", effective: "aurora" } satisfies LookCurrent;
    const p = await load();
    await p.refreshPersonas();
    expect(hub.puts).toEqual([]);
    expect(localStorage.getItem("jarvis.persona")).toBe("aurora");
  });

  it("does not move anything up from a browser that never chose", async () => {
    const p = await load();
    await p.refreshPersonas();
    expect(hub.puts).toEqual([]);
  });

  it("sends a choice to the hub, and follows another device's", async () => {
    const p = await load();
    await p.refreshPersonas();
    p.choosePersona("jarvis-circuit");
    expect(hub.puts).toEqual([{ id: "jarvis-circuit" }]);
    expect(localStorage.getItem("jarvis.persona")).toBe("jarvis-circuit");
    hub.listeners.get("look")!({ type: "look", id: "aurora", effective: "aurora" });
    expect(localStorage.getItem("jarvis.persona")).toBe("aurora");
    hub.listeners.get("look")!({ type: "look", id: "persona", effective: "persona" });
    expect(localStorage.getItem("jarvis.persona")).toBeNull();
  });
});
