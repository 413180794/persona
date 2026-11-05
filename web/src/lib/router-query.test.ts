// useRoute re-renders when only the query changes: /todos?id=A → /todos?id=B (a todo tag in the sidebar while a todo is
// open, or Back between two todos) is another route, so the page must see it.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// useRoute is one useSyncExternalStore: keep its getSnapshot to ask what React would compare (Object.is).
let snapshot: (() => unknown) | null = null;
vi.mock("react", async (orig) => ({
  ...(await orig<typeof import("react")>()),
  useSyncExternalStore: (_sub: unknown, get: () => unknown) => {
    snapshot = get;
    return get();
  },
}));

const { navigate, parse, useRoute } = await import("./router");

function fakeLocation(path: string) {
  const loc = { pathname: "", search: "" };
  const go = (to: string) => {
    const u = new URL(to, "http://hub");
    loc.pathname = u.pathname;
    loc.search = u.search;
  };
  go(path);
  vi.stubGlobal("location", loc);
  vi.stubGlobal("window", { location: loc });
  vi.stubGlobal("history", {
    state: null,
    pushState: (_s: unknown, _t: string, to: string) => go(to),
    replaceState: (_s: unknown, _t: string, to: string) => go(to),
  });
}

beforeEach(() => {
  snapshot = null;
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useRoute", () => {
  it("sees a change of the todo in the query", () => {
    fakeLocation("/todos?id=A");
    expect(useRoute()).toEqual({ name: "todos", id: "A" });
    const before = snapshot!();
    navigate({ name: "todos", id: "B" });
    // the snapshot must change, or React keeps the page on todo A
    expect(Object.is(snapshot!(), before)).toBe(false);
    expect(useRoute()).toEqual({ name: "todos", id: "B" });
  });

  it("sees the view of an agent's page in the query, and a change of it alone is a change of route", () => {
    fakeLocation("/s/m1/a1?view=term");
    expect(useRoute()).toEqual({ name: "session", mid: "m1", sid: "a1", view: "term" });
    const before = snapshot!();
    navigate({ name: "session", mid: "m1", sid: "a1", view: "chat" }, true);
    expect(location.pathname + location.search).toBe("/s/m1/a1?view=chat");
    expect(Object.is(snapshot!(), before)).toBe(false);
    expect(useRoute()).toEqual({ name: "session", mid: "m1", sid: "a1", view: "chat" });
  });

  it("leaves a view switch where it was when the address is already that", () => {
    fakeLocation("/s/m1/a1?view=term");
    const push = vi.spyOn(history, "pushState");
    navigate({ name: "session", mid: "m1", sid: "a1", view: "term" });
    expect(push).not.toHaveBeenCalled();
    navigate({ name: "session", mid: "m1", sid: "a1" }); // without a view is another address
    expect(push).toHaveBeenCalledTimes(1);
  });

  it("still parses the path alone", () => {
    fakeLocation("/s/m1/a1");
    expect(useRoute()).toEqual(parse("/s/m1/a1"));
  });
});
