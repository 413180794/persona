// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { RETIRED_PERSONAS, type PersonaInfo } from "./persona";

vi.mock("./attention-signals", () => ({ setFaviconBase: vi.fn() }));
vi.mock("./api", () => ({ getToken: () => "" }));

const KEY = "persona.look-icon";
const cached = (id: string) => JSON.stringify({ key: `${id}@rev`, light: "data:image/png;base64,AA==", dark: "data:image/png;base64,AA==" });
const pack: PersonaInfo = { id: "nwverse", rev: "rev", manifest: {
  format: 1, id: "nwverse", name: "Milk Frog", avatar: { sizes: { "128": "avatar.png" } }, dot: { states: { idle: { file: "idle.png" } } },
} };
let images: { onload: (() => void) | null; onerror: (() => void) | null }[];

beforeEach(() => {
  vi.resetModules();
  localStorage.clear();
  images = [];
  vi.stubGlobal("Image", class {
    onload: (() => void) | null = null;
    onerror: (() => void) | null = null;
    constructor() { images.push(this); }
    set src(_: string) {}
  });
});

describe("look icons after bundled look removal", () => {
  it.each(RETIRED_PERSONAS)("discards the cached drawing for retired selection %s on first paint", async (id) => {
    localStorage.setItem("persona.persona", id);
    localStorage.setItem(KEY, cached(id));
    await import("./look-icon");
    expect(localStorage.getItem(KEY)).toBeNull();
    expect(images).toHaveLength(0);
  });

  it("discards a cached icon from a different selection or the default", async () => {
    localStorage.setItem(KEY, cached("nwverse"));
    await import("./look-icon");
    expect(localStorage.getItem(KEY)).toBeNull();
    vi.resetModules();
    localStorage.setItem("persona.persona", "unknown-look");
    localStorage.setItem(KEY, cached("nwverse"));
    await import("./look-icon");
    expect(localStorage.getItem(KEY)).toBeNull();
  });

  it("keeps a valid package's cached icon, then clears it for a missing package", async () => {
    localStorage.setItem("persona.persona", "nwverse");
    localStorage.setItem(KEY, cached("nwverse"));
    const { setLookIcon } = await import("./look-icon");
    expect(localStorage.getItem(KEY)).toBe(cached("nwverse"));
    setLookIcon(null);
    expect(localStorage.getItem(KEY)).toBeNull();
  });

  it("does not restore a package icon that finishes after switching to the default", async () => {
    const { setLookIcon } = await import("./look-icon");
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue({ drawImage: vi.fn() } as unknown as CanvasRenderingContext2D);
    vi.spyOn(HTMLCanvasElement.prototype, "toDataURL").mockReturnValue("data:image/png;base64,AA==");
    setLookIcon(pack);
    expect(images).toHaveLength(2);
    setLookIcon(null);
    images.forEach((img) => img.onload?.());
    await Promise.resolve();
    await Promise.resolve();
    expect(localStorage.getItem(KEY)).toBeNull();
  });
});
