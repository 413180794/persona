import { describe, expect, it } from "vitest";
import { assistantView, cardNumber, cardView } from "./assistant-view";

// The cases every app is held to (schema/extension/examples/persona-card/*.json, from sdk/go/wire/personacard.go): a card
// and the view it becomes, or null when it stays a code block.
const CASES = import.meta.glob("../../../schema/extension/examples/persona-card/*.json", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

describe("persona-card → 组件（和每个 App 同一套）", () => {
  it("has the shared cases", () => {
    expect(Object.keys(CASES).length).toBeGreaterThanOrEqual(5);
  });
  for (const [path, text] of Object.entries(CASES)) {
    const name = path.split("/").pop();
    it(`turns ${name} into the same view`, () => {
      const c = JSON.parse(text) as { card: unknown; view: unknown };
      expect(cardView(c.card)).toEqual(c.view ?? null);
    });
  }

  it("writes numbers grouped, one decimal at most", () => {
    expect([2250, 7.5, -1200.34, 0, -0.01, 8432, 999.96, 1234567].map(cardNumber)).toEqual(["2,250", "7.5", "-1,200.3", "0", "0", "8,432", "1,000", "1,234,567"]);
  });

  it("a card that does not read stays code", () => {
    expect(cardView({ type: "stat" })).toBeNull();
    expect(cardView({ type: "pie" })).toBeNull();
    expect(assistantView("persona-card", '{"type":"stat","value":')).toBeNull(); // still being written
  });
});

describe("persona-view", () => {
  it("draws a view or one component, without what would act", () => {
    const v = assistantView("persona-view", JSON.stringify({
      title: "本周",
      sections: [{ items: [
        { type: "stat_grid", stats: [{ label: "今天", value: "1.2M" }] },
        { type: "button", title: "刷新", action: { id: "r" } },
        { type: "row", title: "详情", action: { id: "open" } },
        { type: "link", title: "文档", url: "https://example.com" },
      ] }],
    }));
    expect(v?.title).toBe("本周");
    expect(v?.sections[0].items.map((i) => i.type)).toEqual(["stat_grid", "row", "link"]);
    expect(v?.sections[0].items[1].action).toBeUndefined();
    expect(assistantView("persona-view", JSON.stringify({ type: "markdown", text: "**hi**" }))?.sections[0].items[0].type).toBe("markdown");
    expect(assistantView("persona-view", "[]")).toBeNull();
  });

  it("draws persona-card blocks as cards too", () => {
    expect(assistantView("persona-card", JSON.stringify({ type: "stat", value: 3 }))?.sections[0].items[0]).toEqual({ type: "metric", label: "数值", value: "3" });
  });
});
