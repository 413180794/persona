// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { lostWith } from "./dock-focus";

afterEach(() => {
  document.body.innerHTML = "";
});

describe("lostWith (#5154)", () => {
  it("is lost when the focused button left the page and the body has the focus", () => {
    const b = document.createElement("button");
    document.body.append(b);
    b.focus();
    b.remove();
    expect(lostWith(b, document.activeElement, document.body)).toBe(true);
  });

  it("is lost when the button was disabled while it sends", () => {
    const b = document.createElement("button");
    document.body.append(b);
    b.disabled = true;
    expect(lostWith(b, document.body, document.body)).toBe(true);
  });

  it("is lost when its fieldset was disabled", () => {
    const f = document.createElement("fieldset");
    const b = document.createElement("button");
    f.append(b);
    document.body.append(f);
    f.disabled = true;
    expect(lostWith(b, null, document.body)).toBe(true);
  });

  it("is not lost while something else has the focus", () => {
    const b = document.createElement("button");
    const other = document.createElement("textarea");
    document.body.append(other);
    expect(lostWith(b, other, document.body)).toBe(false);
  });

  it("is not lost when the button is still there and usable (a click on the page's text)", () => {
    const b = document.createElement("button");
    document.body.append(b);
    expect(lostWith(b, document.body, document.body)).toBe(false);
  });

  it("is not lost when nothing above the composer had the focus", () => {
    expect(lostWith(null, document.body, document.body)).toBe(false);
  });
});
