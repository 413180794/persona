/// <reference types="node" />
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const source = (path: string) => readFileSync(new URL(path, import.meta.url), "utf8");

describe("quiet surfaces independent of restored status tints", () => {
  it("keeps the shared permission/plan/question/MCP/terminal card neutral with amber details", () => {
    const interactions = source("../views/chat/Interactions.tsx");
    const card = interactions.slice(interactions.indexOf("function Card("), interactions.indexOf("function OptionLabel("));
    expect(card).toContain("bg-surface shadow-card");
    expect(card).toContain("border-t border-line bg-inset");
    expect(card).toContain("text-attention");
    expect(card).not.toMatch(/bg-(green|orange|ai|red)-tint|text-red/);
  });

  it("keeps automatic-conversation Needs you counts amber instead of borrowing the error role", () => {
    const sidebar = source("../views/Sidebar.tsx");
    const tally = sidebar.slice(sidebar.indexOf("function TallyMark("), sidebar.indexOf("function TallyMark(") + 1100);
    expect(tally).toContain("BADGE_CLASS.attention");
    expect(tally).not.toContain("BADGE_CLASS.red");
  });
});
