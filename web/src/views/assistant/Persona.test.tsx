// @vitest-environment jsdom
// The look store against a hub that keeps the choice (GET /looks, PUT /looks/current, #3200): the hub's choice wins,
// this browser's old one moves up once when the hub has none, choosing goes to the hub, and another device's choice
// arrives as a "look" push.
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { LookCurrent, LookView } from "../../lib/persona";

const hub = vi.hoisted(() => ({
  looks: [] as unknown[],
  current: { id: null, effective: "persona" } as { id: string | null; effective: string },
  puts: [] as unknown[],
  listeners: new Map<string, (m: Record<string, unknown>) => void>(),
  /** When set, GET /looks waits for it (a read still on its way). */
  hold: null as Promise<void> | null,
}));
vi.mock("../../lib/api", async (orig) => {
  const real = await orig<typeof import("../../lib/api")>();
  return {
    ...real,
    getToken: () => "",
    api: vi.fn((path: string, init?: { method?: string; json?: { id: string; if_unset?: boolean } }) => {
      if (path === "/looks") {
        // the answer is what the hub had when the request left
        const answer = { looks: hub.looks, current: hub.current };
        return (hub.hold ?? Promise.resolve()).then(() => answer);
      }
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
const builtins: LookView[] = ["persona", "persona-visor", "persona-circuit"].map((id) => ({ id, source: "builtin", name: id, app_icon: id }));

async function load() {
  vi.resetModules();
  return import("./Persona");
}

describe("the look the hub keeps", () => {
  // The first import transforms Persona's whole module graph, which takes seconds when the full suite loads the
  // machine; doing it once here keeps that cost out of the first test's 5 s budget. load() still resets modules.
  beforeAll(() => import("./Persona"), 60_000);
  beforeEach(() => {
    localStorage.clear();
    hub.looks = [...builtins, aurora];
    hub.current = { id: null, effective: "persona" };
    hub.puts = [];
    hub.hold = null;
  });
  afterEach(() => localStorage.clear());

  it("moves this browser's old choice up once when the hub has none", async () => {
    localStorage.setItem("persona.persona", "aurora");
    const p = await load();
    await p.refreshPersonas();
    expect(hub.puts).toEqual([{ id: "aurora", if_unset: true }]);
    expect(hub.current.id).toBe("aurora");
  });

  it("takes the hub's choice over this browser's", async () => {
    localStorage.setItem("persona.persona", "persona");
    hub.current = { id: "aurora", effective: "aurora" } satisfies LookCurrent;
    const p = await load();
    await p.refreshPersonas();
    expect(hub.puts).toEqual([]);
    expect(localStorage.getItem("persona.persona")).toBe("aurora");
  });

  it("does not move anything up from a browser that never chose", async () => {
    const p = await load();
    await p.refreshPersonas();
    expect(hub.puts).toEqual([]);
  });

  it("sends a choice to the hub, and follows another device's", async () => {
    const p = await load();
    await p.refreshPersonas();
    p.choosePersona("persona-circuit");
    expect(hub.puts).toEqual([{ id: "persona-circuit" }]);
    expect(localStorage.getItem("persona.persona")).toBe("persona-circuit");
    hub.listeners.get("look")!({ type: "look", id: "aurora", effective: "aurora" });
    expect(localStorage.getItem("persona.persona")).toBe("aurora");
    hub.listeners.get("look")!({ type: "look", id: "persona", effective: "persona" });
    expect(localStorage.getItem("persona.persona")).toBeNull();
  });

  // #4682
  const holdReads = () => {
    let release!: () => void;
    hub.hold = new Promise<void>((r) => (release = r));
    return () => {
      hub.hold = null;
      release();
    };
  };

  it("keeps a choice made while the list is being read", async () => {
    hub.current = { id: "aurora", effective: "aurora" };
    const p = await load();
    await p.refreshPersonas();
    const release = holdReads();
    const reading = p.refreshPersonas(); // the picker opens
    p.choosePersona("persona-visor");
    expect(hub.current.id).toBe("persona-visor");
    release();
    await reading;
    expect(localStorage.getItem("persona.persona")).toBe("persona-visor");
  });

  it("keeps another device's choice pushed while the list is being read", async () => {
    hub.current = { id: "aurora", effective: "aurora" };
    const p = await load();
    await p.refreshPersonas();
    const release = holdReads();
    const reading = p.refreshPersonas();
    hub.current = { id: "persona-circuit", effective: "persona-circuit" };
    hub.listeners.get("look")!({ type: "look", id: "persona-circuit", effective: "persona-circuit" });
    release();
    await reading;
    expect(localStorage.getItem("persona.persona")).toBe("persona-circuit");
  });

  it("sends a choice made before the first answer once it comes", async () => {
    hub.current = { id: "aurora", effective: "aurora" };
    const p = await load();
    const release = holdReads();
    const reading = p.refreshPersonas(); // the page's first read
    p.choosePersona("persona-visor");
    expect(hub.puts).toEqual([]); // not known yet whether this hub keeps it
    release();
    await reading;
    expect(hub.puts).toEqual([{ id: "persona-visor" }]);
    expect(hub.current.id).toBe("persona-visor");
    expect(localStorage.getItem("persona.persona")).toBe("persona-visor");
  });

  it("keeps the look a push confirms while the list is being read, even one this page already shows", async () => {
    hub.current = { id: "persona", effective: "persona" };
    localStorage.setItem("persona.persona", "aurora");
    const p = await load();
    const release = holdReads();
    const reading = p.refreshPersonas(); // its answer will say persona
    hub.current = { id: "aurora", effective: "aurora" };
    hub.listeners.get("look")!({ type: "look", id: "aurora", effective: "aurora" });
    release();
    await reading;
    expect(localStorage.getItem("persona.persona")).toBe("aurora");
  });

  it("sends a choice made before the first answer once, when the hub has none yet", async () => {
    const p = await load();
    const release = holdReads();
    const reading = p.refreshPersonas();
    p.choosePersona("persona-visor");
    release();
    await reading;
    expect(hub.puts).toEqual([{ id: "persona-visor" }]);
    expect(hub.current.id).toBe("persona-visor");
  });
});
