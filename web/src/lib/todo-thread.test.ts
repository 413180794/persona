import { describe, expect, it } from "vitest";
import { boundThread, shortThreadId, threadKind, threadLine, threadName, todoThreadsOn } from "./todo-thread";
import type { TodoThread } from "./todos";

const thread = (patch: Partial<TodoThread> = {}): TodoThread => ({
  channel: "feishu", chat_id: "oc_test", thread_id: "omt_1a2b3c4d5e6f", title: "测试话题", ...patch,
});

describe("shortThreadId", () => {
  it("keeps a short id whole and cuts a long one in the middle", () => {
    expect(shortThreadId("omt_test")).toBe("omt_test");
    expect(shortThreadId("omt_1a2b3c4d5e6f")).toBe("omt_1a…5e6f");
  });
  it("counts characters, not bytes", () => {
    expect(shortThreadId("话题一二三四五六七八九十")).toBe("话题一二三四五六七八九十");
    expect(shortThreadId("话题一二三四五六七八九十甲")).toBe("话题一二三四…八九十甲");
  });
});

describe("threadName", () => {
  it("is the title, else the short id", () => {
    expect(threadName(thread())).toBe("测试话题");
    expect(threadName(thread({ title: "  " }))).toBe("omt_1a…5e6f");
    expect(threadName(thread({ title: "", thread_id: "omt_test" }))).toBe("omt_test");
  });
});

describe("threadLine", () => {
  it("names a Lark topic as one, with its title or short id", () => {
    expect(threadKind(thread())).toBe("飞书话题");
    expect(threadLine(thread())).toBe("飞书话题 · 测试话题");
    expect(threadLine(thread({ title: "" }))).toBe("飞书话题 · omt_1a…5e6f");
  });
  it("names any other channel by its id", () => {
    expect(threadLine(thread({ channel: "slack" }))).toBe("话题（slack） · 测试话题");
  });
});

describe("boundThread", () => {
  it("is the thread when the three ids that name it are there", () => {
    expect(boundThread({ thread: thread() })?.thread_id).toBe("omt_1a2b3c4d5e6f");
  });
  it("is nothing for a todo without one, or with one missing an id", () => {
    expect(boundThread({})).toBeUndefined();
    expect(boundThread({ thread: thread({ thread_id: "" }) })).toBeUndefined();
    expect(boundThread({ thread: thread({ chat_id: "" }) })).toBeUndefined();
  });
});

describe("todoThreadsOn", () => {
  it("is on only when the hub names the feature, not before hello or on a hub that names others", () => {
    expect(todoThreadsOn(["locale", "todo_threads"])).toBe(true);
    expect(todoThreadsOn(["locale"])).toBe(false);
    expect(todoThreadsOn([])).toBe(false);
  });
});
