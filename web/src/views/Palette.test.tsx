import { afterEach, describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { closePalette, openPalette, registerContextCommands, setPaletteQuery } from "../lib/palette";
import { registerShortcut } from "../lib/shortcuts";
import { Palette } from "./Palette";
import { ShortcutHelp } from "./ShortcutHelp";
import { ShortcutSettings } from "./settings/ShortcutSettings";

// the modules read the colour scheme and this browser's settings when they load
vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  // The platform is read once and kept (lib/shortcuts.ts): set it before anything renders, or on a Mac (Node has a
  // navigator there, platform MacIntel) the first render keeps ⌘ for the whole file.
  Object.defineProperty(globalThis, "navigator", { value: { platform: "Linux x86_64" }, configurable: true, writable: true });
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
  const kept = new Map<string, string>();
  g.localStorage = { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => void kept.set(k, v), removeItem: (k: string) => void kept.delete(k) };
});

vi.mock("react-dom", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-dom")>()),
  createPortal: (children: React.ReactNode) => children,
}));

const cleanups: (() => void)[] = [];
afterEach(() => {
  while (cleanups.length) cleanups.pop()!();
  closePalette();
  vi.unstubAllGlobals();
});

function render(query: string) {
  vi.stubGlobal("document", { body: {} }); // the portal's target (createPortal is mocked)
  openPalette("jump");
  setPaletteQuery(query);
  return renderToStaticMarkup(<Palette open onClose={() => {}} onNewAgent={() => {}} />);
}

describe("the palette's command mode", () => {
  it("is the same search as ever without a `>`", () => {
    const html = render("");
    expect(html).toContain('aria-label="搜索会话"');
    expect(html).toContain("搜索会话、终端、目录、空间、机器…");
    expect(html).not.toContain("命令面板");
  });
  it("hints at `>` in the search's footer", () => {
    expect(render("")).toMatch(/&gt;<\/kbd> 命令/);
  });
  it("lists the registered commands with their chords as keycaps, under the heading of where they work", () => {
    cleanups.push(registerShortcut("right-panel", () => {}), registerShortcut("settings", () => {}), registerShortcut("collapse-tree", () => {}));
    vi.stubGlobal("navigator", { platform: "Linux x86_64" });
    const html = render(">");
    expect(html).toContain('aria-label="命令面板"');
    expect(html).toContain("输入命令…");
    expect(html).toContain("显示 / 隐藏右侧面板");
    expect(html).toMatch(/打开设置<\/span>.*?<kbd[^>]*>Ctrl<\/kbd><kbd[^>]*>,<\/kbd>/);
    expect(html).toMatch(/折叠侧边栏的全部分组<\/span>.*?>Ctrl<\/kbd>.*?>Alt<\/kbd>.*?>Shift<\/kbd>.*?>↑<\/kbd>/);
    for (const heading of ["全局", "会话页", "侧边栏"]) expect(html).toContain(`uppercase">${heading}</div>`);
    // a command nothing handles here is not listed
    expect(html).not.toContain("终端 ⇄ 对话");
  });
  it("filters by what follows the `>`", () => {
    cleanups.push(registerShortcut("right-panel", () => {}), registerShortcut("settings", () => {}));
    const html = render(">设置");
    expect(html).toContain("打开设置");
    expect(html).not.toContain("显示 / 隐藏右侧面板");
  });
  it("lists what other modules registered, first, under their own heading", () => {
    cleanups.push(registerContextCommands(() => [{ id: "node:rename", section: "节点", title: "重命名…", run: () => {} }]));
    const html = render(">");
    expect(html).toContain("重命名…");
    expect(html.indexOf("重命名…")).toBeLessThan(html.indexOf("新建 Agent"));
    expect(html).toContain('uppercase">节点</div>');
  });
  it("says so when no command matches, and how to get back to the search", () => {
    const html = render(">zzzz");
    expect(html).toContain("没有匹配的命令");
    expect(html).toContain("删掉「&gt;」");
  });
});

describe("the shortcut help", () => {
  it("is closed until it is opened (the dialog draws nothing)", () => {
    expect(renderToStaticMarkup(<ShortcutHelp />)).toBe("");
  });
});

describe("设置 › 快捷键", () => {
  it("lists every command with its chord, and offers 全部恢复默认", () => {
    const html = renderToStaticMarkup(<ShortcutSettings />);
    expect(html).toContain("命令面板");
    expect(html).toContain("全部恢复默认");
    expect(html).toContain("跳转到会话或终端窗格");
    expect(html).toContain("切到第 1–9 台电脑");
    // nothing is rebound: no per-row 恢复默认
    expect(html).not.toContain("的默认快捷键");
  });
});
