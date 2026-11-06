import { describe, expect, it } from "vitest";
import fixture from "../../../spec/fixtures/injected/cases.json";
import { injectedOf, parseInjected, taskOfInjected } from "./injected";
import type { Todo } from "./todos";
import type { Injected, Item } from "./types";

const cases = fixture.cases;

// Fields the hub leaves out are absent; compare with those dropped.
const clean = (v: Injected | null) => (v ? Object.fromEntries(Object.entries(v).filter(([, x]) => x !== undefined && x !== "")) : null);

describe("parseInjected (spec/fixtures/injected)", () => {
  for (const c of cases) {
    it(c.name, () => expect(clean(parseInjected(c.input.text))).toEqual(c.expected));
  }
});

describe("parseInjected lead blocks (#3182)", () => {
  const notice = cases.find((c) => c.name === "task_done")!.input.text;
  for (const lead of ["<persona-context>\n快照\n</persona-context>\n", '<persona-context kind="context" title="t" url="">x</persona-context>\n', "<persona-context>\n快照\n</persona-context>\n"]) {
    it(`skips ${lead.slice(0, 24)}…`, () => expect(parseInjected(lead + notice)?.kind).toBe("task_notice"));
  }
});

describe("injectedOf", () => {
  const notice = cases.find((c) => c.name === "task_done")!.input.text;
  const item = (extra: Partial<Item>): Item => ({ id: "x#0", seq: 1, role: "user", kind: "text", text: notice, ...extra });
  it("takes the hub's mark", () => {
    expect(injectedOf(item({ injected: { kind: "task_notice", line: "任务「x」做完了", todo_id: "td_1" } }))?.todo_id).toBe("td_1");
  });
  it("reads an unmarked line from an older hub", () => expect(injectedOf(item({}))?.line).toBe("任务「README 去掉扩展列表」做完了"));
  it("leaves what an app sent alone", () => expect(injectedOf(item({ client_msg_id: "c1" }))).toBeNull());
  it("only user text", () => expect(injectedOf(item({ role: "assistant" }))).toBeNull());
});

describe("taskOfInjected", () => {
  const todo = (id: string, title: string, origin: string, created_at: string) => ({ id, title, origin, created_at, updated_at: created_at }) as Todo;
  const n: Injected = { kind: "task_notice", title: "跑测试", line: "任务「跑测试」做完了" };
  it("the hub's id first", () => expect(taskOfInjected({ ...n, todo_id: "td_9" }, [], "main")).toBe("td_9"));
  it("else the newest task of that title from this conversation", () => {
    const list = [todo("a", "跑测试", "main", "2026-11-01"), todo("b", "跑测试", "main", "2026-11-04"), todo("c", "跑测试", "other", "2026-11-05")];
    expect(taskOfInjected(n, list, "main")).toBe("b");
    expect(taskOfInjected(n, list, "none")).toBeUndefined();
  });
});
