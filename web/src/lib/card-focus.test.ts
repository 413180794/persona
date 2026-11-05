import { describe, expect, it } from "vitest";
import { cardState, focusedCard, focusedFirst } from "./card-focus";

describe("card focus", () => {
  const cards = [{ id: "a" }, { id: "b" }, { id: "c" }];
  it("moves the tapped card to the front and keeps the others' order", () => {
    expect(focusedFirst(cards, "c").map((c) => c.id)).toEqual(["c", "a", "b"]);
  });
  it("returns the same list when the card is first, not there, or none was tapped", () => {
    expect(focusedFirst(cards, "a")).toBe(cards);
    expect(focusedFirst(cards, "gone")).toBe(cards);
    expect(focusedFirst(cards, undefined)).toBe(cards);
  });
  it("reads the card only from the entry that carries it", () => {
    expect(focusedCard(cardState("b"))).toBe("b");
    expect(focusedCard(null)).toBeUndefined();
    expect(focusedCard({ overlay: "x" })).toBeUndefined();
  });
});
