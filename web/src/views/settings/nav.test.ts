import { describe, expect, it } from "vitest";
import { FROM_LIST, LIST_GROUPS, openedFromList, settingsAt, settingsHref, TABS } from "./nav";

describe("settings location", () => {
  it("names no category on plain /settings: the phone's list (通用 on a wide screen)", () => {
    expect(settingsAt("")).toEqual({ tab: null, sub: null });
    expect(settingsAt("?tab=nope")).toEqual({ tab: null, sub: null });
  });
  it("reads the category and the item its page has open", () => {
    expect(settingsAt("?tab=codex")).toEqual({ tab: "codex", sub: null });
    expect(settingsAt("?tab=model-config&purpose=agent.triage")).toEqual({ tab: "model-config", sub: "agent.triage" });
    expect(settingsAt("?tab=codex&purpose=x")).toEqual({ tab: "codex", sub: null });
  });
  it("names 通用 on a phone, where /settings is the list, and keeps /settings for it on a wide screen", () => {
    expect(settingsHref("general", null, false)).toBe("/settings");
    expect(settingsHref("general", null, true)).toBe("/settings?tab=general");
    expect(settingsHref("extensions", "-channels/wechat", false)).toBe("/settings?tab=extensions&extension=-channels%2Fwechat");
    expect(settingsHref("hub", "ignored", true)).toBe("/settings?tab=hub");
  });
  it("round-trips every category through its URL", () => {
    for (const t of TABS) expect(settingsAt(settingsHref(t, null, true).split("?")[1] ?? "").tab).toBe(t);
  });
  it("lists each category once on the phone's first page", () => {
    const listed = LIST_GROUPS.flatMap((g) => g.tabs);
    expect([...listed].sort()).toEqual([...TABS].sort());
  });
  it("knows a category was opened from the list (Back goes to it)", () => {
    expect(openedFromList({ [FROM_LIST]: true, personaOverlay: "o1" })).toBe(true);
    expect(openedFromList(null)).toBe(false);
    expect(openedFromList({ personaOverlay: "o1" })).toBe(false);
  });
  it("opens a link from before the rename (?tab=plugins&plugin=…) as 扩展", () => {
    expect(settingsAt("?tab=plugins&plugin=-channels%2Fwechat")).toEqual({ tab: "extensions", sub: "-channels/wechat" });
    expect(settingsAt("?tab=extensions&extension=x")).toEqual({ tab: "extensions", sub: "x" });
  });
});
