import { afterEach, describe, expect, it, vi } from "vitest";
import { api, CLIENT_FEATURES } from "./api";
import { answerLines } from "./answered";
import { defaults, dropElicitationDraft, elicitationDraft, elicitationResolution, elicitationSource, fields, numberInputMode, saveElicitationDraft, supported, urlHost, urlTarget, validate } from "./elicitation";
import { interactionAsk, interactionTitle } from "./inbox";
import type { Interaction } from "./types";

const schema = {
  type: "object",
  required: ["name", "count"],
  properties: {
    name: { type: "string", title: "名字", minLength: 2, default: "张三" },
    count: { type: "integer", minimum: 1, maximum: 5 },
    ratio: { type: "number", multipleOf: 0.1 },
    ok: { type: "boolean", default: true },
    color: { type: "string", enum: ["r", "g"], enumNames: ["红", "绿"] },
    tags: { type: "array", items: { type: "string", enum: ["a", "b", "c"] }, maxItems: 2 },
    token: { type: "string", format: "password", default: "never" },
    mail: { type: "string", format: "email" },
    day: { type: "string", format: "date" },
  },
};

const form = (s: Record<string, unknown> = schema): Interaction => ({
  id: "ix_1", machine_id: "m", session_id: "s", kind: "elicitation", mcp_server_name: "example-mcp",
  elicitation: { mode: "form", message: "请填写", requested_schema: s },
});

describe("elicitation form", () => {
  it("字段、必填和默认值（密码不预填）", () => {
    const list = fields(schema);
    expect(list.map((f) => f.key)).toEqual(["name", "count", "ratio", "ok", "color", "tags", "token", "mail", "day"]);
    expect(list.find((f) => f.key === "name")).toMatchObject({ title: "名字", required: true });
    expect(list.find((f) => f.key === "token")?.secret).toBe(true);
    expect(list.find((f) => f.key === "tags")?.choices).toEqual(["a", "b", "c"]);
    expect(defaults(schema)).toEqual({ name: "张三", ok: true });
    expect(supported(schema)).toBe(true);
  });

  it("逐项校验，数字从文本转成数", () => {
    const v = validate(schema, { name: "李四", count: "3", ratio: "0.3", tags: ["a", "b"], mail: "me@example.com", day: "2026-02-28" });
    expect(v.valid).toBe(true);
    expect(v.content).toEqual({ name: "李四", count: 3, ratio: 0.3, tags: ["a", "b"], mail: "me@example.com", day: "2026-02-28" });
    const bad = validate(schema, { name: "李", count: "1.5", ratio: "0.35", tags: ["a", "b", "c"], mail: "nope", day: "2026-02-30" });
    expect(bad.valid).toBe(false);
    expect(Object.keys(bad.errors).sort()).toEqual(["count", "day", "mail", "name", "ratio", "tags"]);
    expect(bad.errors.name).toBe("请检查填写内容和格式");
    expect(validate(schema, {}).errors).toEqual({ name: "请填写这一项", count: "请填写这一项" });
    expect(validate(schema, { name: "李四", count: "9" }).errors.count).toBeDefined();
    expect(validate(schema, { name: "李四", count: "3", color: "b" }).errors.color).toBeDefined();
  });

  it("超出子集的 schema 不能在网页上提交", () => {
    expect(supported({ ...schema, oneOf: [] })).toBe(false);
    expect(supported({ type: "object", properties: { x: { type: "object" } } })).toBe(false);
    expect(supported({ type: "object", properties: { x: { type: "string", pattern: "^a" } } })).toBe(false);
    expect(supported({ type: "object", properties: { x: { type: "string", format: "ipv4" } } })).toBe(false);
    expect(supported({ type: "object", properties: {}, required: ["missing"] })).toBe(false);
    expect(supported({ type: "object", properties: { x: { type: "array", items: { type: "string" } } } })).toBe(false);
    expect(supported(undefined)).toBe(false);
    const ix = form({ type: "object", properties: { x: { type: "string", pattern: "^a" } } });
    expect(elicitationResolution(ix, "accept", { x: "a" })).toBeUndefined();
    expect(elicitationResolution(ix, "decline", {})).toEqual({ action: "decline" });
  });

  it("提交 / 拒绝 / 取消对应 accept / decline / cancel", () => {
    const ix = form();
    expect(elicitationResolution(ix, "accept", { name: "李" , count: "2" })).toBeUndefined();
    expect(elicitationResolution(ix, "accept", { name: "李四", count: "2", ok: false })).toEqual({ action: "accept", content: { name: "李四", count: 2, ok: false } });
    expect(elicitationResolution(ix, "decline", {})).toEqual({ action: "decline" });
    expect(elicitationResolution(ix, "cancel", {})).toEqual({ action: "cancel" });
    expect(elicitationResolution({ ...ix, kind: "question" }, "decline", {})).toBeUndefined();
  });
});

describe("elicitation url", () => {
  const url = "https://example.com/authorize";
  const ix: Interaction = { id: "ix_2", machine_id: "m", session_id: "s", kind: "elicitation", elicitation: { mode: "url", url, message: "请打开链接完成确认" } };

  it("只有打开过这个链接才能继续", () => {
    expect(elicitationResolution(ix, "accept", {})).toBeUndefined();
    expect(elicitationResolution(ix, "accept", {}, url)).toEqual({ action: "accept" });
    expect(elicitationResolution({ ...ix, elicitation: { mode: "url", url: `${url}?new=1` } }, "accept", {}, url)).toBeUndefined();
    expect(elicitationResolution(ix, "cancel", {})).toEqual({ action: "cancel" });
  });

  it("只认带主机、不带账号的 http(s) 链接", () => {
    expect(urlTarget(url)).toBe(url);
    expect(urlHost(url)).toBe("example.com");
    for (const bad of ["javascript:alert(1)", "https:///missing-host", "https:example.com", "https://user:pw@example.com/", "ftp://example.com/", "", undefined]) {
      expect(urlTarget(bad)).toBeUndefined();
      expect(elicitationResolution({ ...ix, elicitation: { mode: "url", url: bad } }, "accept", {}, bad)).toBeUndefined();
    }
  });
});

describe("elicitation 的文案", () => {
  it("来源、标题、摘要", () => {
    expect(elicitationSource({ ...form(), agent_type: "worker", mcp_config_source: "project" })).toEqual(["来自子 Agent · worker", "来自 MCP 服务器 example-mcp（配置来源：project）"]);
    expect(elicitationSource(form())).toEqual(["来自 MCP 服务器 example-mcp"]);
    // Persona's own server and a plugin's read as on Android (mcpServerLabel)
    expect(elicitationSource({ ...form(), mcp_server_name: "plugin_persona" })).toEqual(["来自 MCP 服务器 Persona"]);
    expect(elicitationSource({ ...form(), mcp_server_name: "plugin_docs" })).toEqual(["来自 MCP 服务器 docs"]);
    expect(interactionTitle(form())).toBe("MCP 表单等你填写");
    expect(interactionTitle({ ...form(), elicitation: { mode: "url", url: "https://example.com" } })).toBe("MCP 链接等你确认");
    expect(interactionAsk(form())).toBe("请填写");
  });

  it("答过的卡片：填了什么，密码只说填过", () => {
    expect(answerLines(form(), { action: "accept", content: { name: "李四", ok: false, color: "g", tags: ["a", "b"], token: "x" } })).toEqual([
      { label: "名字", answer: "李四" },
      { label: "ok", answer: "否" },
      { label: "color", answer: "绿" },
      { label: "tags", answer: "a，b" },
      { label: "token", answer: "（已填写）" },
    ]);
    expect(answerLines(form(), { action: "decline" })[0].answer).toBe("已拒绝");
    expect(answerLines(form(), { action: "cancel" })[0].answer).toBe("已取消");
    expect(answerLines({ ...form(), elicitation: { mode: "url", url: "https://example.com" } }, { action: "accept" })[0].answer).toBe("已打开链接，继续");
  });
});

describe("声明能力", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("每个请求带 X-Persona-Client，和 client_hello 的 platform / version 一致", async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response("{\"interactions\":[]}", { status: 200 }));
    vi.stubGlobal("fetch", fetcher);
    await api("/interactions", { token: "t" });
    const headers = fetcher.mock.calls[0][1].headers as Headers;
    expect(headers.get("X-Persona-Client")).toBe("web/1");
    expect(CLIENT_FEATURES).toContain("elicitation");
  });
});

describe("elicitation 的输入和草稿", () => {
  it("数字键盘只给不能是负数的字段（手机的数字键盘没有负号）", () => {
    const f = (props: Record<string, unknown>) => fields({ type: "object", properties: { n: props } })[0];
    expect(numberInputMode(f({ type: "integer", minimum: 1 }))).toBe("numeric");
    expect(numberInputMode(f({ type: "number", exclusiveMinimum: 0 }))).toBe("decimal");
    expect(numberInputMode(f({ type: "integer" }))).toBeUndefined();
    expect(numberInputMode(f({ type: "number", minimum: -5 }))).toBeUndefined();
    expect(numberInputMode(f({ type: "string" }))).toBeUndefined();
  });

  it("草稿留着填过的和打开过的链接，不留密码", () => {
    saveElicitationDraft("ix_d", schema, { values: { name: "李四", token: "secret" }, opened: "https://example.com/a" });
    expect(elicitationDraft("ix_d")).toEqual({ values: { name: "李四" }, opened: "https://example.com/a" });
    dropElicitationDraft("ix_d");
    expect(elicitationDraft("ix_d")).toBeUndefined();
  });
});
