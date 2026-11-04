import { afterEach, describe, expect, it } from "vitest";
import { LIVE_STALE_MS, liveEntry, receiveLive, resetLive, shownLive, type LiveEntry } from "./live";

afterEach(resetLive);

describe("shownLive", () => {
  const entry: LiveEntry = { live: { verb: "Frosting", tokens_down: 22_800 }, at: 1000 };

  it("shows the line while the session works and it is fresh", () => {
    expect(shownLive(entry, true, 1000 + LIVE_STALE_MS - 1)).toEqual(entry.live);
  });

  it("shows nothing once the session no longer works", () => {
    expect(shownLive(entry, false, 1000)).toBeNull();
  });

  it("shows nothing after 15 s without an update", () => {
    expect(shownLive(entry, true, 1000 + LIVE_STALE_MS)).toBeNull();
  });

  it("shows nothing without a line", () => {
    expect(shownLive(undefined, true, 1000)).toBeNull();
  });
});

describe("receiveLive", () => {
  it("keeps the latest line per session and forgets it on live: null", () => {
    receiveLive({ machine_id: "m", session_id: "s", live: { verb: "A" } }, 5);
    receiveLive({ machine_id: "m", session_id: "s", live: { verb: "B" } }, 6);
    expect(liveEntry("m", "s")).toEqual({ live: { verb: "B" }, at: 6 });
    receiveLive({ machine_id: "m", session_id: "s", live: null });
    expect(liveEntry("m", "s")).toBeUndefined();
    expect(liveEntry("m", "other")).toBeUndefined();
  });
});
