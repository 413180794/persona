// @vitest-environment jsdom
// The sidebar folds (#2524): to a rail of icons with its button or ⌘⌥B / Ctrl+Alt+B (kept across reloads), and each of
// its blocks under its title: 页面, Agents, and the groups inside Agents, each fold kept in this browser.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
// jsdom has no matchMedia (the sidebar's modules read it when they load) and no ResizeObserver (its highlight)
vi.hoisted(() => {
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});
import { COLLAPSED_KEY, getLayout, setSidebarCollapsed } from "../lib/layout";
import { setAllFolded } from "../lib/sidebar-api";
import { checkBinding, eventChord, getBindings, matchShortcut } from "../lib/shortcuts";
import { useShortcuts } from "../lib/use-shortcuts";
import { Sidebar, SidebarRail } from "./Sidebar";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 5; i++) await act(async () => { await Promise.resolve(); });
};
const button = (name: string) => [...host.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.getAttribute("aria-label") === name || b.textContent?.trim() === name);
const sectionBody = (title: string) => button(title)!.closest(".mt-1, .mt-3")!.querySelector("[inert], .flex.flex-col") as HTMLElement;

beforeEach(() => {
  localStorage.clear();
  setSidebarCollapsed(false);
  vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response("{}", { headers: { "Content-Type": "application/json" } }))));
  history.replaceState(null, "", "/");
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe("folding the whole sidebar", () => {
  it("has a button at the top that folds it to the rail and keeps that", async () => {
    act(() => root.render(<Sidebar route={{ name: "home" }} onSearch={() => {}} onNewAgent={() => {}} />));
    await flush();
    const fold = button("收起侧边栏")!;
    expect(fold.getAttribute("aria-expanded")).toBe("true");
    expect(fold.title).toContain("Ctrl+Alt+B");
    act(() => fold.click());
    expect(getLayout().sidebarCollapsed).toBe(true);
    expect(localStorage.getItem(COLLAPSED_KEY)).toBe("1");
  });

  it("the rail keeps the pages as icons, their counts as dots, and its own way back", async () => {
    act(() => root.render(<SidebarRail route={{ name: "settings" }} onSearch={() => {}} onNewAgent={() => {}} />));
    await flush();
    const links = [...host.querySelectorAll("nav a")].map((a) => a.getAttribute("aria-label"));
    expect(links).toEqual(expect.arrayContaining(["Persona", "总览", "收件箱", "定时任务", "知识库", "机器", "设置"]));
    expect(host.querySelector('a[aria-label="设置"]')!.getAttribute("aria-current")).toBe("page");
    expect(host.textContent).toBe(""); // icons only: no labels, no numbers
    const open = button("展开侧边栏")!;
    expect(open.getAttribute("aria-expanded")).toBe("false");
    setSidebarCollapsed(true);
    act(() => open.click());
    expect(getLayout().sidebarCollapsed).toBe(false);
  });
});

describe("the shortcut", () => {
  function Shell({ narrow, toggleDrawer }: { narrow: boolean; toggleDrawer: () => void }) {
    useShortcuts({ route: { name: "home" }, narrow, toggleDrawer });
    return null;
  }
  const press = () => dispatchEvent(new KeyboardEvent("keydown", { key: "b", code: "KeyB", ctrlKey: true, altKey: true, bubbles: true, cancelable: true }));

  it("is Mod+Alt+B: Mod+B is already the right panel's, Mod+Shift+B the browser's", () => {
    const b = getBindings();
    expect(b["left-sidebar"]).toBe("Mod+Alt+B");
    expect(b["right-panel"]).toBe("Mod+B");
    expect(checkBinding("left-sidebar", { mod: true, alt: false, shift: false, key: "B" }, b, "other")).toMatchObject({ ok: false, reason: "conflict", with: "right-panel" });
    expect(checkBinding("left-sidebar", { mod: true, alt: false, shift: true, key: "B" }, b, "other")).toMatchObject({ ok: false, reason: "reserved" });
    const ev = { key: "b", code: "KeyB", ctrlKey: true, metaKey: false, altKey: true, shiftKey: false };
    expect(eventChord(ev, "other")?.mod).toBe(true);
    expect(matchShortcut(ev, b, "other")?.def.id).toBe("left-sidebar");
    expect(matchShortcut({ ...ev, ctrlKey: false, metaKey: true }, b, "mac")?.def.id).toBe("left-sidebar");
  });

  it("folds and opens the column on a wide screen, kept in this browser", () => {
    const drawer = vi.fn();
    act(() => root.render(<Shell narrow={false} toggleDrawer={drawer} />));
    press();
    expect(getLayout().sidebarCollapsed).toBe(true);
    expect(localStorage.getItem(COLLAPSED_KEY)).toBe("1");
    press();
    expect(getLayout().sidebarCollapsed).toBe(false);
    expect(drawer).not.toHaveBeenCalled();
  });

  it("still opens and closes the drawer on a narrow screen, folding nothing", () => {
    const drawer = vi.fn();
    act(() => root.render(<Shell narrow toggleDrawer={drawer} />));
    press();
    expect(drawer).toHaveBeenCalledTimes(1);
    expect(getLayout().sidebarCollapsed).toBe(false);
  });
});

describe("folding a block of the sidebar", () => {
  it("folds 页面 under its title and keeps it", async () => {
    act(() => root.render(<Sidebar route={{ name: "home" }} onSearch={() => {}} onNewAgent={() => {}} />));
    await flush();
    const title = button("页面")!;
    expect(title.getAttribute("aria-expanded")).toBe("true");
    act(() => title.click());
    expect(title.getAttribute("aria-expanded")).toBe("false");
    expect(localStorage.getItem("fold:pages")).toBe("1");
    expect(sectionBody("页面").hasAttribute("inert")).toBe(true);
    // the Persona row stays above it
    expect(host.querySelector('a[title="Persona 的主对话"]')).not.toBeNull();
  });

  it("folds the whole Agents block, the grouping and「+」 staying on its title", async () => {
    act(() => root.render(<Sidebar route={{ name: "home" }} onSearch={() => {}} onNewAgent={() => {}} />));
    await flush();
    const title = button("Agents")!;
    act(() => title.click());
    expect(title.getAttribute("aria-expanded")).toBe("false");
    expect(localStorage.getItem("fold:agents")).toBe("1");
    expect(button("新建 Agent")).toBeDefined();
  });

  it("starts as it was left", async () => {
    localStorage.setItem("fold:pages", "1");
    localStorage.setItem("fold:agents", "1");
    act(() => root.render(<Sidebar route={{ name: "home" }} onSearch={() => {}} onNewAgent={() => {}} />));
    await flush();
    expect(button("页面")!.getAttribute("aria-expanded")).toBe("false");
    expect(button("Agents")!.getAttribute("aria-expanded")).toBe("false");
  });

  it("leaves 页面 and Agents open when every group folds (their titles stay to read)", async () => {
    act(() => root.render(<Sidebar route={{ name: "home" }} onSearch={() => {}} onNewAgent={() => {}} />));
    await flush();
    act(() => setAllFolded(true));
    expect(button("页面")!.getAttribute("aria-expanded")).toBe("true");
    expect(button("Agents")!.getAttribute("aria-expanded")).toBe("true");
    expect(localStorage.getItem("fold:pages")).toBeNull();
  });
});

describe("the Agents list menu (#3027)", () => {
  it("has 全部折叠 and 全部展开, which open the spaces folded by hand and leave the other blocks", async () => {
    localStorage.setItem("fold:space:mac/a", "1");
    act(() => root.render(<Sidebar route={{ name: "home" }} onSearch={() => {}} onNewAgent={() => {}} />));
    await flush();
    const menu = [...host.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.getAttribute("aria-label")?.startsWith("Agent 分组"))!;
    act(() => menu.click());
    await flush();
    const item = (label: string) => [...document.querySelectorAll<HTMLElement>("[role=menuitem], [role=menuitemradio], [role=menuitemcheckbox]")].find((el) => el.textContent?.includes(label));
    expect(item("全部折叠")).toBeDefined();
    act(() => item("全部展开")!.click());
    await flush();
    expect(localStorage.getItem("fold:space:mac/a")).toBe("0");
    expect(localStorage.getItem("fold:jarvis")).toBeNull();
  });
});
