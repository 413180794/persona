import { afterEach, describe, expect, it, vi } from "vitest";
import {
  canSendStep, connectorState, connectorSummary, connectorTone, deletedLine, feedsFooter, fieldInput, githubInboxFooter, githubMachineLine,
  githubOptions, githubScopeAdvice, isDeviceCode, isStarting, labelColor, listConnectors, mediaLine, mergeNewest, mergeOlder, mutableRepo,
  otherConnectors, setGitHubAuth, signsInHere, startsGroup, switchFooter, undescribed,
  type HubConnector, type ThreadMessage, type ThreadPage,
} from "./connectors";

const conn = (status: string, extra: Partial<HubConnector> = {}): HubConnector => ({ id: "github", name: "GitHub", status, ...extra });

afterEach(() => vi.unstubAllGlobals());

describe("连接器的状态", () => {
  it("says each state and its dot; an unknown state is not 出错了", () => {
    expect(connectorState(conn("not_signed_in"))).toBe("未登录");
    expect(connectorState(conn("rate_limited"))).toBe("请求太多，暂停中");
    expect(connectorState(conn("weird"))).toBe("状态未知");
    expect(["ok", "rate_limited", "error", "no_permission", "disabled", "weird"].map((s) => connectorTone(conn(s)))).toEqual([
      "green", "orange", "red", "red", "idle", "idle",
    ]);
    expect(connectorTone(null)).toBe("idle");
  });

  it("names the account when connected, else the state", () => {
    expect(connectorSummary(conn("ok", { account: "alice" }))).toBe("alice");
    expect(connectorSummary(conn("ok"))).toBe("已连接");
    expect(connectorSummary(conn("disabled", { account: "alice" }))).toBe("已关闭");
  });

  it("tells a sign-in here from a program that has not started", () => {
    expect(signsInHere(conn("ok", { capabilities: ["status", "login"] }))).toBe(true);
    expect(signsInHere(conn("ok", { capabilities: ["status"] }))).toBe(false);
    expect(undescribed(conn("error", { capabilities: [] }))).toBe(true);
    expect(undescribed(conn("ok"))).toBe(false); // an older hub without capabilities
    expect(isStarting(conn("error", { capabilities: [], enabled: true }))).toBe(true);
    expect(isStarting(conn("disabled", { capabilities: [], enabled: false }))).toBe(false);
  });

  it("leaves mail and Feishu to their own groups", () => {
    const list = [conn("ok", { id: "lark" }), conn("ok", { id: "mail" }), conn("ok"), conn("ok", { id: "telegram" })];
    expect(otherConnectors(list).map((c) => c.id)).toEqual(["github", "telegram"]);
  });

  it("words the footers", () => {
    expect(switchFooter(conn("ok"))).toContain("关掉后不再读 GitHub");
    expect(switchFooter(conn("disabled", { id: "telegram", name: "Telegram", enabled: false }))).toContain("私聊和群");
    expect(feedsFooter(conn("ok", { archive: true, inbox: false }))).toBe(
      "进知识库：GitHub 的内容存进知识库，助手和 AI 分身都能查。关掉后不再存新的，已存的留着。\n不进收件箱：不再收新的，已有的条目留着。",
    );
    expect(feedsFooter(conn("ok"))).toBe("");
    expect(deletedLine({ chats: 3, people: 5, inbox: 2 })).toBe("已删除：知识库里 3 个会话、5 个人，收件箱里 2 条。");
    expect(deletedLine({ chats: 1 }, false)).toBe("已删除：知识库里 1 个会话、0 个人。");
  });
});

describe("分步登录", () => {
  it("sends a step only when every field is filled in", () => {
    const p = { step: "credentials", fields: [{ name: "api_id", label: "api_id" }, { name: "api_hash", label: "api_hash", secret: true }] };
    expect(canSendStep(p, { api_id: "1" })).toBe(false);
    expect(canSendStep(p, { api_id: "1", api_hash: "  " })).toBe(false);
    expect(canSendStep(p, { api_id: "1", api_hash: "x" })).toBe(true);
    expect(canSendStep(null, {})).toBe(false);
  });

  it("knows a device code and each field's input", () => {
    expect(isDeviceCode({ kind: "device_code", user_code: "ABCD-1234" })).toBe(true);
    expect(isDeviceCode({ kind: "device_code" })).toBe(false);
    expect(fieldInput({ name: "password", label: "", secret: true }).type).toBe("password");
    expect(fieldInput({ name: "phone", label: "" }).inputMode).toBe("tel");
    expect(fieldInput({ name: "code", label: "" }).inputMode).toBe("numeric");
  });
});

describe("GitHub", () => {
  it("says each computer's gh login", () => {
    expect(githubMachineLine({ id: "a", name: "A", state: "signed_in", account: "alice" })).toBe("gh 已登录 · alice");
    expect(githubMachineLine({ id: "a", name: "A", state: "signed_in", missing: [{ scope: "repo", affects: [] }, { scope: "notifications", affects: [] }] }))
      .toBe("gh 已登录 · 少 repo、notifications 权限");
    expect(githubMachineLine({ id: "a", name: "A", state: "offline" })).toBe("离线");
    expect(githubMachineLine({ id: "a", name: "A", state: "error", error: "boom" })).toBe("读不了 gh 的登录：boom");
  });

  it("advises on missing scopes by where the token comes from", () => {
    const missing = [{ scope: "repo", affects: [] }, { scope: "notifications", affects: [] }];
    expect(githubScopeAdvice({ source: "machine", machine: "m", machines: [{ id: "m", name: "MacBook" }] }, missing)).toBe(
      "MacBook 的 gh 少了 repo、notifications 权限：在那台电脑上运行 gh auth refresh -s repo,notifications，或者改用设备码登录。",
    );
    expect(githubScopeAdvice({ source: "device", machines: [] }, missing)).toContain("设备码登录少了");
  });

  it("reads its inbox options and checks a repository to mute", () => {
    expect(githubOptions({ ci: "mine", muted_repos: ["a/b", 3] })).toEqual({ ci: "mine", muted_repos: ["a/b"], ignore_branches: undefined });
    expect(githubOptions({ muted_repos: [] })).toBeNull();
    expect(githubOptions(null)).toBeNull();
    expect(mutableRepo(" owner/name ", [])).toBe("owner/name");
    expect(mutableRepo("owner/name", ["owner/name"])).toBeNull();
    expect(mutableRepo("owner", [])).toBeNull();
    expect(githubInboxFooter({ ci: "off", muted_repos: [] })).toMatch(/^CI 失败不进收件箱。.*自动分支（train\/\*）不提醒/);
  });

  it("asks the hub with the right bodies", async () => {
    const calls: { url: string; init: RequestInit }[] = [];
    vi.stubGlobal("localStorage", { getItem: () => "jd_test" });
    vi.stubGlobal("fetch", vi.fn(async (url: string, init: RequestInit) => {
      calls.push({ url, init });
      return new Response(JSON.stringify({ connectors: [conn("ok")], source: "device", machines: [] }), { status: 200 });
    }));
    expect((await listConnectors(true)).map((c) => c.id)).toEqual(["github"]);
    await setGitHubAuth(null);
    await setGitHubAuth("mbp");
    expect(calls.map((c) => c.url)).toEqual(["/api/v1/connectors?all=1", "/api/v1/connectors/github/auth", "/api/v1/connectors/github/auth"]);
    expect(calls.slice(1).map((c) => c.init.body)).toEqual(['{"source":"device"}', '{"source":"machine","machine":"mbp"}']);
  });
});

describe("会话页", () => {
  const msg = (id: string, extra: Partial<ThreadMessage> = {}): ThreadMessage => ({ id, at: "2026-11-01T09:00:00Z", sender: { id: "u1", name: "示例" }, ...extra });
  const page = (ids: string[], before?: string): ThreadPage => ({ chat: { id: "c", name: "c", kind: "dm" }, messages: ids.map((i) => msg(i)), before });

  it("puts an older page before, one copy per id", () => {
    const merged = mergeOlder(page(["3", "4"], "3"), page(["1", "2", "3"], "1"));
    expect(merged.messages.map((m) => m.id)).toEqual(["1", "2", "3", "4"]);
    expect(merged.before).toBe("1");
  });

  it("puts the newest page in place of the newest messages, keeping the older ones", () => {
    const have = page(["1", "2", "3", "4"], "1");
    expect(mergeNewest(have, page(["3", "4", "5"], "3")).messages.map((m) => m.id)).toEqual(["1", "2", "3", "4", "5"]);
    expect(mergeNewest(have, page(["3", "4", "5"], "3")).before).toBe("1");
    expect(mergeNewest(have, page(["9"])).messages.map((m) => m.id)).toEqual(["9"]); // nothing in common: the new page
    expect(mergeNewest(null, page(["1"])).messages.length).toBe(1);
  });

  it("groups a sender's messages within five minutes", () => {
    const a = msg("1");
    expect(startsGroup(msg("2", { at: "2026-11-01T09:04:00Z" }), a)).toBe(false);
    expect(startsGroup(msg("2", { at: "2026-11-01T09:06:00Z" }), a)).toBe(true);
    expect(startsGroup(msg("2", { sender: { name: "我" }, mine: true }), a)).toBe(true);
    expect(startsGroup(msg("2", { reply_to: { id: "1" } }), a)).toBe(true);
    expect(startsGroup(a)).toBe(true);
  });

  it("describes media and label colors", () => {
    expect(mediaLine({ kind: "voice", duration: 72 })).toBe("语音 1:12");
    expect(mediaLine({ kind: "file", name: "示例.pdf", size: 284000 })).toBe("示例.pdf · 277 KB");
    expect(mediaLine({ kind: "file", size: 3 << 20 })).toBe("文件 · 3.0 MB");
    expect(labelColor("a2eeef")).toBe("#a2eeef");
    expect(labelColor("red")).toBeUndefined();
  });
});
