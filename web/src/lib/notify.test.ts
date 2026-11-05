import { describe, expect, it, vi } from "vitest";
import { DEFAULT_PREFS, durationText, normalize, notificationText, waitingCard, wants } from "./notify";
import type { Interaction } from "./types";

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

describe("收回系统通知（#3458）", () => {
  it("关掉本页建的和 service worker 建的那条", async () => {
    const closed: string[] = [];
    class FakeNote {
      onclick: (() => void) | null = null;
      onclose: (() => void) | null = null;
      constructor(public title: string, public opts: { tag: string }) {}
      close() {
        closed.push(`page:${this.opts.tag}`);
        this.onclose?.();
      }
    }
    const swNote = { close: () => closed.push("sw:m/a#needs") };
    const getNotifications = vi.fn(async ({ tag }: { tag: string }) => (tag === "m/a#needs" ? [swNote] : []));
    vi.stubGlobal("Notification", Object.assign(FakeNote, { permission: "granted" }));
    vi.stubGlobal("navigator", { serviceWorker: { getRegistration: async () => ({ getNotifications }) } });
    const store = new Map([["persona.systemNotify", "1"]]);
    vi.stubGlobal("localStorage", { getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => store.set(k, v), removeItem: (k: string) => store.delete(k) });
    const { postNotification, closeNotification } = await import("./notify");
    postNotification({ title: "a 需要你", body: "", tag: "m/a#needs", url: "/" }, () => {});
    closeNotification("m/a#needs");
    await new Promise((r) => setTimeout(r, 0));
    expect(closed).toEqual(["page:m/a#needs", "sw:m/a#needs"]);
    expect(getNotifications).toHaveBeenCalledWith({ tag: "m/a#needs" });
    closed.length = 0;
    closeNotification("m/a#needs"); // already taken back: only the worker is asked again
    await new Promise((r) => setTimeout(r, 0));
    expect(closed).toEqual(["sw:m/a#needs"]);
    vi.unstubAllGlobals();
  });
});

describe("收回后同一件事马上能再弹（#3458 review）", () => {
  it("A 弹出 → 别处答了收回 → 5 秒内又要 B：B 能弹出来", async () => {
    const store = new Map<string, string>();
    vi.stubGlobal("localStorage", { getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => store.set(k, v), removeItem: (k: string) => store.delete(k) });
    vi.stubGlobal("navigator", {});
    const { claimNotification, closeNotification } = await import("./notify");
    const now = 1_000_000;
    expect(claimNotification("m/a#needs", now)).toBe(true); // A
    expect(claimNotification("m/a#needs", now + 1000)).toBe(false); // another tab, same event: still once
    claimNotification("m/b#needs", now);
    closeNotification("m/a#needs"); // A answered on the phone
    expect(claimNotification("m/a#needs", now + 2000)).toBe(true); // B
    expect(claimNotification("m/b#needs", now + 2000)).toBe(false); // the other tag's claim is kept
    vi.unstubAllGlobals();
  });
});

describe("需要你的通知写清楚问什么（#4056）", () => {
  const asks = { ...words, asks: (k: Interaction["kind"], n: string) => `${n} ${k}` };
  const s = { machine_id: "m", session_id: "a", title: "修登录", cwd: "/p", last_text: "我先看一下" };
  const ix = (over: Partial<Interaction>): Interaction => ({ id: "i", machine_id: "m", session_id: "a", kind: "question", ...over });
  it("提问：标题说在问，正文是问题，不是最后一句", () => {
    expect(notificationText(s, "needs", asks, { ix: ix({ questions: [{ question: "用哪个\n框架？" } as never] }) })).toEqual({ title: "修登录 question", body: "用哪个 框架？" });
  });
  it("授权：正文是说明，没有说明时是命令", () => {
    expect(notificationText(s, "needs", asks, { ix: ix({ kind: "permission", tool_name: "Bash", tool_input: { command: "go test ./..." } }) }).body).toBe("go test ./...");
  });
  it("计划：正文是计划的第一行", () => {
    expect(notificationText(s, "needs", asks, { ix: ix({ kind: "plan", plan: "\n# 改同步脚本\n1. 先…" }) }).body).toBe("改同步脚本");
  });
  it("没有卡片时用宿主说的原因，再退回最后一句", () => {
    expect(notificationText(s, "needs", asks, { reason: "等你确认" })).toEqual({ title: "修登录 需要你", body: "等你确认" });
    expect(notificationText(s, "needs", asks, {}).body).toBe("我先看一下");
  });
  it("卡片什么都没写时退回最后一句；做完的通知不看卡片", () => {
    expect(notificationText(s, "needs", asks, { ix: ix({}) }).body).toBe("我先看一下");
    expect(notificationText(s, "finished", asks, { ix: ix({ questions: [{ question: "q" } as never] }) })).toEqual({ title: "修登录 做完了", body: "我先看一下" });
  });
  it("一个会话有几张卡时取最早的那张", () => {
    const cards = [ix({ id: "b", created_at: "2026-01-02" }), ix({ id: "x", session_id: "other", created_at: "2026-01-01" }), ix({ id: "a", created_at: "2026-01-01" })];
    expect(waitingCard(cards, s)?.id).toBe("a");
    expect(waitingCard(cards, { machine_id: "m", session_id: "none" })).toBeUndefined();
  });
});
