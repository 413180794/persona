import { describe, expect, it } from "vitest";
import { FOLD_SCROLL, moreBelow, scrollsAFold, scrollsBy } from "./fold-scroll";

describe("a fold that scrolls", () => {
  const top = { scrollTop: 0, clientHeight: 240, scrollHeight: 480 };
  const middle = { ...top, scrollTop: 120 };
  const end = { ...top, scrollTop: 240 };

  it("fades while there is more below, not at its end", () => {
    expect(moreBelow(top)).toBe(true);
    expect(moreBelow(middle)).toBe(true);
    expect(moreBelow(end)).toBe(false);
    expect(moreBelow({ scrollTop: 0, clientHeight: 240, scrollHeight: 240 })).toBe(false);
  });

  it("takes the wheel until its top or end, then the chat scrolls on", () => {
    expect(scrollsBy(top, 40)).toBe(true);
    expect(scrollsBy(top, -40)).toBe(false);
    expect(scrollsBy(middle, -40)).toBe(true);
    expect(scrollsBy(end, 40)).toBe(false);
    expect(scrollsBy(end, -40)).toBe(true);
    expect(scrollsBy(middle, 0)).toBe(false);
  });

  it("is found from where the wheel is, inside the chat only", () => {
    const fold: Element = { closest: () => fold, ...middle } as unknown as Element;
    const chat = { contains: (n: unknown) => n === fold } as unknown as Element;
    const other = { contains: () => false } as unknown as Element;
    expect(scrollsAFold(fold, chat, 40)).toBe(true);
    expect(scrollsAFold(fold, other, 40)).toBe(false);
    expect(scrollsAFold(null, chat, 40)).toBe(false);
    expect(FOLD_SCROLL).toBe("data-fold-scroll");
  });
});
