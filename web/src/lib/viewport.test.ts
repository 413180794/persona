import { describe, expect, it } from "vitest";
import { enterSends, keyboardFrame } from "./viewport";

describe("keyboardFrame", () => {
  it("follows the visible part while the keyboard covers the bottom", () => {
    expect(keyboardFrame(844, { height: 508.4, offsetTop: 120, scale: 1 })).toEqual({ top: 120, height: 508 });
  });

  it("is nothing without a keyboard, without visualViewport, or while pinch-zoomed", () => {
    expect(keyboardFrame(844, { height: 844, offsetTop: 0, scale: 1 })).toBeUndefined();
    expect(keyboardFrame(844, { height: 829, offsetTop: 0, scale: 1 })).toBeUndefined(); // a scrollbar
    expect(keyboardFrame(844, undefined)).toBeUndefined();
    expect(keyboardFrame(844, { height: 400, offsetTop: 200, scale: 2 })).toBeUndefined();
  });
});

describe("enterSends", () => {
  it("sends on Enter with a pointer, never with Shift", () => {
    expect(enterSends({ key: "Enter", shiftKey: false }, false)).toBe(true);
    expect(enterSends({ key: "Enter", shiftKey: true }, false)).toBe(false);
    expect(enterSends({ key: "a", shiftKey: false }, false)).toBe(false);
  });

  it("makes a new line on a touch screen", () => {
    expect(enterSends({ key: "Enter", shiftKey: false }, true)).toBe(false);
  });
});
