import { describe, expect, it } from "vitest";
import { applySessionDelta, isCodexHistory, sessionRefreshed, sessionSnapshot } from "./sessions-delta";
import type { Session } from "./types";

const s = (id: string, patch: Partial<Session> = {}): Session =>
  ({ machine_id: "m", session_id: id, title: id, cwd: "/code/app", status: "idle", state: "idle", ...patch }) as Session;
const ids = (list: Session[]) => list.map((x) => x.session_id);

describe("会话增量的合并", () => {
  it("改过的原地替换，新的接在后面，走了的去掉", () => {
    const base = [s("a"), s("b"), s("c")];
    const out = applySessionDelta(base, [s("b", { title: "b2" }), s("d")], ["c"]);
    expect(ids(out)).toEqual(["a", "b", "d"]);
    expect(out[1]!.title).toBe("b2");
  });

  it("空增量原样返回，不重排列表", () => {
    const base = [s("a"), s("b")];
    expect(ids(applySessionDelta(base, [], []))).toEqual(["a", "b"]);
  });

  it("一次增量里既删又加同一个 id：先按删掉，再按加进来", () => {
    expect(ids(applySessionDelta([s("a")], [s("a", { title: "again" })], ["a"]))).toEqual(["a"]);
  });

  it("多个新增保持 hub 送来的顺序", () => {
    expect(ids(applySessionDelta([], [s("z"), s("a"), s("m")], []))).toEqual(["z", "a", "m"]);
  });
});

describe("整份快照与 Codex 历史", () => {
  it("非增量 hub：快照就是全部，本地旧的不留", () => {
    expect(ids(sessionSnapshot([s("a")], [s("old")], false))).toEqual(["a"]);
  });

  it("增量 hub：快照没有 Codex 历史，本地已有的历史留着", () => {
    const history = s("h", { source_kind: "codex_history" });
    expect(ids(sessionSnapshot([s("a")], [s("gone"), history], true))).toEqual(["a", "h"]);
  });

  it("快照里已带的会话不会因本地也有而重复", () => {
    const history = s("h", { source_kind: "codex_history" });
    expect(ids(sessionSnapshot([history], [history], true))).toEqual(["h"]);
  });
});

describe("GET /sessions 的答案与推送的交错", () => {
  it("没连增量 hub：接口答案是全部事实", () => {
    expect(ids(sessionRefreshed([s("rest")], [s("pushed")], false))).toEqual(["rest"]);
  });

  it("连着增量 hub：推送的更新，只从接口取 Codex 历史", () => {
    const history = s("h", { source_kind: "codex_history" });
    expect(ids(sessionRefreshed([s("stale"), history], [s("pushed")], true))).toEqual(["pushed", "h"]);
  });

  it("机器上已经没有的历史会去掉，推送的会话照推送的留着", () => {
    const old = s("h", { source_kind: "codex_history" });
    expect(ids(sessionRefreshed([], [s("pushed"), old], true))).toEqual(["pushed"]);
  });

  it("已有的 Codex 历史换成接口给的新副本：看过、离线都跟着变（#2425）", () => {
    const had = s("h", { source_kind: "codex_history", state: "unread", status: "done" });
    const now = s("h", { source_kind: "codex_history", state: "offline", status: "offline" });
    const out = sessionRefreshed([s("stale"), now], [s("pushed"), had], true);
    expect(ids(out)).toEqual(["pushed", "h"]);
    expect(out[1].state).toBe("offline");
    expect(out[0]).toEqual(s("pushed"));
  });
});

describe("Codex 历史识别", () => {
  it("只有 source_kind 为 codex_history 才算", () => {
    expect(isCodexHistory(s("x", { source_kind: "codex_history" }))).toBe(true);
    expect(isCodexHistory(s("y", { source_kind: "herdr" }))).toBe(false);
    expect(isCodexHistory(s("z"))).toBe(false);
  });
});
