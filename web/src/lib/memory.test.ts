import { describe, expect, it } from "vitest";
import { indexTitles, MEMORY_INDEX, memoryEntries, memoryTypeLabel, memoryTypeTone, parseMemory } from "./memory";

describe("parseMemory", () => {
  it("reads the frontmatter, type under metadata or at the top", () => {
    expect(parseMemory("---\nname: morning-format\ndescription: 早报一屏以内\nmetadata:\n  type: feedback\n---\n\n早报一屏以内，结论先行。")).toEqual({
      name: "morning-format",
      description: "早报一屏以内",
      type: "feedback",
      body: "早报一屏以内，结论先行。",
    });
    expect(parseMemory("---\r\nname: \"tea\"\r\ntype: 'user'\r\ndescription:\r\n---\r\n喜欢的茶")).toEqual({ name: "tea", type: "user", body: "喜欢的茶" });
  });

  it("keeps a file without (or with a broken) frontmatter whole", () => {
    expect(parseMemory("  只是一段话\n")).toEqual({ body: "只是一段话" });
    expect(parseMemory("---\nname: x\n没有结束")).toEqual({ body: "---\nname: x\n没有结束" });
  });

  it("keeps colons inside values", () => {
    expect(parseMemory("---\ndescription: 时间: 早上 9:00\n---\nx").description).toBe("时间: 早上 9:00");
  });
});

describe("index", () => {
  it("reads the titles and hooks", () => {
    expect(indexTitles("# 记忆\n- [早报格式](morning-format.md) — 一屏以内，结论先行\n- [周报](weekly.md)\n* [茶](tea.md): 喜欢乌龙\n随便一行")).toEqual({
      "morning-format.md": { title: "早报格式", hook: "一屏以内，结论先行" },
      "weekly.md": { title: "周报", hook: undefined },
      "tea.md": { title: "茶", hook: "喜欢乌龙" },
    });
  });
});

describe("memoryEntries", () => {
  const files = [
    { name: MEMORY_INDEX, content: "- [早报格式](morning-format.md) — 一屏以内，结论先行", updated_at: "2026-11-01T09:00:00Z" },
    { name: "morning-format.md", content: "---\nname: morning-format\nmetadata:\n  type: feedback\n---\n\n早报一屏以内。", updated_at: "2026-11-01T09:00:00Z" },
    { name: "weekly-report.md", content: "---\nname: weekly-report\ndescription: 周报先给草稿\ntype: project\n---\n\n周报先给草稿。", updated_at: "2026-10-30T09:00:00Z" },
    { name: "plain.md", content: "没有头", updated_at: "2026-11-02T09:00:00Z" },
  ];

  it("leaves out the index, newest first, the index's title first", () => {
    const e = memoryEntries(files);
    expect(e.map((x) => [x.file.name, x.title, x.detail, x.type])).toEqual([
      ["plain.md", "plain.md", undefined, undefined],
      ["morning-format.md", "早报格式", "一屏以内，结论先行", "feedback"],
      ["weekly-report.md", "weekly-report", "周报先给草稿", "project"],
    ]);
  });

  it("works without an index", () => {
    expect(memoryEntries(files.slice(1)).map((x) => x.title)).toEqual(["plain.md", "morning-format", "weekly-report"]);
  });
});

describe("types", () => {
  it("names and tints the kinds", () => {
    expect(memoryTypeLabel("user")).toBe("用户");
    expect(memoryTypeLabel("feedback")).toBe("反馈");
    expect(memoryTypeLabel("project")).toBe("项目");
    expect(memoryTypeLabel("reference")).toBe("参考");
    expect(memoryTypeLabel("other")).toBe("other");
    expect(memoryTypeLabel(undefined)).toBeUndefined();
    expect(memoryTypeTone("user")).toBe("accent");
    expect(memoryTypeTone("reference")).toBe("neutral");
  });
});
