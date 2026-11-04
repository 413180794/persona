import { describe, expect, it } from "vitest";
import { directoryPanelKey } from "./panel";
describe("directory panel identity", () => {
  it("remounts files and Git for different Agent-free workspace directories", () => {
    const a = { mid: "computer", sid: "", cwd: "/projects/a" };
    const b = { ...a, cwd: "/projects/b" };
    expect(directoryPanelKey(a)).not.toBe(directoryPanelKey(b));
    expect(directoryPanelKey(a)).toBe(directoryPanelKey({ ...a }));
  });
  it("retains the actual machine and session identities without inventing a session ID", () => {
    const p = { mid: "computer", sid: "real-session", cwd: "/projects/a" };
    expect(JSON.parse(directoryPanelKey(p))).toEqual([p.mid, p.sid, p.cwd]);
    expect(directoryPanelKey(p)).not.toBe(directoryPanelKey({ ...p, mid: "other" }));
    expect(directoryPanelKey(p)).not.toBe(directoryPanelKey({ ...p, sid: "another" }));
  });
});
