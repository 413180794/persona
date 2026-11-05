import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { NoOtherLooks } from "./Persona";

// 形象 with no looks beyond the built-in ones (#3597): it says where to get one in words a user can follow, with
// 设置 › 扩展 as a link, never a file in the repository.
describe("the picker with no other looks", () => {
  const html = renderToStaticMarkup(<NoOtherLooks />);

  it("links to 设置 › 扩展", () => {
    expect(html).toContain('href="/settings?tab=extensions"');
    expect(html).toMatch(/<a [^>]*>设置 › 扩展<\/a>/);
    expect(html).toContain("~/.persona/personas/");
  });

  it("names no repository file", () => {
    expect(html).not.toMatch(/docs\/|\.md\b/);
  });
});
