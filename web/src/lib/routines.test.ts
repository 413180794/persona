import { describe, expect, it } from "vitest";
import {
  blankTask, clockLabel, dayLabel, everyLabel, fromLocalInput, fromTemplate, lastRunText, momentLabel, nextHour, nextText, normalizeDays,
  parseTime, repeatsLabel, ruleText, runDays, runDetail, runsWith, sameTask, splitTasks, taskBody, taskTitle, TEMPLATES, toLocalInput, untilLabel,
  upcoming, validateTask, withRepeat, type Run, type Task,
} from "./routines";

// Local times throughout, so the tests pass in any time zone. Mon 2 Nov 2026, 10:00.
const now = new Date(2026, 10, 2, 10, 0);
const at = (d: number, h: number, m = 0) => new Date(2026, 10, d, h, m);
const iso = (d: Date) => d.toISOString();
const task = (p: Partial<Task>): Task => ({ ...blankTask(), id: "t1", title: "早报", prompt: "给我早报", ...p });

describe("words", () => {
  it("says which days", () => {
    expect(repeatsLabel([])).toBe("每天");
    expect(repeatsLabel([1, 2, 3, 4, 5, 6, 7])).toBe("每天");
    expect(repeatsLabel([5, 1, 2, 3, 4])).toBe("工作日");
    expect(repeatsLabel([7, 6])).toBe("周末");
    expect(repeatsLabel([1, 3, 5, 3])).toBe("每周一、三、五");
    expect(normalizeDays([3, 1, 9, 1])).toEqual([1, 3]);
  });

  it("says a clock time as Reminders does", () => {
    expect(clockLabel("09:00")).toBe("上午 9:00");
    expect(clockLabel("08:45")).toBe("早上 8:45");
    expect(clockLabel("12:30")).toBe("中午 12:30");
    expect(clockLabel("17:00")).toBe("下午 5:00");
    expect(clockLabel("21:30")).toBe("晚上 9:30");
    expect(clockLabel("00:15")).toBe("凌晨 12:15");
    expect(clockLabel("9:00")).toBe("9:00"); // not HH:MM: left as it is
  });

  it("says an interval", () => {
    expect(everyLabel(30)).toBe("每 30 分钟");
    expect(everyLabel(120)).toBe("每 2 小时");
    expect(everyLabel(90)).toBe("每 1 小时 30 分钟");
    expect(everyLabel(1440)).toBe("每天");
    expect(everyLabel(2880)).toBe("每 2 天");
  });

  it("says the day and how long until", () => {
    expect(dayLabel(at(2, 18), now)).toBe("今天");
    expect(dayLabel(at(3, 9), now)).toBe("明天");
    expect(dayLabel(at(1, 9), now)).toBe("昨天");
    expect(dayLabel(at(5, 9), now)).toBe("周四");
    expect(dayLabel(at(20, 9), now)).toBe("11月20日");
    expect(momentLabel(at(3, 9, 5), now)).toBe("明天 09:05");
    expect(untilLabel(new Date(now.getTime() + 20_000), now)).toBe("马上");
    expect(untilLabel(at(2, 10, 25), now)).toBe("25 分钟后");
    expect(untilLabel(at(2, 11, 30), now)).toBe("1 小时 30 分钟后");
    expect(untilLabel(at(2, 17), now)).toBe("7 小时后");
    expect(untilLabel(at(5, 9), now)).toBe("3 天后");
    expect(untilLabel(at(2, 9), now)).toBe("已过时间");
  });

  it("says the rule", () => {
    expect(ruleText(task({ repeat: "daily", time: "09:00", weekdays: [1, 2, 3, 4, 5] }))).toBe("每个工作日 上午 9:00");
    expect(ruleText(task({ repeat: "daily", time: "22:00", weekdays: [] }))).toBe("每天 晚上 10:00");
    expect(ruleText(task({ repeat: "daily", time: "17:00", weekdays: [5] }))).toBe("每周五 下午 5:00");
    expect(ruleText(task({ repeat: "interval", every_minutes: 30 }))).toBe("每 30 分钟");
    expect(ruleText(task({ repeat: "once", session_id: "s1" }))).toBe("一次 · 接着原来的对话");
    expect(ruleText(task({ repeat: "once" }))).toBe("一次 · 新对话");
  });

  it("titles a follow-up without one by its prompt", () => {
    expect(taskTitle({ title: "", prompt: "\n周五发周报前核对数据\n第二行" })).toBe("周五发周报前核对数据");
    expect(taskTitle({ title: "  周报 ", prompt: "x" })).toBe("周报");
  });

  it("says the next and the last run", () => {
    expect(nextText(task({ enabled: false, next_run: iso(at(3, 9)) }), now)).toBe("已关闭");
    expect(nextText(task({ next_run: iso(at(3, 9)) }), now)).toBe("下次 明天 09:00 · 23 小时后");
    expect(nextText(task({ repeat: "interval", at: iso(at(2, 10, 12)) }), now)).toBe("下次 今天 10:12 · 12 分钟后");
    expect(lastRunText(task({}), now)).toBeNull();
    expect(lastRunText(task({ last_run: "0001-01-01T00:00:00Z" }), now)).toBeNull(); // Go's zero time
    expect(lastRunText(task({ last_run: iso(at(2, 9, 3)), last_status: "ok" }), now)).toEqual({ text: "上次 今天 09:03 · 已运行", failed: false });
    expect(lastRunText(task({ last_run: iso(at(30, 17)), last_status: "failed" }), at(31, 12))).toEqual({ text: "上次 昨天 17:00 · 失败", failed: true });
  });

  it("says what it runs with when not the default", () => {
    expect(runsWith({})).toBeNull();
    expect(runsWith({ permission_mode: "default" })).toBeNull();
    expect(runsWith({ launcher: "claude-fast", permission_mode: "auto", effort: "low" })).toEqual({ text: "claude-fast · 自动 · 思考低", danger: false });
    expect(runsWith({ permission_mode: "bypassPermissions" })).toEqual({ text: "全部放行", danger: true });
  });
});

describe("the editor", () => {
  it("checks a task before saving", () => {
    expect(validateTask(task({ title: " " }), now)).toBe("要有标题");
    expect(validateTask(task({ repeat: "once", title: "", at: iso(at(2, 15)) }), now)).toBeNull(); // a one-off needs no title
    expect(validateTask(task({ prompt: "  " }), now)).toBe("提示词不能为空");
    expect(validateTask(task({ time: "9:00" }), now)).toBe("时间要写成 HH:MM，比如 09:00");
    expect(validateTask(task({ time: "24:00" }), now)).toBe("时间要写成 HH:MM，比如 09:00");
    expect(validateTask(task({ time: "" }), now)).toBe("时间要写成 HH:MM，比如 09:00");
    expect(validateTask(task({ time: "23:59" }), now)).toBeNull();
    expect(validateTask(task({ repeat: "interval", every_minutes: 4 }), now)).toBe("间隔要在 5 分钟到 7 天之间");
    expect(validateTask(task({ repeat: "interval", every_minutes: 7 * 1440 + 1 }), now)).toBe("间隔要在 5 分钟到 7 天之间");
    expect(validateTask(task({ repeat: "interval", every_minutes: 7.5 }), now)).toBe("间隔要在 5 分钟到 7 天之间");
    expect(validateTask(task({ repeat: "interval", every_minutes: 5 }), now)).toBeNull();
    expect(validateTask(task({ repeat: "interval", every_minutes: 7 * 1440 }), now)).toBeNull();
    expect(validateTask(task({ repeat: "once", at: undefined }), now)).toBe("要选一个运行时间");
    expect(validateTask(task({ repeat: "once", at: iso(at(2, 9)) }), now)).toBe("这个时间已经过了，选一个以后的时间");
    expect(validateTask(task({ repeat: "once", at: iso(at(2, 9)), enabled: false }), now)).toBeNull();
  });

  it("sees what changed", () => {
    const t = task({ repeat: "daily", time: "09:00", weekdays: [1, 2, 3, 4, 5], next_run: iso(at(3, 9)) });
    expect(sameTask(t, { ...t, next_run: undefined, last_status: "failed" })).toBe(true); // the hub's own fields
    expect(sameTask(t, { ...t, weekdays: [5, 4, 3, 2, 1] })).toBe(true);
    expect(sameTask(t, { ...t, every_minutes: 45 })).toBe(true); // another rule's field
    expect(sameTask(t, { ...t, time: "09:30" })).toBe(false);
    expect(sameTask(t, { ...t, enabled: false })).toBe(false);
    expect(sameTask(t, { ...t, permission_mode: "auto" })).toBe(false);
    expect(sameTask(t, { ...t, title: "早报 " })).toBe(true);
  });

  it("saves every field the hub replaces, and the model ones only when changed", () => {
    const saved = task({ repeat: "daily", time: "09:00", weekdays: [1, 2, 3, 4, 5, 6, 7], launcher: "claude", effort: "low", session_id: "s1", at: "0001-01-01T00:00:00Z" });
    const body = taskBody({ ...saved, title: " 早报 ", permission_mode: "auto" }, saved);
    expect(body).toEqual({ title: "早报", prompt: "给我早报", repeat: "daily", enabled: true, time: "09:00", weekdays: [], every_minutes: 0, session_id: "s1", permission_mode: "auto" });
    expect(taskBody({ ...saved, launcher: "", effort: "high" }, saved)).toMatchObject({ launcher: "", effort: "high" });
    expect(taskBody({ ...saved, session_id: undefined }, saved).session_id).toBe(""); // runs go to the task's own conversation
    const once = taskBody(task({ repeat: "once", at: iso(at(2, 15)) }));
    expect(once).toMatchObject({ repeat: "once", at: iso(at(2, 15)), launcher: "", effort: "", time: "", weekdays: [] });
    expect(taskBody(task({ repeat: "interval", every_minutes: 45 }))).toMatchObject({ every_minutes: 45, time: "" });
    expect("at" in taskBody(task({ repeat: "interval", every_minutes: 45 }))).toBe(false);
  });

  it("keeps the editor valid when the rule changes", () => {
    const d = withRepeat(task({ repeat: "daily" }), "once", now);
    expect(parseTime(d.at)).toEqual(at(2, 11));
    expect(withRepeat(task({ every_minutes: 0 }), "interval", now).every_minutes).toBe(30);
    expect(withRepeat(task({ repeat: "interval", time: "" }), "daily", now).time).toBe("09:00");
    const future = iso(at(4, 8));
    expect(withRepeat(task({ at: future }), "once", now).at).toBe(future);
    expect(nextHour(at(2, 23, 30))).toEqual(at(3, 0, 0));
  });

  it("reads and writes a datetime-local value in local time", () => {
    expect(toLocalInput(at(2, 9, 5))).toBe("2026-11-02T09:05");
    expect(fromLocalInput("2026-11-02T09:05")).toEqual(at(2, 9, 5));
    expect(fromLocalInput("")).toBeNull();
    expect(toLocalInput(null)).toBe("");
  });

  it("starts from a template", () => {
    const t = fromTemplate(TEMPLATES.find((x) => x.key === "patrol")!);
    expect(t).toMatchObject({ id: "", title: "Agent 巡检", repeat: "interval", every_minutes: 30, enabled: true });
    expect(validateTask(t, now)).toBeNull();
    for (const tpl of TEMPLATES) expect(validateTask(fromTemplate(tpl), now)).toBeNull();
    expect(validateTask(blankTask(), now)).toBe("要有标题");
  });
});

describe("lists", () => {
  it("splits repeating and one-off tasks, one-offs soonest first", () => {
    const list = [
      task({ id: "a", repeat: "once", at: iso(at(3, 9)), next_run: iso(at(3, 9)) }),
      task({ id: "b", repeat: "daily" }),
      task({ id: "c", repeat: "once", at: iso(at(2, 15)), next_run: iso(at(2, 15)) }),
      task({ id: "d", repeat: "interval", every_minutes: 30 }),
    ];
    const { repeating, oneOff } = splitTasks(list);
    expect(repeating.map((t) => t.id)).toEqual(["b", "d"]);
    expect(oneOff.map((t) => t.id)).toEqual(["c", "a"]);
  });

  it("lists what comes up next, without what is off", () => {
    const list = [
      task({ id: "a", next_run: iso(at(3, 9)) }),
      task({ id: "b", enabled: false, next_run: iso(at(2, 11)) }),
      task({ id: "c", repeat: "once", title: "", prompt: "回电话", at: iso(at(2, 12)), next_run: iso(at(2, 12)) }),
    ];
    expect(upcoming(list).map((u) => [u.id, u.title, u.detail])).toEqual([
      ["c", "回电话", "跟进 · 新对话"],
      ["a", "早报", "每个工作日 上午 9:00"],
    ]);
  });

  it("says how a run went and groups runs by day", () => {
    const r = (p: Partial<Run>): Run => ({ id: "r", kind: "routine", ref_id: "t1", title: "早报", due: iso(at(2, 9)), at: iso(at(2, 9)), status: "ok", ...p });
    expect(runDetail(r({}))).toBe("准时");
    expect(runDetail(r({ at: iso(new Date(at(2, 9).getTime() + 21_000)) }))).toBe("晚了 21 秒");
    expect(runDetail(r({ at: iso(at(2, 9, 3)) }))).toBe("晚了 3 分钟");
    expect(runDetail(r({ at: iso(at(2, 11)) }))).toBe("晚了 2 小时");
    expect(runDetail(r({ manual: true }))).toBe("手动运行");
    expect(runDetail(r({ status: "failed", error: "assistant machine is offline" }))).toBe("assistant machine is offline");
    expect(runDetail(r({ status: "skipped" }))).toBe("已跳过");
    const days = runDays([r({ id: "1", at: iso(at(1, 22)) }), r({ id: "2", at: iso(at(2, 9)) }), r({ id: "3", at: iso(at(1, 9)) }), r({ id: "4", at: iso(new Date(2026, 9, 20, 9)) })], now);
    expect(days.map((d) => [d.day, d.runs.map((x) => x.id)])).toEqual([["今天", ["2"]], ["昨天", ["1", "3"]], ["10月20日", ["4"]]]);
  });
});
