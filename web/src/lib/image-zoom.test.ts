import { describe, expect, it } from "vitest";
import { clampView, doubleTapView, fingers, IDENTITY, isZoomed, panView, wheelFactor, zoomAt, ZOOM_MAX } from "./image-zoom";

// A 400×300 picture fitted at (100, 50) in an 600×400 area.
const fit = { x: 100, y: 50, width: 400, height: 300 };
const area = { width: 600, height: 400 };

describe("zoomAt", () => {
  it("keeps the point under the pointer still", () => {
    const p = { x: 200, y: 100 };
    const v = zoomAt(IDENTITY, 2, p, fit, area);
    expect(v.scale).toBe(2);
    // the picture point under p before (100, 50 in its own fitted pixels) is under p after
    expect(fit.x + v.x + 100 * v.scale).toBeCloseTo(p.x);
    expect(fit.y + v.y + 50 * v.scale).toBeCloseTo(p.y);
  });
  it("holds the scale between 1 and 6", () => {
    expect(zoomAt(IDENTITY, 0.5, { x: 300, y: 200 }, fit, area)).toEqual(IDENTITY);
    expect(zoomAt(IDENTITY, 100, { x: 300, y: 200 }, fit, area).scale).toBe(ZOOM_MAX);
  });
  it("comes back to rest at 1×", () => {
    const v = zoomAt(IDENTITY, 3, { x: 120, y: 60 }, fit, area);
    expect(zoomAt(v, 1 / 3, { x: 500, y: 300 }, fit, area)).toEqual(IDENTITY);
  });
});

describe("clampView", () => {
  it("keeps a picture wider than the area covering it", () => {
    const v = clampView({ scale: 2, x: 500, y: -900 }, fit, area); // 800×600 shown
    expect(fit.x + v.x).toBe(0); // left edge at most the area's left
    expect(fit.y + v.y + 600).toBe(400); // bottom edge at least the area's bottom
  });
  it("centres a side that fits", () => {
    const wide = { x: 0, y: 150, width: 600, height: 100 }; // a strip: at 2× it is 1200×200, still shorter than 400
    const v = clampView({ scale: 2, x: 0, y: 77 }, wide, area);
    expect(wide.y + v.y).toBe((400 - 200) / 2);
  });
});

describe("panView", () => {
  it("moves a zoomed picture, not past its edges", () => {
    const v = zoomAt(IDENTITY, 2, { x: 300, y: 200 }, fit, area);
    const moved = panView(v, 30, 0, fit, area);
    expect(moved.x).toBe(v.x + 30);
    expect(panView(v, 10_000, 10_000, fit, area)).toEqual({ scale: 2, x: -fit.x, y: -fit.y });
  });
  it("does nothing at 1×", () => {
    expect(panView(IDENTITY, 40, 40, fit, area)).toEqual(IDENTITY);
  });
});

describe("doubleTapView", () => {
  it("zooms in at the point, then back out", () => {
    const p = { x: 200, y: 100 };
    const v = doubleTapView(IDENTITY, p, fit, area, { width: 400, height: 300 });
    expect(v.scale).toBe(2.5);
    expect(doubleTapView(v, p, fit, area, { width: 400, height: 300 })).toEqual(IDENTITY);
  });
  it("goes to the real size when that is bigger, at most 6×", () => {
    expect(doubleTapView(IDENTITY, { x: 300, y: 200 }, fit, area, { width: 1600, height: 1200 }).scale).toBe(4);
    expect(doubleTapView(IDENTITY, { x: 300, y: 200 }, fit, area, { width: 4000, height: 3000 }).scale).toBe(ZOOM_MAX);
  });
});

describe("isZoomed / fingers", () => {
  it("reads a view and two touches", () => {
    expect(isZoomed(IDENTITY)).toBe(false);
    expect(isZoomed({ scale: 1.5, x: 0, y: 0 })).toBe(true);
    expect(fingers({ x: 0, y: 0 }, { x: 30, y: 40 })).toEqual({ dist: 50, mid: { x: 15, y: 20 } });
  });
});

describe("wheelFactor", () => {
  it("zooms in on a wheel up and out on a wheel down", () => {
    expect(wheelFactor({ deltaY: -100, deltaMode: 0, ctrlKey: false })).toBeGreaterThan(1);
    expect(wheelFactor({ deltaY: 100, deltaMode: 0, ctrlKey: false })).toBeLessThan(1);
  });
  it("takes a trackpad pinch (ctrl + small deltas) at the same pace as the fingers", () => {
    const pinch = wheelFactor({ deltaY: -10, deltaMode: 0, ctrlKey: true });
    expect(pinch).toBeGreaterThan(1.05);
    expect(pinch).toBeLessThan(1.2);
  });
  it("counts lines as pixels", () => {
    expect(wheelFactor({ deltaY: -3, deltaMode: 1, ctrlKey: false })).toBeCloseTo(wheelFactor({ deltaY: -48, deltaMode: 0, ctrlKey: false }));
  });
});
