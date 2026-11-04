import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { isChunkError, PageBoundary, reloadsFor } from "./PageBoundary";

describe("PageBoundary", () => {
  it("knows a chunk that would not load from other errors", () => {
    expect(isChunkError(new TypeError("Failed to fetch dynamically imported module: /assets/Settings-abc.js"))).toBe(true);
    expect(isChunkError(new TypeError("Importing a module script failed."))).toBe(true);
    expect(isChunkError(new Error("Cannot read properties of undefined"))).toBe(false);
    // an API call that failed while the page drew is not a missing chunk: no reload for it
    expect(isChunkError(new TypeError("Failed to fetch"))).toBe(false);
  });
  it("reloads once for a chunk error, not again within a minute", () => {
    const e = new TypeError("Failed to fetch dynamically imported module");
    expect(reloadsFor(e, null, 1_000_000)).toBe(true);
    expect(reloadsFor(e, 990_000, 1_000_000)).toBe(false);
    expect(reloadsFor(e, 900_000, 1_000_000)).toBe(true);
    expect(reloadsFor(new Error("boom"), null, 1_000_000)).toBe(false);
  });
  it("draws its page while nothing failed, and a way out once it does", () => {
    expect(renderToStaticMarkup(<PageBoundary page="x"><p>页</p></PageBoundary>)).toBe("<p>页</p>");
    const failed = PageBoundary.getDerivedStateFromError(new TypeError("Failed to fetch dynamically imported module"));
    const b = new PageBoundary({ page: "x", children: null });
    b.state = failed;
    const html = renderToStaticMarkup(<>{b.render()}</>);
    expect(html).toContain("这一页没能加载");
    expect(html).toContain("重新加载");
  });
});
