import { describe, expect, it } from "vitest";
import { scaledValue } from "./text-scale";

describe("text scale", () => {
  it("scales font sizes in px, rem and tokens, and px line heights", () => {
    expect(scaledValue("font-size", "12.5px")).toBe("calc(12.5px * var(--text-scale, 1))");
    expect(scaledValue("font-size", "0.75rem")).toBe("calc(0.75rem * var(--text-scale, 1))");
    expect(scaledValue("font-size", "var(--text-sm)")).toBe("calc(var(--text-sm) * var(--text-scale, 1))");
    expect(scaledValue("line-height", "20px")).toBe("calc(20px * var(--text-scale, 1))");
    // Tailwind v4's leading-4 / leading-4.5
    expect(scaledValue("line-height", "calc(var(--spacing) * 4)")).toBe("calc(calc(var(--spacing) * 4) * var(--text-scale, 1))");
    expect(scaledValue("line-height", "calc(var(--spacing)*4.5)")).toBe("calc(calc(var(--spacing)*4.5) * var(--text-scale, 1))");
  });
  it("leaves what follows its parent, keywords, unitless line heights and its own output", () => {
    for (const [p, v] of [["font-size", "1.3em"], ["font-size", "inherit"], ["font-size", "90%"], ["line-height", "1.55"], ["font-size", "calc(12px * var(--text-scale, 1))"], ["padding", "12px"], ["font-size", "var(--a, 12px)"]]) {
      expect(scaledValue(p, v)).toBeNull();
    }
  });
});
