// The Git panel's two kinds of saved-away changes keep apart in Chinese (#3671): git stash and what `git add`
// recorded get different words, so the stash count on the branch card no longer reads like one staged file.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const zh = JSON.parse(readFileSync(join(__dirname, "../../i18n/locales/zh-Hans/session-gitpanel.json"), "utf8")) as Record<string, string>;

describe("the Git panel's words for stash and staged", () => {
  it("calls a stash 贮藏, never 暂存", () => {
    for (const key of ["stashes_one", "stashes_other", "stashes_hint"]) {
      expect(zh[key], key).not.toContain("暂存");
    }
    expect(zh.stashes_one).toContain("贮藏");
    expect(zh.stashes_other).toContain("贮藏");
  });

  it("keeps 暂存 for staged files", () => {
    expect(zh.staged).toBe("已暂存");
    expect(zh.partly_staged).toBe("部分已暂存");
    expect(zh.unstaged).toBe("未暂存");
  });
});
