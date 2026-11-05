import { describe, expect, it } from "vitest";
import { clock, loopbackOnly, pairedDevice, secondsLeft, webBase } from "./connect-device";

describe("connect a new device (#2731)", () => {
  it("points the sign-in link at the page's root, under the hub's sub-path", () => {
    expect(webBase("https://hub.example.com", "")).toBe("https://hub.example.com/");
    expect(webBase("https://example.com", "/persona")).toBe("https://example.com/persona/");
  });

  it("tells an address only this computer can open", () => {
    for (const o of ["http://127.0.0.1:5173", "http://localhost:5173", "http://app.localhost", "http://[::1]:18660"]) expect(loopbackOnly(o)).toBe(true);
    for (const o of ["https://hub.example.com", "http://192.0.2.10:18660", "https://[2001:db8::1]", "not a url"]) expect(loopbackOnly(o)).toBe(false);
  });

  it("counts down to the code's expiry and stops at 0", () => {
    const now = Date.parse("2026-11-05T10:00:00Z");
    expect(secondsLeft("2026-11-05T10:10:00Z", now)).toBe(600);
    expect(secondsLeft("2026-11-05T10:00:00.400Z", now)).toBe(1);
    expect(secondsLeft("2026-11-05T09:59:00Z", now)).toBe(0);
    expect(secondsLeft("", now)).toBe(0);
    expect(clock(600)).toBe("10:00");
    expect(clock(65)).toBe("1:05");
    expect(clock(-3)).toBe("0:00");
  });

  it("finds the device that used the code: in use and new since the code was made", () => {
    const before = new Set(["a", "b"]);
    expect(pairedDevice(before, [{ id: "a" }, { id: "b" }])).toBeUndefined();
    expect(pairedDevice(before, [{ id: "a" }, { id: "c", revoked_at: "2026-11-05T10:01:00Z" }])).toBeUndefined();
    expect(pairedDevice(before, [{ id: "a" }, { id: "d" }])?.id).toBe("d");
  });
});
