import { describe, expect, it } from "vitest";
import type { LarkMessage } from "./inbox";
import { historyProblem, LATEST_KEPT, latestOf, nearTop, OlderPaging, screensFromEnd, showsJumpToLatest } from "./lark-history";

const msg = (i: number): LarkMessage => ({ id: `m${i}`, position: String(i), text: `${i}` });
const range = (a: number, b: number) => Array.from({ length: b - a + 1 }, (_, i) => msg(a + i));

describe("OlderPaging", () => {
  it("asks for one page at a time and nothing without a cursor", () => {
    const p = new OlderPaging();
    expect(p.begin(undefined)).toBeNull();
    const req = p.begin("21");
    expect(req?.cursor).toBe("21");
    expect(p.loading).toBe(true);
    expect(p.begin("21")).toBeNull();
  });

  it("drops rows already shown and keeps the page's cursor", () => {
    const p = new OlderPaging();
    const req = p.begin("21")!;
    const got = p.finish(req, { messages: range(1, 21), has_older: true, older_cursor: "1" }, range(21, 40));
    expect(got?.messages.map((m) => m.id)).toEqual(range(1, 20).map((m) => m.id));
    expect(got?.older_cursor).toBe("1");
    expect(p.problem).toBeNull();
    expect(p.loading).toBe(false);
  });

  it("stops on an empty or duplicate page that claims more, and retries the same boundary", () => {
    const p = new OlderPaging();
    const req = p.begin("21")!;
    const got = p.finish(req, { messages: [msg(21)], has_older: true, older_cursor: "20" }, range(21, 40));
    expect(got?.messages).toEqual([]);
    expect(got?.older_cursor).toBe("21");
    expect(p.problem).toBe("stopped");
    expect(p.begin("21")).toBeNull();
    expect(p.begin("21", true)).not.toBeNull();
  });

  it("stops on a cursor seen before", () => {
    const p = new OlderPaging();
    let req = p.begin("21")!;
    p.finish(req, { messages: range(11, 20), has_older: true, older_cursor: "11" }, range(21, 40));
    req = p.begin("11")!;
    const got = p.finish(req, { messages: range(5, 10), has_older: true, older_cursor: "21" }, range(11, 40));
    expect(got?.messages.length).toBe(6);
    expect(p.problem).toBe("stopped");
  });

  it("ends quietly on a final empty page", () => {
    const p = new OlderPaging();
    const req = p.begin("1")!;
    const got = p.finish(req, { messages: [], has_older: false }, range(1, 20));
    expect(got?.has_older).toBe(false);
    expect(p.problem).toBeNull();
  });

  it("drops a reply that comes after the window was replaced", () => {
    const p = new OlderPaging();
    const req = p.begin("21")!;
    p.reset();
    expect(p.finish(req, { messages: range(1, 20) }, [])).toBeNull();
    expect(p.fail(req, "failed")).toBe(false);
    expect(p.problem).toBeNull();
  });

  it("waits for a retry after a failure", () => {
    const p = new OlderPaging();
    const req = p.begin("21")!;
    expect(p.fail(req, historyProblem(undefined))).toBe(true);
    expect(p.problem).toBe("failed");
    expect(p.begin("21")).toBeNull();
    expect(p.begin("21", true)).not.toBeNull();
    expect(p.problem).toBeNull();
  });
});

describe("historyProblem", () => {
  it("names the account cases", () => {
    expect(historyProblem("lark_account_changed")).toBe("account_changed");
    expect(historyProblem("lark_not_logged_in")).toBe("signed_out");
    expect(historyProblem("other")).toBe("failed");
  });
});

describe("scroll geometry", () => {
  it("is near the top within half a screen", () => {
    expect(nearTop({ scrollTop: 200, clientHeight: 400 })).toBe(true);
    expect(nearTop({ scrollTop: 201, clientHeight: 400 })).toBe(false);
  });

  it("shows 回到最新 more than a screen up, or when the window is not at the newest", () => {
    const at = (top: number) => ({ scrollTop: top, clientHeight: 400, scrollHeight: 2000 });
    expect(screensFromEnd(at(1600))).toBe(0);
    expect(showsJumpToLatest(false, at(1200))).toBe(false);
    expect(showsJumpToLatest(false, at(1100))).toBe(true);
    expect(showsJumpToLatest(true, at(1600))).toBe(true);
  });
});

describe("latestOf", () => {
  it("keeps the newest messages and marks the window as at the newest", () => {
    const kept = latestOf({ messages: range(1, 100), has_older: false, has_newer: true, newer_cursor: "x" });
    expect(kept.messages.length).toBe(LATEST_KEPT);
    expect(kept.messages[0].id).toBe("m21");
    expect(kept.has_newer).toBe(false);
    expect(kept.has_older).toBe(true);
    expect(kept.older_cursor).toBe("21");
  });

  it("leaves a short window as it is", () => {
    const kept = latestOf({ messages: range(1, 10), has_older: true, older_cursor: "1" });
    expect(kept.messages.length).toBe(10);
    expect(kept.older_cursor).toBe("1");
  });
});
