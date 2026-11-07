// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Run, Task } from "../../lib/routines";
import { Routines } from "./Routines";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
});

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const task: Task = { id: "task-1", title: "Daily summary", prompt: "Summarize the day", repeat: "daily", time: "09:00", enabled: true };
const run: Run = { id: "run-1", kind: "task", ref_id: task.id, title: "Completed summary", due: "2026-11-05T09:00:00Z", at: "2026-11-05T09:00:00Z", status: "ok" };
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const failure = () => json({ error: "run history unavailable" }, 500);
// errorText explains a 5xx and keeps the hub's own text after it (#3728)
const failedText = /读不到运行记录：.*run history unavailable/;

let runsReply: () => Response | Promise<Response>;
let tasksReply: () => Response;
let calls: string[];
let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  calls = [];
  runsReply = failure;
  tasksReply = () => json({ tasks: [task] });
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => {
    const path = String(input);
    calls.push(path);
    if (path === "/api/v1/assistant/tasks") return Promise.resolve(tasksReply());
    if (path === "/api/v1/assistant/runs") return Promise.resolve(runsReply());
    throw new Error(`Unexpected request: ${path}`);
  }));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const render = () => act(async () => root.render(<Routines onToggleSidebar={() => {}} />));
const history = () => host.querySelector("aside")!;
const retry = () => [...history().querySelectorAll("button")].find((b) => b.textContent === "重试");
const refresh = () => act(async () => host.querySelector<HTMLButtonElement>('button[aria-label="刷新"]')!.click());

describe("routine run history loading", () => {
  it("uses green success marks for completed runs without coloring failed-run surfaces", async () => {
    runsReply = () => json({ runs: [run, { ...run, id: "failed-run", title: "Failed summary", status: "failed" }] });
    await render();
    const marks = [...history().querySelectorAll('[role="img"]')];
    expect(marks.some((mark) => mark.classList.contains("bg-green-tint") && mark.classList.contains("text-green"))).toBe(true);
    expect(marks.some((mark) => mark.classList.contains("bg-field") && mark.classList.contains("text-red"))).toBe(true);
  });

  it("shows a failed request's reason and retry while keeping the tasks visible", async () => {
    await render();

    expect(host.textContent).toContain(task.title);
    expect(history().textContent).toMatch(failedText);
    expect(history().textContent).not.toContain("加载中");
    expect(history().textContent).not.toContain("还没有运行过");
    expect(retry()).toBeDefined();
  });

  it("shows successful history independently of a failed task request", async () => {
    tasksReply = () => json({ error: "tasks unavailable" }, 500);
    runsReply = () => json({ runs: [run] });
    await render();

    expect(host.textContent).toContain("读不到定时任务");
    expect(host.textContent).toContain("tasks unavailable");
    expect(history().textContent).toContain(run.title);
    expect(history().textContent).not.toContain("读不到运行记录");
    expect(retry()).toBeUndefined();
  });

  it.each([
    { name: "nonempty", runs: [run], expected: run.title },
    { name: "empty", runs: [] as Run[], expected: "还没有运行过" },
  ])("retries both requests and recovers to $name history", async ({ runs, expected }) => {
    await render();
    expect(retry()).toBeDefined();
    runsReply = () => json({ runs });

    await act(async () => retry()!.click());

    expect(calls.filter((path) => path === "/api/v1/assistant/tasks")).toHaveLength(2);
    expect(calls.filter((path) => path === "/api/v1/assistant/runs")).toHaveLength(2);
    expect(host.textContent).toContain(task.title);
    expect(history().textContent).toContain(expected);
    expect(history().textContent).not.toMatch(failedText);
    expect(history().textContent).not.toContain("加载中");
    expect(retry()).toBeUndefined();
  });

  it("shows a refresh failure after earlier success and clears it when refreshed successfully", async () => {
    runsReply = () => json({ runs: [run] });
    await render();
    expect(history().textContent).toContain(run.title);

    runsReply = failure;
    await refresh();

    expect(host.textContent).toContain(task.title);
    expect(history().textContent).toMatch(failedText);
    expect(history().textContent).not.toContain(run.title);
    expect(history().textContent).not.toContain("加载中");
    expect(retry()).toBeDefined();

    const recovered = { ...run, id: "run-2", title: "Latest summary" };
    runsReply = () => json({ runs: [recovered] });
    await refresh();

    expect(history().textContent).toContain(recovered.title);
    expect(history().textContent).not.toMatch(failedText);
    expect(retry()).toBeUndefined();
  });

  it("ignores an older request's failure after a newer refresh succeeded", async () => {
    let finish!: (response: Response) => void;
    const pending = new Promise<Response>((resolve) => { finish = resolve; });
    runsReply = () => pending;
    await render();
    expect(history().textContent).toContain("加载中");

    runsReply = () => json({ runs: [run] });
    await refresh();
    expect(history().textContent).toContain(run.title);

    await act(async () => finish(failure()));

    expect(history().textContent).toContain(run.title);
    expect(history().textContent).not.toMatch(failedText);
    expect(retry()).toBeUndefined();
  });
});
