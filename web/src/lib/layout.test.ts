// @vitest-environment jsdom
// The sidebar folded to its rail (#2524) is kept in this browser: a reload comes back folded, and maximizing still
// hides it outright for the page only.
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
