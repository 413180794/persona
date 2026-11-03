import { describe, expect, it, vi } from "vitest";
import { openTodoFromTag } from "../lib/todos";

describe("openTodoFromTag (#881 review)", () => {
  it("调用 navigate 导航到待办详情并触发 onNavigate 关闭抽屉", () => {
    const nav = vi.fn();
    const onNavigate = vi.fn();

    openTodoFromTag("todo-weather", nav, onNavigate);

    expect(nav).toHaveBeenCalledWith({ name: "todos", id: "todo-weather" });
    expect(onNavigate).toHaveBeenCalledTimes(1);
  });

  it("当未提供 onNavigate 时依然正常调用 navigate", () => {
    const nav = vi.fn();

    openTodoFromTag("todo-weather", nav);

    expect(nav).toHaveBeenCalledWith({ name: "todos", id: "todo-weather" });
  });
});
