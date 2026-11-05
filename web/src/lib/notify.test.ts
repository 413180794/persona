import { describe, expect, it } from "vitest";
import { DEFAULT_PREFS, durationText, normalize, notificationText, wants } from "./notify";

const units = { s: (n: number) => `${n} 秒`, m: (n: number) => `${n} 分钟` };
const words = { needs: (n: string) => `${n} 需要你`, finished: (n: string) => `${n} 做完了`, needsBody: "等你", finishedBody: "做完" };

describe("通知偏好", () => {
  it("需要你看开关", () => {
    expect(wants("needs", { input: true, done: "off" })).toBe(true);
    expect(wants("needs", { input: false, done: "always" })).toBe(false);
  });
  it("做完：不推送 / 每次都推", () => {
    expect(wants("finished", { input: true, done: "off" }, 10 * 60_000)).toBe(false);
    expect(wants("finished", { input: true, done: "always" }, 1000)).toBe(true);
    expect(wants("finished", { input: true, done: "always" })).toBe(true);
  });
  it("只推长任务：按 hub 给的 long_turn_seconds，没给按 60 秒；不知道多长不算长", () => {
    const p = { input: true, done: "long" as const };
    expect(wants("finished", p, 59_000)).toBe(false);
    expect(wants("finished", p, 60_000)).toBe(true);
    expect(wants("finished", { ...p, long_turn_seconds: 120 }, 90_000)).toBe(false);
    expect(wants("finished", { ...p, long_turn_seconds: 30 }, 30_000)).toBe(true);
    expect(wants("finished", p)).toBe(false);
  });
  it("hub 的回答补默认值，不认识的做完档按只推长任务", () => {
    expect(normalize({})).toEqual({ ...DEFAULT_PREFS, long_turn_seconds: undefined, done_delay_seconds: undefined });
    expect(normalize({ input: false, done: "weird" as never, long_turn_seconds: 90 }).done).toBe("long");
    expect(normalize({ input: false, done: "always" })).toMatchObject({ input: false, done: "always" });
  });
});

describe("时长", () => {
  it("整分钟说分钟，其余说秒，没给按 60 秒（和 App 一样）", () => {
    expect(durationText(undefined, units)).toBe("1 分钟");
    expect(durationText(120, units)).toBe("2 分钟");
    expect(durationText(90, units)).toBe("90 秒");
    expect(durationText(30, units)).toBe("30 秒");
  });
});

describe("通知文字", () => {
  it("标题是会话名和发生了什么，正文是最后一句（压成一行、最多 200 字）", () => {
    const n = notificationText({ title: "修登录", cwd: "/p", last_text: "要不要\n继续？" }, "needs", words);
    expect(n).toEqual({ title: "修登录 需要你", body: "要不要 继续？" });
    expect(notificationText({ title: "", cwd: "/x/proj", last_text: "a".repeat(300) }, "finished", words).body).toHaveLength(200);
  });
  it("没有最后一句时用默认的话", () => {
    expect(notificationText({ title: "t", cwd: "/p" }, "finished", words)).toEqual({ title: "t 做完了", body: "做完" });
  });
});
