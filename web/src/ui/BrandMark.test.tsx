import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import type { BrandId } from "../lib/connectors";
import { BrandMark, type BrandFit } from "./BrandMark";

const draw = (brand: BrandId, fit: BrandFit) => renderToStaticMarkup(<BrandMark brand={brand} size={32} fit={fit} />);
const px = (html: string) => Number(/width="(\d+)"/.exec(html)?.[1] ?? /width:(\d+)px/.exec(html)?.[1]);

describe("BrandMark", () => {
  it("draws each brand's own image; none falls back to Telegram's", () => {
    const src = (b: BrandId) => /src="([^"]+)"/.exec(draw(b, "tile"))?.[1];
    expect(src("lark")).toBeTruthy();
    expect(src("lark")).not.toBe(src("telegram"));
    expect(draw("github", "tile")).toContain("mask"); // a template: one color, drawn in the text color
  });

  it("keeps the sizes the marks had: Telegram's fills its box, the others sit smaller", () => {
    const sizes = (b: BrandId) => [px(draw(b, "tile")), px(draw(b, "badge"))];
    expect(["telegram", "lark", "github"].map((b) => sizes(b as BrandId))).toEqual([[25, 32], [19, 23], [19, 23]]);
  });
});
