// The service worker's notificationclick (#2971): an app window it controls comes forward on the conversation, a
// focused one first; with none, or one that can't be navigated, a new window opens.
import { describe, expect, it, vi } from "vitest";
import { openFromNotification, type ClickWindow } from "./sw-click";

const win = (o: Partial<ClickWindow> & { name: string }, navigates = true) => {
  const w = {
    ...o,
    focus: vi.fn(async () => w),
    navigate: vi.fn(async () => (navigates ? w : null)),
  };
  return w;
};

const clients = (wins: ClickWindow[]) => ({
  matchAll: vi.fn(async () => wins),
  openWindow: vi.fn(async () => null),
});

describe("点系统通知", () => {
  it("只看这个 worker 管着的窗口，挑有焦点的那个，转到对话", async () => {
    const back = win({ name: "back", visibilityState: "hidden" });
    const front = win({ name: "front", focused: true, visibilityState: "visible" });
    const c = clients([back, front]);
    await openFromNotification(c, "/m/s");
    expect(c.matchAll).toHaveBeenCalledWith({ type: "window", includeUncontrolled: false });
    expect(front.focus).toHaveBeenCalled();
    expect(front.navigate).toHaveBeenCalledWith("/m/s");
    expect(back.navigate).not.toHaveBeenCalled();
    expect(c.openWindow).not.toHaveBeenCalled();
  });

  it("没有窗口就开一个新的", async () => {
    const c = clients([]);
    await openFromNotification(c, "/m/s");
    expect(c.openWindow).toHaveBeenCalledWith("/m/s");
  });

  it("窗口转不过去（返回空或报错）就开一个新的", async () => {
    const stuck = win({ name: "stuck" }, false);
    const c = clients([stuck]);
    await openFromNotification(c, "/m/s");
    expect(c.openWindow).toHaveBeenCalledWith("/m/s");

    const broken = win({ name: "broken" });
    broken.navigate.mockRejectedValueOnce(new TypeError("not controlled"));
    const c2 = clients([broken]);
    await openFromNotification(c2, "/m/s");
    expect(c2.openWindow).toHaveBeenCalledWith("/m/s");
  });
});
