// The todo detail's stage bar (#2776): the six steps in order with the current one marked (aria-current="step"),
// nothing at all when the hub sends no stage or one this build doesn't know, and the PR link when it sends one.
import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { t } from "../i18n";
import { TODO_STAGES } from "../lib/todo-stage";
import { TodoStageBar } from "./TodoStageBar";

const html = (stage?: string, prURL?: string) => renderToStaticMarkup(<TodoStageBar stage={stage} prURL={prURL} />);

/** Each step's state, in the order the bar draws them. */
function states(markup: string): Record<string, string> {
  return Object.fromEntries([...markup.matchAll(/data-stage="([\w-]+)" data-state="([\w-]+)"/g)].map((m) => [m[1], m[2]]));
}

function currentSteps(markup: string): string[] {
  return [...markup.matchAll(/data-stage="([\w-]+)"[^>]*aria-current="step"/g)].map((m) => m[1]);
}

describe("TodoStageBar", () => {
  it("draws nothing without a stage", () => {
    expect(html()).toBe("");
    expect(html(undefined, "")).toBe("");
  });

  it("draws nothing for a stage it doesn't know", () => {
    expect(html("shipped")).toBe("");
    expect(html("PLAN")).toBe("");
  });

  it("marks the steps before the current one done, the current one, and the rest ahead", () => {
    TODO_STAGES.forEach((stage, current) => {
      const markup = html(stage);
      expect(Object.keys(states(markup))).toEqual([...TODO_STAGES]);
      TODO_STAGES.forEach((s, i) => {
        expect(states(markup)[s], `${stage} → ${s}`).toBe(i < current ? "done" : i === current ? "current" : "ahead");
      });
      expect(currentSteps(markup)).toEqual([stage]);
    });
  });

  it("is an ordered list of the six steps, in order and named", () => {
    const markup = html("review");
    expect(markup).toContain("<ol ");
    expect(markup).toContain('aria-label="' + t("todos.stage_label") + '"');
    expect(states(html("plan"))).toEqual({ plan: "current", develop: "ahead", review: "ahead", merge: "ahead", deploy: "ahead", verify: "ahead" });
    const labels = ["stage_plan", "stage_develop", "stage_review", "stage_merge", "stage_deploy", "stage_verify"].map((k) => t(`todos.${k}`));
    const at = labels.map((label) => markup.indexOf(label));
    expect(at.every((i) => i >= 0)).toBe(true);
    expect([...at].sort((a, b) => a - b)).toEqual(at);
  });

  it("links the PR when the todo has one, opening it like the app's other external links", () => {
    const markup = html("develop", "https://example.com/pull/7");
    expect(markup).toContain('href="https://example.com/pull/7"');
    expect(markup).toContain('target="_blank"');
    expect(markup).toContain('rel="noreferrer noopener"');
    expect(markup).toContain(t("todos.stage_pr_n", { n: "7" }));
    expect(markup).toContain('aria-label="' + t("todos.stage_pr_label", { pr: t("todos.stage_pr_n", { n: "7" }), host: "example.com" }) + '"');
  });

  it("names a PR address without a number just PR, and draws no link for anything but http(s)", () => {
    expect(html(undefined, "https://example.com/merge_requests")).toContain(">" + t("todos.stage_pr") + "<");
    expect(html(undefined, "javascript:alert(1)")).toBe("");
    expect(html(undefined, "not a url")).toBe("");
    expect(html("review", "data:text/html,x")).not.toContain("href=");
  });

  it("shows the PR link even when the stage is unknown, and only that", () => {
    const markup = html("shipped", "https://example.com/pull/7");
    expect(markup).not.toContain("<ol");
    expect(markup).toContain('href="https://example.com/pull/7"');
  });
});
