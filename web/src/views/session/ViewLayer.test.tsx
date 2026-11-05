import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { ViewLayer } from "./ViewLayer";

const layer = (props: Partial<Parameters<typeof ViewLayer>[0]> = {}) =>
  renderToStaticMarkup(<ViewLayer on from="start" {...props}><p>body</p></ViewLayer>);

describe("ViewLayer", () => {
  it("shows the one that is on, in reach", () => {
    const html = layer();
    expect(html).toContain("opacity-100");
    expect(html).not.toContain("inert");
    expect(html).not.toContain("invisible");
  });
  it("keeps the one that is off mounted, unseen and out of reach, at the same size", () => {
    const html = layer({ on: false });
    expect(html).toContain("<p>body</p>");
    expect(html).toContain("inert");
    expect(html).toContain("invisible");
    expect(html).toContain("pointer-events-none");
    expect(html).toContain("absolute inset-0");
    expect(html).not.toMatch(/(?<![-\w])hidden(?![-\w])/); // display:none would let xterm measure 100px and resize the pane
  });
  it("leaves to the side it came from", () => {
    expect(layer({ on: false, from: "start" })).toContain("-translate-x-3");
    expect(layer({ on: false, from: "end" })).toMatch(/ translate-x-3/);
  });
  it("fades in when it first appears, if asked", () => {
    expect(layer({ enter: true })).toContain("starting:opacity-0");
    expect(layer()).not.toContain("starting:");
  });
  it("is nothing but its children when bare", () => {
    expect(layer({ bare: true })).toBe("<p>body</p>");
  });
  it("turns visible at once on the way in and after the fade on the way out", () => {
    expect(layer()).toContain("visibility 0s linear 0ms");
    expect(layer({ on: false })).toContain("visibility 0s linear 200ms");
  });
});
