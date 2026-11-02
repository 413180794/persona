import { describe, expect, it } from "vitest";
import { parseAPIError } from "./api-errors";
import { buildTaskIndex, loopCadence, loopLabel, loopNext, loopShortLabel, loopTask, taskBadge, taskPill, wakeSummary } from "./tasks";
import type { Item } from "./types";

describe("buildTaskIndex", () => {
  it("finds the call a notice came from by its task id", () => {
    const items: Item[] = [
      { id: "t1#0", seq: 1, ts: "2026-11-02T10:00:00Z", role: "assistant", kind: "tool_use", tool_use_id: "call1", tool_name: "Bash", tool_input: { command: "make", description: "构建", run_in_background: true } },
      { id: "r1#0", seq: 2, role: "user", kind: "tool_result", tool_use_id: "call1", text: "Command running in background with ID: bk1. Output is being written to: /tmp/bk1.output" },
      { id: "n1#0", seq: 3, ts: "2026-11-02T10:05:00Z", role: "system", kind: "notice", text: "后台命令完成 · make", task: { kind: "shell", status: "completed", title: "make", task_id: "bk1" } },
    ];
    const x = buildTaskIndex(items);
    expect(x.started.has("call1")).toBe(true);
    expect(x.calls.get("n1#0")).toBe("call1");
    expect(x.descriptions.get("call1")).toBe("构建");
    expect(taskBadge(x.notes.get("call1")!).text).toBe("后台 · 已完成");
    expect(taskPill(items[2].task!, x.startedAt.get("call1"), items[2].ts)).toEqual({ text: "已完成 · 5 分", tone: "green" });
  });

  it("badges a call still running, with its events", () => {
    expect(taskBadge([]).text).toBe("后台 · 运行中");
    expect(taskBadge([{ kind: "monitor", status: "event" }, { kind: "monitor", status: "event" }]).text).toBe("后台 · 2 个事件");
    expect(taskBadge([{ kind: "shell", status: "failed", exit_code: 1 }])).toEqual({ text: "后台 · 失败", failed: true });
  });
});

describe("wakeSummary", () => {
  it("reads an older hub's summary", () => {
    expect(wakeSummary({ id: "n", seq: 1, role: "system", kind: "notice", text: 'Background command "Release" completed (exit code 0)' })).toEqual({ text: "后台命令完成 · Release", failed: false });
    expect(wakeSummary({ id: "n", seq: 1, role: "system", kind: "notice", text: 'Background command "lint" failed with exit code 2' }).failed).toBe(true);
  });
  it("takes the hub's line when it read the notification", () => {
    expect(wakeSummary({ id: "n", seq: 1, role: "system", kind: "notice", text: "后台命令失败 · lint", task: { kind: "shell", status: "failed", title: "lint" } })).toEqual({ text: "后台命令失败 · lint", failed: true });
  });
});

describe("loops", () => {
  const now = Date.parse("2026-11-02T10:00:00Z");
  it("reads the cadence and the next wakeup", () => {
    const loop = { kind: "wakeup", prompt: "/loop check CI", interval_seconds: 1800, next_at: "2026-11-02T10:04:00Z" };
    expect(loopTask(loop)).toBe("check CI");
    expect(loopCadence(loop)).toBe("每 30 分钟");
    expect(loopLabel(loop, now)).toMatch(/^下次 \d\d:\d\d$/);
    expect(loopShortLabel(loop, now)).toMatch(/^\d\d:\d\d$/);
    expect(loopNext(loop, now)).toContain("还有 4 分钟");
    expect(loopLabel({ ...loop, next_at: "2026-11-02T09:00:00Z" }, now)).toBe("循环执行中");
    expect(loopCadence({ kind: "cron", cron: "33 17 * * *" })).toBe("按计划 33 17 * * *");
  });
});

describe("parseAPIError", () => {
  it("explains common errors", () => {
    expect(parseAPIError("API Error: 401 {\"type\":\"authentication_error\"} · Please run /login")).toMatchObject({ status: 401, transient: false, reason: "登录过期了，要在电脑上重新 /login" });
    expect(parseAPIError("API 错误：Overloaded（正在重试）")).toMatchObject({ transient: true, reason: "模型服务太忙，过载了" });
    const gpu = parseAPIError("API Error: 502 upstream error: Req 202610302115A1B2C3D4 Session 1ebd0000:7e470000 no available GPU pod");
    expect(gpu).toMatchObject({ status: 502, transient: true, requestId: "202610302115A1B2C3D4", session: "1ebd0000:7e470000" });
    expect(parseAPIError("all good")).toBeNull();
  });
});
