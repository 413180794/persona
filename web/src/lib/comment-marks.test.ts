// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { anchorOf, nearest, rangeFor, textOf } from "./comment-marks";

function scope(html: string): HTMLElement {
  document.body.innerHTML = `<div id="s">${html}</div>`;
  return document.getElementById("s")!;
}

function select(node: Node, from: number, to: number): Range {
  const r = document.createRange();
  r.setStart(node, from);
  r.setEnd(node, to);
  return r;
}

beforeEach(() => {
  document.body.innerHTML = "";
});

describe("where picked words sit in the transcript", () => {
  it("anchors a selection to its message and finds it again after a re-render", () => {
    const s = scope('<div data-items="m1 m2"><p>Use <b>the</b> cache first.</p><p>Then retry.</p></div>');
    const p2 = s.querySelectorAll("p")[1].firstChild!;
    const a = anchorOf(select(p2, 0, 4))!;
    expect(a).toMatchObject({ item: "m1", exact: "Then" });
    expect(textOf(s.firstElementChild!)).toBe("Use the cache first.Then retry.");
    // the same words in new nodes
    scope('<div data-items="m1"><p>Use the cache first.</p><p>Then retry.</p></div>');
    const r = rangeFor(document.body, a)!;
    expect(r.toString()).toBe("Then");
  });

  it("looks for the words near where they were when the text moved, and gives up when they are gone", () => {
    const s = scope('<div data-items="m1"><p>one two one two</p></div>');
    const a = anchorOf(select(s.querySelector("p")!.firstChild!, 12, 15))!;
    expect(a.exact).toBe("two");
    scope('<div data-items="m1"><p>intro: one two one two</p></div>');
    const moved = rangeFor(document.body, a)!;
    expect(moved.toString()).toBe("two");
    expect(moved.startOffset).toBe(19); // the second "two": the words before it match, though the first is nearer to the old offset
    scope('<div data-items="m1"><p>nothing alike</p></div>');
    expect(rangeFor(document.body, a)).toBeNull();
    scope('<div data-items="other"><p>one two</p></div>');
    expect(rangeFor(document.body, a)).toBeNull();
  });

  it("anchors nothing for words outside a message or across two", () => {
    const s = scope('<div data-items="a"><p>first</p></div><div data-items="b"><p>second</p></div><p id="x">free</p>');
    const across = document.createRange();
    across.setStart(s.querySelector("p")!.firstChild!, 0);
    across.setEnd(s.querySelectorAll("p")[1].firstChild!, 3);
    expect(anchorOf(across)).toBeNull();
    expect(anchorOf(select(s.querySelector("#x")!.firstChild!, 0, 4))).toBeNull();
  });

  it("finds the nearest of several matches", () => {
    expect(nearest("ab ab ab", "ab", 4)).toBe(3);
    expect(nearest("ab ab ab", "ab", 100)).toBe(6);
    expect(nearest("abc", "x", 0)).toBe(-1);
    expect(nearest("abc", "", 0)).toBe(-1);
  });
});
