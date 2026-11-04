import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { Collapse } from "./atoms";

describe("Collapse", () => {
  it("runs the grid's row between 0fr and 1fr, inert while closed", () => {
    const closed = renderToStaticMarkup(<Collapse open={false}><p>trace</p></Collapse>);
    expect(closed).toContain("grid-template-rows:0fr");
    expect(closed).toContain("inert");
    expect(closed).toContain("trace"); // not lazy: always there
    const open = renderToStaticMarkup(<Collapse open><p>trace</p></Collapse>);
    expect(open).toContain("grid-template-rows:1fr");
    expect(open).not.toContain("inert");
  });

  it("mounts a lazy collapse's children only when open", () => {
    expect(renderToStaticMarkup(<Collapse open={false} lazy><p>trace</p></Collapse>)).not.toContain("trace");
    expect(renderToStaticMarkup(<Collapse open lazy><p>trace</p></Collapse>)).toContain("trace");
  });
});
