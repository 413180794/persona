// @vitest-environment jsdom
// The diff's name a person reads (#4078): the comment card and the box under a Git diff show "Unstaged" / "Staged" / "Commit" …,
// never the English words written into the message for the agent.
import { describe, expect, it } from "vitest";
import { diffLabel } from "../../lib/comments";
import { diffName, spotWhere } from "./CommentCard";

describe("diffName", () => {
  it("words each diff in the page's language", () => {
    expect(diffName({ kind: "unstaged" })).toBe("未暂存");
    expect(diffName({ kind: "staged" })).toBe("已暂存");
    expect(diffName({ kind: "commit", sha: "a1b2c3d4e5f6a7b8" })).toBe("提交 a1b2c3d4");
  });

  it("keeps the agent's English in diffLabel", () => {
    expect(diffLabel({ kind: "unstaged" })).toBe("unstaged");
    expect(diffLabel({ kind: "commit", sha: "a1b2c3d4e5f6a7b8" })).toBe("commit a1b2c3d4");
  });

  it("names the diff in a code comment's place", () => {
    const where = spotWhere({ file: "src/app.ts", path: "src/app.ts", from: 12, to: 14, omitted: 0, diff: { kind: "staged" } } as Parameters<typeof spotWhere>[0]);
    expect(where).toBe("src/app.ts:12-14 · 已暂存");
  });
});
