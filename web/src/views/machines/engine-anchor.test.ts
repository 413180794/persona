// @vitest-environment jsdom
// The engine pill in a machine page's header (#2842): it scrolls to Engine and moves focus to the chosen engine.
import { afterEach, describe, expect, it, vi } from "vitest";
import { openEngineSection } from "./engine-anchor";

let reduce = false;
vi.stubGlobal("matchMedia", (q: string) => ({ matches: reduce && q.includes("reduce"), addEventListener() {}, removeEventListener() {} }));

function page(radios: string) {
  document.body.innerHTML = `<button id="pill">Orca</button><section id="engine" tabindex="-1">${radios}</section>`;
  const section = document.getElementById("engine")!;
  const scroll = vi.fn();
  section.scrollIntoView = scroll;
  return { section, scroll };
}

afterEach(() => {
  reduce = false;
  document.body.innerHTML = "";
});

describe("openEngineSection", () => {
  it("scrolls smoothly and focuses the chosen engine", () => {
    const { scroll } = page(`<button role="radio" aria-checked="false">herdr</button><button id="orca" role="radio" aria-checked="true">Orca</button>`);
    openEngineSection();
    expect(scroll).toHaveBeenCalledWith({ behavior: "smooth", block: "start" });
    expect(document.activeElement?.id).toBe("orca");
  });

  it("focuses the first checked engine with host sets", () => {
    page(`<button role="checkbox" aria-checked="false">herdr</button><button id="orca" role="checkbox" aria-checked="true">Orca</button>`);
    openEngineSection();
    expect(document.activeElement?.id).toBe("orca");
  });

  it("does not animate when the system asks for less motion", () => {
    reduce = true;
    const { scroll } = page(`<button role="radio" aria-checked="true">herdr</button>`);
    openEngineSection();
    expect(scroll).toHaveBeenCalledWith({ behavior: "auto", block: "start" });
  });

  it("focuses the section when the chosen engine cannot take focus", () => {
    const { section } = page(`<button role="radio" aria-checked="true" disabled>herdr</button>`);
    openEngineSection();
    expect(document.activeElement).toBe(section);
  });

  it("does nothing without the section", () => {
    document.body.innerHTML = `<button id="pill">Orca</button>`;
    expect(() => openEngineSection()).not.toThrow();
  });
});
