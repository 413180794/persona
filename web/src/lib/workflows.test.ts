import { describe, expect, it } from "vitest";
import type { Session, Workflow } from "./types";
import { workflowShortLine, backgroundCount, backgroundRunning, otherBackground, workflowAgentFor, workflowAgents, workflowElapsed, workflowLine, workflowPhase, workflowStatus } from "./workflows";

const start = Date.parse("2026-11-05T13:46:57Z");
const run: Workflow = {
  task_id: "wdp5zxh37",
  run_id: "wf_c1d9daf8-cd2",
  name: "parity-audit",
  status: "running",
  running: true,
  phases: ["Audit", "Verify"],
  phase: "Verify",
  phase_index: 2,
  agents_started: 185,
  agents_done: 169,
  agents_running: 16,
  started_at: "2026-11-05T13:46:57Z",
  agents: [
    { id: "a1", session_id: "s1@a1", label: "audit:chat", phase: "Audit", running: false, last_text: "found 2 gaps" },
    { id: "a2", session_id: "s1@a2", label: "verify:chat#0", phase: "Verify", running: true },
    { id: "a3", session_id: "s1@a3", label: "verify:chat#1", phase: "Verify", running: false },
  ],
};
const session = (over: Partial<Session>): Session => ({ machine_id: "m", session_id: "s1", status: "done", state: "working", ...over }) as Session;

describe("workflows (#2935)", () => {
  it("sums up a running run in one line", () => {
    const s = session({ workflows: [run], background: [{ id: "wdp5zxh37", kind: "workflow", run_id: run.run_id }] });
    expect(workflowLine(s, start + 14 * 60_000)).toBe("后台 workflow「parity-audit」 · Verify 2/2 · 169/185 · 14 分");
    expect(workflowPhase(run)).toBe("Verify 2/2");
    expect(workflowShortLine(s)).toBe("169/185 · parity-audit · Verify");
  });

  it("counts a run once, not also as its background task", () => {
    const s = session({ workflows: [run], background: [{ id: "wdp5zxh37", kind: "workflow" }, { id: "b1", kind: "shell" }] });
    expect(otherBackground(s).map((b) => b.id)).toEqual(["b1"]);
    expect(backgroundCount(s)).toBe(2);
    expect(backgroundRunning(s)).toBe(true);
  });

  it("falls back to 后台运行中 on a hub without workflows", () => {
    const s = session({ background: [{ id: "w1", kind: "workflow" }] });
    expect(workflowLine(s, start)).toBe("后台运行中");
    expect(otherBackground(s).map((b) => b.id)).toEqual(["w1"]); // a task like the others there
    expect(backgroundCount(s)).toBe(1);
  });

  it("says nothing for ended runs, and reads their status", () => {
    const ended: Workflow = { ...run, running: false, status: "killed", ended_at: "2026-11-05T13:47:12Z" };
    const s = session({ workflows: [ended] });
    expect(workflowLine(s, start)).toBe("");
    expect(backgroundCount(s)).toBe(0);
    expect(workflowStatus(ended)).toBe("killed");
    expect(workflowStatus({ ...ended, status: "something-new" })).toBe("completed");
    expect(workflowElapsed(ended, start + 3_600_000)).toBe("15 秒");
  });

  it("names more than one running run", () => {
    const s = session({ workflows: [run, { ...run, task_id: "w2", name: "review" }] });
    expect(workflowLine(s, start + 60_000)).toContain("（共 2 个）");
  });

  it("lists running agents first, then the latest", () => {
    expect(workflowAgents(run).map((a) => a.id)).toEqual(["a2", "a3", "a1"]);
    expect(workflowAgentFor(session({ workflows: [run] }), "s1@a3")?.agent.label).toBe("verify:chat#1");
    expect(workflowAgentFor(session({ workflows: [run] }), "s1@zz")).toBeNull();
  });
});
