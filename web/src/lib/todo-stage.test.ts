// The stage bar's math (#2776): which of the six steps the hub's `stage` means, and how each step reads against it.
import { describe, expect, it } from "vitest";
import { TODO_STAGES, todoStageIndex, todoStageState } from "./todo-stage";

describe("todoStageIndex", () => {
  it("numbers the six stages in order", () => {
    TODO_STAGES.forEach((stage, i) => expect(todoStageIndex(stage)).toBe(i));
  });

  it("is -1 for nothing and for a stage this build doesn't know", () => {
    expect(todoStageIndex(undefined)).toBe(-1);
    expect(todoStageIndex(null)).toBe(-1);
    expect(todoStageIndex("")).toBe(-1);
    expect(todoStageIndex("shipped")).toBe(-1);
    expect(todoStageIndex("Plan")).toBe(-1);
  });
});

describe("todoStageState", () => {
  it("is done before the current step, current on it, ahead after it", () => {
    expect(todoStageState(0, 2)).toBe("done");
    expect(todoStageState(2, 2)).toBe("current");
    expect(todoStageState(3, 2)).toBe("ahead");
  });
});
