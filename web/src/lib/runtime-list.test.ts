import { describe, expect, it } from "vitest";
import { runtimeIdentity, runtimeOrder, runtimeRemoval } from "./runtime-list";
import type { Session } from "./types";
const row = (session_id: string, updated_at: string): Session => ({ machine_id: "demo", session_id, title: session_id, updated_at, cwd: "/demo", status: "idle" });

describe("runtime transport ordering", () => {
  it("orders instants rather than RFC3339 text, including offsets", () => {
    const earlier = row("a", "2026-01-01T12:00:00+01:00"), later = row("b", "2026-01-01T11:00:01Z");
    expect([earlier, later].sort(runtimeOrder)).toEqual([later, earlier]);
  });
  it("preserves Go's sub-millisecond order before using identity ties", () => {
    const earlier = row("a", "2026-01-01T00:00:00.123456780Z"), later = row("b", "2026-01-01T00:00:00.123456789Z");
    expect([earlier, later].sort(runtimeOrder)).toEqual([later, earlier]);
  });
  it("uses lexical composite identity, not a locale or slash-splitting order", () => {
    const a = row("A/with/slashes", "2026-01-01T00:00:00Z"), b = row("a", "2026-01-01T00:00:00Z");
    expect(runtimeIdentity(a)).toBe('["demo","A/with/slashes"]');
    expect([b, a].sort(runtimeOrder)).toEqual([a, b]);
  });
  it("normalizes JSON tuple removals, including Go-escaped HTML and line separators", () => {
    const id = "id/<&>/" + String.fromCharCode(8232, 8233);
    const key = runtimeIdentity(row(id, "2026-01-01T00:00:00Z"));
    expect(JSON.parse(key)).toEqual(["demo", id]);
    expect(key).toContain(String.fromCharCode(92) + "u003c");
    expect(runtimeRemoval(JSON.stringify(["demo", id]))).toBe(key);
    expect(runtimeRemoval(key)).toBe(key);
    expect(() => runtimeRemoval("demo/id")).toThrow();
  });
  it("matches UTF-8 ordering for supplementary Unicode identities", () => {
    const bmp = row("a" + String.fromCodePoint(0xe000), "2026-01-01T00:00:00Z");
    const supplementary = row("a" + String.fromCodePoint(0x10000), "2026-01-01T00:00:00Z");
    expect([supplementary, bmp].sort(runtimeOrder)).toEqual([bmp, supplementary]);
  });
});
