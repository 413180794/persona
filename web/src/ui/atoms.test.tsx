import { afterEach, describe, expect, it, vi } from "vitest";
import { useState } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { stateColor, STATE_LABEL } from "../lib/format";
import type { AgentState } from "../lib/types";
import { Menu, useMenu } from "./atoms";

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
});
