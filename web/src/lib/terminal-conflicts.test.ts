import { describe, expect, it } from "vitest";
import { HERDR_DIRECT_KEYS, herdrOwnsKey, inFullSurface, isCtrlV, linkOpens, rightClickRoute } from "./terminal-conflicts";
import { SHORTCUTS, eventChord, matchShortcut, parseChord } from "./shortcuts";

const key = (k: string, mods: Partial<{ ctrlKey: boolean; metaKey: boolean; altKey: boolean; shiftKey: boolean }> = {}) => ({
  key: k,
  ctrlKey: false,
  metaKey: false,
  altKey: false,
  shiftKey: false,
  ...mods,
});

describe("right click in the full surface", () => {
  it("is herdr's while it has the mouse, Persona's menu with Shift", () => {
    expect(rightClickRoute({ shiftKey: false }, true)).toBe("herdr");
    expect(rightClickRoute({ shiftKey: true }, true)).toBe("menu");
  });
  it("is Persona's menu where herdr does not have the mouse, the browser's with Shift (as in the single pane)", () => {
    expect(rightClickRoute({ shiftKey: false }, false)).toBe("menu");
    expect(rightClickRoute({ shiftKey: true }, false)).toBe("browser");
  });
  it("leaves a touch long press to the phone's text selection", () => {
    expect(rightClickRoute({ shiftKey: false }, true, "touch")).toBe("browser");
    expect(rightClickRoute({ shiftKey: false }, false, "touch")).toBe("browser");
  });
  it("is the same for a mouse and a pen", () => {
    expect(rightClickRoute({ shiftKey: false }, true, "mouse")).toBe("herdr");
    expect(rightClickRoute({ shiftKey: false }, true, "pen")).toBe("herdr");
  });
});

describe("a link in herdr's screen", () => {
  it("needs ⌘ or Ctrl while herdr has the mouse", () => {
    expect(linkOpens({ metaKey: false, ctrlKey: false }, true, "mouse")).toBe(false);
    expect(linkOpens({ metaKey: true, ctrlKey: false }, true, "mouse")).toBe(true);
    expect(linkOpens({ metaKey: false, ctrlKey: true }, true, "mouse")).toBe(true);
  });
  it("opens on a plain click where herdr has no mouse, and on a tap", () => {
    expect(linkOpens({ metaKey: false, ctrlKey: false }, false, "mouse")).toBe(true);
    expect(linkOpens({ metaKey: false, ctrlKey: false }, true, "touch")).toBe(true);
  });
});

describe("herdr's own keys", () => {
  it("knows its prefix, ctrl+b, and only that chord", () => {
    expect(HERDR_DIRECT_KEYS).toEqual(["ctrl+b"]);
    expect(herdrOwnsKey(key("b", { ctrlKey: true }))).toBe(true);
    expect(herdrOwnsKey(key("B", { ctrlKey: true }))).toBe(true);
    expect(herdrOwnsKey(key("b"))).toBe(false);
    expect(herdrOwnsKey(key("b", { metaKey: true }))).toBe(false); // ⌘B on a Mac is Persona's
    expect(herdrOwnsKey(key("b", { ctrlKey: true, altKey: true }))).toBe(false); // Ctrl+Alt+B is the sidebar
    expect(herdrOwnsKey(key("b", { ctrlKey: true, shiftKey: true }))).toBe(false);
    expect(herdrOwnsKey(key("k", { ctrlKey: true }))).toBe(false);
  });
  it("reads a configured chord the way herdr's config writes it", () => {
    const f12 = ["f12"];
    expect(herdrOwnsKey(key("F12"), f12)).toBe(true);
    expect(herdrOwnsKey(key("a", { ctrlKey: true }), ["ctrl+a", "alt+shift+left"])).toBe(true);
    expect(herdrOwnsKey(key("ArrowLeft", { altKey: true, shiftKey: true }), ["ctrl+a", "alt+shift+left"])).toBe(true);
    expect(herdrOwnsKey(key("Escape"), ["esc"])).toBe(true);
  });
  it("is, off the Mac, exactly one chord of Persona's registry: Mod+B (右侧面板)", () => {
    // for every default chord of the registry: the Ctrl keypress it is off the Mac — does herdr own it?
    const clashes = SHORTCUTS.flatMap((s) => {
      const c = parseChord(s.chord)!;
      return c.key === "1-9" ? [] : [{ id: s.id, ev: key(c.key.length === 1 ? c.key.toLowerCase() : c.key, { ctrlKey: true, altKey: c.alt, shiftKey: c.shift }) }];
    }).filter(({ ev }) => herdrOwnsKey(ev));
    expect(clashes.map((c) => c.id)).toEqual(["right-panel"]);
    // on a Mac Mod is ⌘ and herdr's keys are Ctrl: nothing is shared
    for (const s of SHORTCUTS) {
      const c = parseChord(s.chord)!;
      if (c.key === "1-9") continue;
      const ev = { ...key(c.key.length === 1 ? c.key.toLowerCase() : c.key, { metaKey: true, altKey: c.alt, shiftKey: c.shift }), code: c.key.length === 1 ? `Key${c.key}` : undefined };
      expect(herdrOwnsKey(ev)).toBe(false);
      expect(eventChord(ev, "mac")?.mod ?? false).toBe(true);
    }
    expect(matchShortcut({ ...key("b", { ctrlKey: true }), code: "KeyB" }, undefined, "other")?.def.id).toBe("right-panel");
  });
});

describe("the full surface marker", () => {
  it("is found from a node inside it", () => {
    const inside = { closest: (s: string) => (s === "[data-herdr-full]" ? {} : null) };
    expect(inFullSurface(inside)).toBe(true);
    expect(inFullSurface({ closest: () => null })).toBe(false);
    expect(inFullSurface(null)).toBe(false);
    expect(inFullSurface({})).toBe(false);
  });
});

describe("Ctrl+V", () => {
  it("is looked at by the page off the Mac only, with no other modifier", () => {
    expect(isCtrlV(key("v", { ctrlKey: true }), "other")).toBe(true);
    expect(isCtrlV(key("V", { ctrlKey: true }), "other")).toBe(true);
    expect(isCtrlV(key("v", { ctrlKey: true }), "mac")).toBe(false); // ^V on a Mac is the program's; ⌘V pastes by itself
    expect(isCtrlV(key("v", { metaKey: true }), "mac")).toBe(false);
    expect(isCtrlV(key("v", { ctrlKey: true, shiftKey: true }), "other")).toBe(false); // Ctrl+Shift+V already pastes
    expect(isCtrlV(key("v", { ctrlKey: true, altKey: true }), "other")).toBe(false);
    expect(isCtrlV(key("v"), "other")).toBe(false);
  });
});
