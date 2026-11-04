import { describe, expect, it } from "vitest";
import { splitDevices } from "./devices";

describe("splitDevices", () => {
  const tokens = [
    { id: "old", last_seen: "2026-11-01T10:00:00Z" },
    { id: "gone-early", last_seen: "2026-11-04T09:00:00Z", revoked_at: "2026-11-02T08:00:00Z" },
    { id: "me", last_seen: "2026-11-03T10:00:00Z" },
    { id: "recent", last_seen: "2026-11-04T10:00:00Z" },
    { id: "gone-late", revoked_at: "2026-11-04T08:00:00Z" },
  ];

  it("keeps revoked credentials out of the devices in use", () => {
    const { active, revoked } = splitDevices(tokens, "me");
    expect(active.map((t) => t.id)).toEqual(["me", "recent", "old"]); // this browser first, then by last use
    expect(revoked.map((t) => t.id)).toEqual(["gone-late", "gone-early"]); // the latest revoked first
  });

  it("works without a current browser and without revoked ones", () => {
    const { active, revoked } = splitDevices(tokens.filter((t) => !t.revoked_at));
    expect(active.map((t) => t.id)).toEqual(["recent", "me", "old"]);
    expect(revoked).toEqual([]);
  });
});
