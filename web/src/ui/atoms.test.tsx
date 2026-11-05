import { afterEach, describe, expect, it, vi } from "vitest";
import { useState } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { stateColor, STATE_LABEL } from "../lib/format";
import type { AgentState } from "../lib/types";
import { Dialog, IconButton, Menu, useMenu } from "./atoms";

vi.mock("react", async (importOriginal) => ({
  ...await importOriginal<typeof import("react")>(),
  useState: vi.fn((initial) => [initial, () => {}]),
}));

vi.mock("react-dom", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-dom")>(),
  createPortal: (children: React.ReactNode) => children,
}));

afterEach(() => vi.unstubAllGlobals());

function renderMenu(state?: AgentState) {
  vi.stubGlobal("document", { body: {} });
  return renderToStaticMarkup(
    <Menu
      anchor={null}
      onClose={() => {}}
      items={[{ key: "agent", label: "#1 · 示例任务", state, checked: true, onSelect: () => {} }]}
    />,
  );
}

describe("menu anchor", () => {
  it("captures the button before a deferred state updater loses currentTarget", () => {
    const setAnchor = vi.fn();
    vi.mocked(useState).mockReturnValueOnce([undefined, setAnchor]);
    const menu = useMenu();
    const target = {} as HTMLElement;
    const event = { currentTarget: target as HTMLElement | null };
    menu.open(event as React.MouseEvent<HTMLElement>);
    event.currentTarget = null;
    const update = setAnchor.mock.calls[0][0] as (previous: HTMLElement | null) => HTMLElement | null;
    expect(update(null)).toBe(target);
    expect(update(target)).toBeNull();
  });
});

describe("agent status in menus", () => {
  it.each(Object.keys(STATE_LABEL) as AgentState[])("shows the %s dot and readable status", (state) => {
    const html = renderMenu(state);
    expect(html).toContain(`aria-label="${STATE_LABEL[state]}"`);
    expect(html).toContain(`background:${stateColor(state)}`);
    expect(html).toContain(`>${STATE_LABEL[state]}</span>`);
    expect(html).toContain(`title="#1 · 示例任务 · ${STATE_LABEL[state]}"`);
    expect(html).toContain("#1 · 示例任务</span>");
    expect(html).toContain("whitespace-nowrap");
  });

  it("does not add status UI to ordinary menu items", () => {
    const html = renderMenu();
    expect(html).not.toContain('role="img"');
    expect(html).not.toContain("已看");
    expect(html).not.toContain("title=");
    expect(html).toContain("#1 · 示例任务</span>");
  });

  it("gives a finger rows of at least 44pt", () => {
    expect(renderMenu()).toContain("pointer-coarse:min-h-11");
  });
});

describe("dialog", () => {
  function renderDialog(footer?: React.ReactNode) {
    vi.stubGlobal("document", { body: {} });
    return renderToStaticMarkup(
      <Dialog open onClose={() => {}} title="新建 Agent" width={520} footer={footer}>
        <p>正文</p>
      </Dialog>,
    );
  }

  it("is labelled by its title", () => {
    const html = renderDialog();
    const id = /<h2 id="([^"]+)"/.exec(html)?.[1];
    expect(id).toBeTruthy();
    expect(html).toContain(`aria-labelledby="${id}"`);
    expect(html).toContain('aria-modal="true"');
  });

  it("keeps the wide card's width, through a variable the phone's sheet can lift", () => {
    const html = renderDialog();
    expect(html).toContain("--dialog-w:520px");
    expect(html).toContain("max-w-(--dialog-w)");
    expect(html).toContain("max-sm:max-w-none");
    expect(html).not.toContain("max-width:520px");
  });

  it("is a sheet on a phone: on the keyboard's frame, at most 90% of it, sliding up unless motion is reduced", () => {
    const html = renderDialog();
    expect(html).toContain("max-sm:top-(--kb-top)");
    expect(html).toContain("max-sm:h-(--kb-h)");
    expect(html).toContain("max-sm:items-end");
    expect(html).toContain("max-sm:max-h-[calc(var(--kb-h)*0.9)]");
    // a transition (cut to 1ms by the reduced-motion rule in styles.css), not the wide card's keyframes
    expect(html).toContain("max-sm:starting:translate-y-full");
    expect(html).toContain("max-sm:animate-none");
  });

  it("scrolls its body and pins the footer above the home indicator", () => {
    const html = renderDialog(<button type="button">启动</button>);
    expect(html).toContain("max-sm:overflow-y-auto");
    expect(html).toMatch(/max-sm:shrink-0 max-sm:pb-\[calc\(10px\+env\(safe-area-inset-bottom\)\)\]">.*启动/);
    // without a footer the body keeps the safe area
    expect(renderDialog()).toContain("max-sm:pb-[calc(16px+env(safe-area-inset-bottom))]");
  });
});

describe("icon button", () => {
  it("has a 44pt hit area on touch screens (touch-hit), whatever its size", () => {
    const html = renderToStaticMarkup(<IconButton icon="x" label="关闭" size={20} />);
    expect(html).toContain("touch-hit");
    expect(html).toContain("width:20px");
  });
});

describe("menu with the new row kinds", () => {
  function render(items: React.ComponentProps<typeof Menu>["items"], extra: Partial<React.ComponentProps<typeof Menu>> = {}) {
    vi.stubGlobal("document", { body: {} });
    return renderToStaticMarkup(<Menu anchor={null} onClose={() => {}} items={items} {...extra} />);
  }

  it("shows a row's letter, and ⌫ on the close action", () => {
    const html = render([
      { key: "copy", label: "复制路径", accel: "c", onSelect: () => {} },
      { key: "close", label: "关闭标签页", danger: true, closes: true, confirm: "确认关闭", onSelect: () => {} },
    ]);
    expect(html).toContain('aria-keyshortcuts="c"');
    expect(html).toMatch(/>C<\/span>/);
    expect(html).toContain("⌫");
    // the question is not asked until the row is chosen
    expect(html).toContain("关闭标签页");
    expect(html).not.toContain("确认关闭");
  });

  it("marks a row with a submenu", () => {
    const html = render([{ key: "git", label: "Git", children: [], onSelect: () => {} }]);
    expect(html).toContain('aria-haspopup="menu"');
    expect(html).toContain("-rotate-90");
  });

  it("is the same markup as before for a plain row", () => {
    const html = render([{ key: "a", label: "拷贝目录", icon: "folder", onSelect: () => {} }]);
    expect(html).not.toContain("aria-haspopup");
    expect(html).not.toContain("aria-keyshortcuts");
    expect(html).not.toContain("bg-red-tint");
    expect(html).toContain('data-menu="');
  });

  it("opens at a pointer with no anchor", () => {
    expect(render([{ key: "a", label: "复制", onSelect: () => {} }], { point: { x: 10, y: 20 } })).toContain("复制");
  });
});
