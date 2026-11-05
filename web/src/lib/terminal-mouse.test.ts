import { describe, expect, it } from "vitest";
import {
  CellThrottle,
  MOD_ALT,
  MOD_CTRL,
  clampedPoint,
  contextAction,
  mouseButton,
  mouseModifiers,
  mouseRoute,
  parseMouseMode,
} from "./terminal-mouse";

const keys = (o: Partial<{ shiftKey: boolean; ctrlKey: boolean; altKey: boolean; metaKey: boolean }> = {}) => ({
  shiftKey: false,
  ctrlKey: false,
  altKey: false,
  metaKey: false,
  ...o,
});

describe("buttons and modifiers", () => {
  it("maps the DOM buttons to herdr's and drops the rest", () => {
    expect([0, 1, 2].map(mouseButton)).toEqual(["left", "middle", "right"]);
    expect(mouseButton(3)).toBeNull(); // back
    expect(mouseButton(4)).toBeNull(); // forward
  });

  it("sends ctrl and alt as herdr's bits, never shift (it chose the route)", () => {
    expect(mouseModifiers(keys())).toBe(0);
    expect(mouseModifiers(keys({ ctrlKey: true }))).toBe(MOD_CTRL);
    expect(mouseModifiers(keys({ altKey: true }))).toBe(MOD_ALT);
    expect(mouseModifiers(keys({ ctrlKey: true, altKey: true, shiftKey: true, metaKey: true }))).toBe(MOD_CTRL | MOD_ALT);
  });

  it("reads the saved mode, program unless it says local", () => {
    expect(parseMouseMode(null)).toBe("program");
    expect(parseMouseMode("nonsense")).toBe("program");
    expect(parseMouseMode("local")).toBe("local");
  });
});

describe("who gets the pointer", () => {
  it("program mode: the program, Shift keeps it on the page", () => {
    expect(mouseRoute("program", keys())).toBe("program");
    expect(mouseRoute("program", keys({ ctrlKey: true, altKey: true }))).toBe("program");
    expect(mouseRoute("program", keys({ shiftKey: true }))).toBe("page");
  });

  it("local mode: the page, Shift sends it on", () => {
    expect(mouseRoute("local", keys())).toBe("page");
    expect(mouseRoute("local", keys({ shiftKey: true }))).toBe("program");
  });

  it("meta and touch always stay with the page (⌘-click opens a link; a finger scrolls and selects)", () => {
    expect(mouseRoute("program", keys({ metaKey: true }))).toBe("page");
    expect(mouseRoute("local", keys({ metaKey: true, shiftKey: true }))).toBe("page");
    expect(mouseRoute("program", keys(), "touch")).toBe("page");
    expect(mouseRoute("program", keys(), "pen")).toBe("program");
  });
});

describe("the right click", () => {
  it("is Persona's menu / the browser's with Shift while nothing is forwarded (as before)", () => {
    expect(contextAction(keys(), false, "program")).toBe("menu");
    expect(contextAction(keys({ shiftKey: true }), false, "program")).toBe("browser");
    expect(contextAction(keys(), false, "local", "touch")).toBe("browser");
  });

  it("goes to the program when forwarding, and Shift+right click is Persona's menu", () => {
    expect(contextAction(keys(), true, "program")).toBe("program");
    expect(contextAction(keys({ shiftKey: true }), true, "program")).toBe("menu");
  });

  it("local mode keeps the menu on the plain right click, Shift sends it on", () => {
    expect(contextAction(keys(), true, "local")).toBe("menu");
    expect(contextAction(keys({ shiftKey: true }), true, "local")).toBe("program");
  });

  it("leaves a touch long press to the browser (it selects text)", () => {
    expect(contextAction(keys(), true, "program", "touch")).toBe("browser");
  });
});

describe("drag throttling", () => {
  it("sends one event per cell change", () => {
    const t = new CellThrottle();
    t.reset([3, 4]); // the press was at 3,4
    expect(t.step([3, 4])).toBe(false);
    expect(t.step([4, 4])).toBe(true);
    expect(t.step([4, 4])).toBe(false);
    expect(t.step([4, 5])).toBe(true);
    expect(t.step([4, 4])).toBe(true); // back is a change too
  });

  it("the first step after a plain reset is sent", () => {
    const t = new CellThrottle();
    expect(t.step([0, 0])).toBe(true);
    t.reset();
    expect(t.step([0, 0])).toBe(true);
  });
});

describe("a drag that leaves the screen", () => {
  it("points at the nearest edge", () => {
    expect(clampedPoint(-5, 10, 800, 600)).toEqual([0, 10]);
    expect(clampedPoint(900, 700, 800, 600)).toEqual([800, 600]);
    expect(clampedPoint(10, 20, 800, 600)).toEqual([10, 20]);
  });
});
