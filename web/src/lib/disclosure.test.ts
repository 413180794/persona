import { describe, expect, it } from "vitest";
import { anchoredScroll, collapseMounted, followsAfterScroll, FOLLOW_MARGIN, FOLLOW_STEP } from "./disclosure";

// A 1000 px tall list in an 800 px view at the top of the window.
const view = { clientHeight: 800, viewTop: 0 };

describe("anchoredScroll", () => {
  it("moves the offset by however far the held edge moved", () => {
    // a row above grew by 120 px while it opened: the clicked row went down, the offset follows it
    const r = anchoredScroll({ edge: "top", at: 300, follow: false }, { ...view, scrollTop: 500, scrollHeight: 3000, edgeAt: 420, elTop: 420 });
    expect(r).toEqual({ scrollTop: 620, follow: false });
    // nothing above it changed (it unfolds below the header): no scrolling at all
    expect(anchoredScroll({ edge: "top", at: 300, follow: false }, { ...view, scrollTop: 500, scrollHeight: 3000, edgeAt: 300, elTop: 300 }).scrollTop).toBe(500);
  });

  it("holds a work row's end when it folds from its 收起 button: the header comes down", () => {
    // 2000 px of trace above the end went away this frame: the end moved up 2000, the offset goes back with it
    const r = anchoredScroll({ edge: "bottom", at: 600, follow: false }, { ...view, scrollTop: 4000, scrollHeight: 9000, edgeAt: -1400, elTop: -1440 });
    expect(r.scrollTop).toBe(2000);
  });

  it("cannot scroll past the ends: near the end a fold rests on the end", () => {
    const r = anchoredScroll({ edge: "top", at: 300, follow: false }, { ...view, scrollTop: 1200, scrollHeight: 1900, edgeAt: 250, elTop: 250 });
    expect(r.scrollTop).toBe(1100); // max = 1900 - 800
    expect(anchoredScroll({ edge: "top", at: 300, follow: false }, { ...view, scrollTop: 20, scrollHeight: 1900, edgeAt: 200, elTop: 200 }).scrollTop).toBe(0);
  });

  it("keeps following the end while the row stays in view", () => {
    // the end is 40 px further down this frame (an unfolding row moves ~25 px a frame)
    const r = anchoredScroll({ edge: "top", at: 500, follow: true }, { ...view, scrollTop: 1000, scrollHeight: 1840, edgeAt: 500, elTop: 500 });
    expect(r).toEqual({ scrollTop: 1040, follow: true });
  });

  it("stops following where the row would leave the top, and holds it there", () => {
    // the header near the top: pinning the end (40 px this frame) would take it to 30 - 40 = -10
    const r = anchoredScroll({ edge: "top", at: 30, follow: true }, { ...view, scrollTop: 1000, scrollHeight: 1840, edgeAt: 30, elTop: 30 });
    expect(r.follow).toBe(false);
    expect(r.scrollTop).toBe(1000 + 30 - FOLLOW_MARGIN); // the header at the margin
  });

  it("stops following when a lot lands at once: the row stays put", () => {
    // a folded trace's 650 steps arrived: the end is 2000 px further down this frame
    const r = anchoredScroll({ edge: "top", at: 250, follow: true }, { ...view, scrollTop: 1000, scrollHeight: 3800, edgeAt: 250, elTop: 250 });
    expect(r).toEqual({ scrollTop: 1000, follow: false });
    // just under the limit still follows
    expect(anchoredScroll({ edge: "top", at: 250, follow: true }, { ...view, scrollTop: 1000, scrollHeight: 1800 + FOLLOW_STEP, edgeAt: 250, elTop: 250 }).follow).toBe(true);
  });
});

describe("collapseMounted", () => {
  it("keeps a lazy collapse's content while it closes, drops it once closed", () => {
    expect(collapseMounted(true, false, false)).toBe(false);
    expect(collapseMounted(true, true, false)).toBe(true);
    expect(collapseMounted(true, false, true)).toBe(true);
    expect(collapseMounted(false, false, false)).toBe(true);
  });
});

describe("followsAfterScroll (#1340)", () => {
  const reader = true;
  it("keeps following when the chat's own pin lands after more content did", () => {
    // pinned at 3000; before its scroll event fired, 480 px of images came in below: the end is far, the offset did not go up
    expect(followsAfterScroll(true, { scrollTop: 3000, lastTop: 2600, gap: 480, reader: false })).toBe(true);
    expect(followsAfterScroll(true, { scrollTop: 3000, lastTop: 3000, gap: 480, reader: false })).toBe(true);
  });

  it("keeps following when the browser pulls the offset up without the reader", () => {
    // the composer re-measured: the view grew for a moment, the offset was clamped 3 px up, then the view shrank again
    expect(followsAfterScroll(true, { scrollTop: 1914, lastTop: 1917, gap: 108, reader: false })).toBe(true);
  });

  it("stops following once the reader scrolls up", () => {
    expect(followsAfterScroll(true, { scrollTop: 2400, lastTop: 3000, gap: 600, reader })).toBe(false);
    // even while content keeps landing below
    expect(followsAfterScroll(true, { scrollTop: 2990, lastTop: 3000, gap: 500, reader })).toBe(false);
  });

  it("does not start following on the way down until the end", () => {
    expect(followsAfterScroll(false, { scrollTop: 2800, lastTop: 2400, gap: 300, reader })).toBe(false);
    expect(followsAfterScroll(false, { scrollTop: 3170, lastTop: 2800, gap: 30, reader })).toBe(true);
  });

  it("near the end always follows", () => {
    expect(followsAfterScroll(false, { scrollTop: 2000, lastTop: 2600, gap: 0, reader })).toBe(true);
    expect(followsAfterScroll(true, { scrollTop: 2000, lastTop: 2600, gap: 0, reader: false })).toBe(true);
  });
});
