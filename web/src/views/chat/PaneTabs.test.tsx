import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { Session } from "../../lib/types";
import { nextTab, PaneTabs } from "./PaneTabs";

const pane = (id: string, agent: string, title: string, state: Session["state"] = "idle"): Session => ({ machine_id: "mac", session_id: id, title, cwd: "/code", status: "idle", state, agent });
const noop = () => {};

describe("PaneTabs", () => {
  it("is nothing for a tab with one pane", () => {
    expect(renderToStaticMarkup(<PaneTabs sessions={[pane("a", "claude", "修 bug")]} current="a" onSelect={noop} />)).toBe("");
    expect(renderToStaticMarkup(<PaneTabs sessions={[]} current="a" onSelect={noop} />)).toBe("");
  });

  it("is a tab list with the current pane selected: its state, agent and title", () => {
    const html = renderToStaticMarkup(<PaneTabs sessions={[pane("a", "claude", "修 bug", "working"), pane("b", "codex", "写测试", "needs_you")]} current="b" onSelect={noop} />);
    expect(html).toContain('role="tablist"');
    expect(html.match(/role="tab"/g)).toHaveLength(2);
    const tabs = html.split('role="tab"').slice(1);
    expect(tabs[0]).toContain('aria-selected="false"');
    expect(tabs[0]).toContain('tabindex="-1"');
    expect(tabs[0]).toContain(">Claude<");
    expect(tabs[0]).toContain("修 bug");
    expect(tabs[0]).toContain('aria-label="工作中"');
    expect(tabs[1]).toContain('aria-selected="true"');
    expect(tabs[1]).toContain('tabindex="0"');
    expect(tabs[1]).toContain(">Codex<");
    expect(tabs[1]).toContain('aria-label="需要你"');
  });

  it("names an agent the app does not know by its label", () => {
    const html = renderToStaticMarkup(<PaneTabs sessions={[pane("a", "claude", "x"), { ...pane("b", "dsh", "y"), agent_label: "DSH" }]} current="a" onSelect={noop} />);
    expect(html).toContain("DSH");
  });

  it("is tall enough to hit, and taller on a touch screen", () => {
    const html = renderToStaticMarkup(<PaneTabs sessions={[pane("a", "claude", "x"), pane("b", "codex", "y")]} current="a" onSelect={noop} />);
    expect(html).toContain("h-9");
    expect(html).toContain("[@media(pointer:coarse)]:h-11");
  });
});

describe("nextTab", () => {
  it("moves with the arrow keys, wrapping, and jumps with Home / End", () => {
    expect(nextTab(0, "ArrowRight", 3)).toBe(1);
    expect(nextTab(2, "ArrowRight", 3)).toBe(0);
    expect(nextTab(0, "ArrowLeft", 3)).toBe(2);
    expect(nextTab(1, "Home", 3)).toBe(0);
    expect(nextTab(1, "End", 3)).toBe(2);
    expect(nextTab(1, "Enter", 3)).toBeUndefined();
  });
});
