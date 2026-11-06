// @vitest-environment jsdom
// The "Needs you" section's count writes messages and waiting agents apart (#3288): the sidebar's "Inbox" badge counts messages only.
import { afterEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});
import { initI18n } from "../../i18n";
import { sectionCount } from "./InboxList";

describe("sectionCount", { timeout: 30_000 }, () => {
  afterEach(() => initI18n("zh-Hans"), 30_000);

  it("says what it counts", () => {
    expect(sectionCount(4, 0)).toEqual({ text: "4", label: "4 条消息" });
    expect(sectionCount(0, 7)).toEqual({ text: "7 个 Agent", label: "7 个 Agent" });
    expect(sectionCount(4, 7)).toEqual({ text: "4 条 · 7 个 Agent", label: "4 条消息，7 个 Agent" });
  });

  it("gives the messages their unit next to the agents' too, as a phone shows no hover (#4146)", async () => {
    await initI18n("ja");
    expect(sectionCount(5, 7).text).toBe("メッセージ5件 · エージェント7件");
  });

  it("uses English plurals", async () => {
    await initI18n("en");
    expect(sectionCount(1, 1)).toEqual({ text: "1 message · 1 agent", label: "1 message, 1 agent" });
    expect(sectionCount(2, 3)).toEqual({ text: "2 messages · 3 agents", label: "2 messages, 3 agents" });
  });
});
