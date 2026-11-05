import { afterEach, describe, expect, it, vi } from "vitest";
import { replyMotion, replyMotionEnabled, scrollBehavior, setReplyMotionEnabled } from "./motion";

function stub(reduce: boolean) {
  const store = new Map<string, string>();
  vi.stubGlobal("localStorage", {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, v),
    removeItem: (k: string) => void store.delete(k),
  });
  vi.stubGlobal("matchMedia", () => ({ matches: reduce }));
}

afterEach(() => vi.unstubAllGlobals());

describe("回复动效", () => {
  it("is on by default and remembers being turned off", () => {
    stub(false);
    expect(replyMotionEnabled()).toBe(true);
    expect(replyMotion()).toBe(true);
    setReplyMotionEnabled(false);
    expect(replyMotionEnabled()).toBe(false);
    expect(replyMotion()).toBe(false);
    setReplyMotionEnabled(true);
    expect(replyMotion()).toBe(true);
  });

  it("is off whenever the system asks for reduced motion", () => {
    stub(true);
    expect(replyMotionEnabled()).toBe(true);
    expect(replyMotion()).toBe(false);
  });
});

describe("scrollBehavior", () => {
  it("animates unless the system asks for reduced motion", () => {
    stub(false);
    expect(scrollBehavior()).toBe("smooth");
    stub(true);
    expect(scrollBehavior()).toBe("auto");
  });

  it("animates where matchMedia is missing", () => {
    vi.stubGlobal("matchMedia", undefined);
    expect(scrollBehavior()).toBe("smooth");
  });
});
