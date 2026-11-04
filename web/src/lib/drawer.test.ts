import { describe, expect, it } from "vitest";
import { dragAxis, dragProgress, drawerA11y, drawerTakes, FLING, focusBack, scrollsWith, settlesOpen, SLOP, velocityOf } from "./drawer";

describe("dragAxis: within 45° of horizontal is the drawer's", () => {
  it("waits until the finger has moved SLOP", () => {
    expect(dragAxis(3, 2)).toBeUndefined();
    expect(dragAxis(SLOP - 1, 0)).toBeUndefined();
    expect(dragAxis(SLOP, 0)).toBe("x");
  });
  it("splits at 45°: a 30° or 40° swipe is sideways, a 50° one is the page scrolling", () => {
    const at = (deg: number) => dragAxis(40 * Math.cos((deg * Math.PI) / 180), 40 * Math.sin((deg * Math.PI) / 180));
    expect(at(30)).toBe("x");
    expect(at(40)).toBe("x");
    expect(at(45)).toBe("x");
    expect(at(50)).toBe("y");
    expect(dragAxis(-30, -20)).toBe("x");
    expect(dragAxis(5, -30)).toBe("y");
  });
});

describe("drawerTakes", () => {
  it("closed it only opens to the right; open it only closes to the left", () => {
    expect(drawerTakes(false, 20)).toBe(true);
    expect(drawerTakes(false, -20)).toBe(false);
    expect(drawerTakes(true, -20)).toBe(true);
    expect(drawerTakes(true, 20)).toBe(false);
  });
});

describe("dragProgress: the drawer follows the finger", () => {
  it("opens from 0 and closes from 1 by the finger's distance over its width, clamped", () => {
    expect(dragProgress(false, 71, 284)).toBeCloseTo(0.25);
    expect(dragProgress(false, 400, 284)).toBe(1);
    expect(dragProgress(false, -10, 284)).toBe(0);
    expect(dragProgress(true, -142, 284)).toBeCloseTo(0.5);
    expect(dragProgress(true, -500, 284)).toBe(0);
    expect(dragProgress(true, 0, 0)).toBe(1);
  });
});

describe("settlesOpen: a fling goes its way, else past half", () => {
  it("follows a fast finger whatever the distance", () => {
    expect(settlesOpen(0.1, FLING + 0.05)).toBe(true);
    expect(settlesOpen(0.9, -(FLING + 0.05))).toBe(false);
  });
  it("slow: past half opens, short of it closes", () => {
    expect(settlesOpen(0.51, 0.1)).toBe(true);
    expect(settlesOpen(0.49, 0.1)).toBe(false);
    expect(settlesOpen(0.6, -0.1)).toBe(true);
    expect(settlesOpen(0.5, 0)).toBe(false);
  });
});

describe("velocityOf", () => {
  it("measures the last ~100 ms", () => {
    expect(velocityOf([])).toBe(0);
    expect(velocityOf([{ x: 0, t: 0 }])).toBe(0);
    // an early slow part is left out: 120 px over the last 100 ms
    expect(velocityOf([{ x: 0, t: 0 }, { x: 10, t: 400 }, { x: 70, t: 450 }, { x: 130, t: 500 }])).toBeCloseTo(1.2);
    expect(velocityOf([{ x: 100, t: 0 }, { x: 40, t: 60 }])).toBeCloseTo(-1);
  });
});

describe("scrollsWith: a sideways scroller under the finger scrolls first", () => {
  const box = (scrollLeft: number, overflowX = "auto") => ({ scrollLeft, scrollWidth: 600, clientWidth: 300, overflowX });
  it("takes a right drag while off its left edge, and a left drag while off its right edge", () => {
    expect(scrollsWith(box(40), 20)).toBe(true);
    expect(scrollsWith(box(0), 20)).toBe(false); // at its left edge the drawer opens, like iOS
    expect(scrollsWith(box(0), -20)).toBe(true);
    expect(scrollsWith(box(300), -20)).toBe(false);
  });
  it("is not a scroller without overflow or with nothing to scroll", () => {
    expect(scrollsWith(box(40, "visible"), 20)).toBe(false);
    expect(scrollsWith(box(40, "hidden"), 20)).toBe(false);
    expect(scrollsWith({ scrollLeft: 0, scrollWidth: 300, clientWidth: 300, overflowX: "auto" }, -20)).toBe(false);
  });
});

describe("drawerA11y", () => {
  it("narrow and shut: inert; narrow and open: a modal over an inert page; wide: none", () => {
    expect(drawerA11y(true, false)).toEqual({ inert: true, modal: false });
    expect(drawerA11y(true, true)).toEqual({ inert: false, modal: true });
    expect(drawerA11y(false, true)).toEqual({ inert: false, modal: false });
    expect(drawerA11y(false, false)).toEqual({ inert: false, modal: false });
  });
});

describe("focusBack", () => {
  const body = { isConnected: true };
  const button = { isConnected: true };
  it("returns focus to the opener when it was left in the drawer or dropped to the body", () => {
    expect(focusBack(button, {}, true, body)).toBe(button);
    expect(focusBack(button, body, false, body)).toBe(button);
    expect(focusBack(button, null, false, body)).toBe(button);
  });
  it("leaves focus that moved elsewhere, an opener gone from the page, and a body opener", () => {
    expect(focusBack(button, {}, false, body)).toBeNull();
    expect(focusBack({ isConnected: false }, body, true, body)).toBeNull();
    expect(focusBack(body, body, true, body)).toBeNull();
    expect(focusBack(null, body, true, body)).toBeNull();
  });
});
