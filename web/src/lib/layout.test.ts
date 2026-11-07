// @vitest-environment jsdom
// The sidebar folded to its rail (#2524) is kept in this browser: a reload comes back folded, and maximizing still
// hides it outright for the page only. Its width, dragged on a wide screen, is kept the same way.
import { beforeEach, describe, expect, it, vi } from "vitest";

const load = async () => {
  vi.resetModules();
  return import("./layout");
};

beforeEach(() => localStorage.clear());

describe("the sidebar's fold", () => {
  it("is open by default and kept in localStorage once folded", async () => {
    const l = await load();
    expect(l.getLayout().sidebarCollapsed).toBe(false);
    l.toggleSidebarColumn();
    expect(l.getLayout().sidebarCollapsed).toBe(true);
    expect(localStorage.getItem(l.COLLAPSED_KEY)).toBe("1");
    l.toggleSidebarColumn();
    expect(localStorage.getItem(l.COLLAPSED_KEY)).toBeNull();
  });

  it("comes back after a reload", async () => {
    (await load()).setSidebarCollapsed(true);
    const after = await load();
    expect(after.getLayout().sidebarCollapsed).toBe(true);
    expect(after.sidebarRail(after.getLayout())).toBe(true);
  });

  it("draws the rail, not nothing; maximizing hides the column and is not kept", async () => {
    const l = await load();
    l.setSidebarCollapsed(true);
    expect(l.sidebarShown(l.getLayout())).toBe(true);
    l.toggleMaximized();
    expect(l.sidebarShown(l.getLayout())).toBe(false);
    expect(l.sidebarRail(l.getLayout())).toBe(false);
    expect((await load()).getLayout().maximized).toBe(false);
  });

  it("follows another tab folding it", async () => {
    const l = await load();
    localStorage.setItem(l.COLLAPSED_KEY, "1");
    dispatchEvent(new StorageEvent("storage", { key: l.COLLAPSED_KEY }));
    expect(l.getLayout().sidebarCollapsed).toBe(true);
  });
});

describe("the sidebar's width", () => {
  it("is the old 284px until dragged, then kept in localStorage and back after a reload", async () => {
    const l = await load();
    expect(l.getSidebarWidth()).toBe(l.SIDEBAR_DEFAULT);
    expect(l.SIDEBAR_DEFAULT).toBe(284);
    l.setSidebarWidth(360.4);
    expect(l.getSidebarWidth()).toBe(360);
    expect(localStorage.getItem(l.SIDEBAR_WIDTH_KEY)).toBe("360");
    expect((await load()).getSidebarWidth()).toBe(360);
  });

  it("stays within its bounds, whatever is asked or was stored", async () => {
    const l = await load();
    l.setSidebarWidth(10);
    expect(l.getSidebarWidth()).toBe(l.SIDEBAR_MIN);
    l.setSidebarWidth(5000);
    expect(l.getSidebarWidth()).toBe(l.SIDEBAR_MAX);
    localStorage.setItem(l.SIDEBAR_WIDTH_KEY, "9999");
    expect((await load()).getSidebarWidth()).toBe(l.SIDEBAR_MAX);
    localStorage.setItem(l.SIDEBAR_WIDTH_KEY, "nonsense");
    expect((await load()).getSidebarWidth()).toBe(l.SIDEBAR_DEFAULT);
  });

  it("forgets the width on reset", async () => {
    const l = await load();
    l.setSidebarWidth(400);
    l.resetSidebarWidth();
    expect(l.getSidebarWidth()).toBe(l.SIDEBAR_DEFAULT);
    expect(localStorage.getItem(l.SIDEBAR_WIDTH_KEY)).toBeNull();
  });

  it("follows another tab dragging it", async () => {
    const l = await load();
    localStorage.setItem(l.SIDEBAR_WIDTH_KEY, "420");
    dispatchEvent(new StorageEvent("storage", { key: l.SIDEBAR_WIDTH_KEY }));
    expect(l.getSidebarWidth()).toBe(420);
  });

  it("is drawn within the room the window leaves, but the room never takes it under the old width", async () => {
    const { sidebarWidth, SIDEBAR_DEFAULT, SIDEBAR_MIN, SIDEBAR_MAX } = await load();
    // plenty of room: what was asked for
    expect(sidebarWidth(400, 1000)).toBe(400);
    expect(sidebarWidth(Infinity, 1000)).toBe(SIDEBAR_MAX);
    // a tighter window gives way, down to the width it always had
    expect(sidebarWidth(400, 320)).toBe(320);
    expect(sidebarWidth(400, 100)).toBe(SIDEBAR_DEFAULT);
    // dragged narrower than that is still honoured
    expect(sidebarWidth(250, 100)).toBe(250);
    expect(sidebarWidth(0, 1000)).toBe(SIDEBAR_MIN);
  });
});
