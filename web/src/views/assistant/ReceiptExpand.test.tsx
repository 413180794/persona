// @vitest-environment jsdom
// A task receipt's summary cut at four lines opens the rest in place (#4715), so the owner reads the result on the card.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { ReceiptSummary } from "./Tasks";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let tall = 0;
const desc = { scroll: Object.getOwnPropertyDescriptor(HTMLElement.prototype, "scrollHeight"), client: Object.getOwnPropertyDescriptor(HTMLElement.prototype, "clientHeight") };

beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, "scrollHeight", { configurable: true, get: () => tall });
  Object.defineProperty(HTMLElement.prototype, "clientHeight", { configurable: true, get: () => 80 });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
  if (desc.scroll) Object.defineProperty(HTMLElement.prototype, "scrollHeight", desc.scroll);
  if (desc.client) Object.defineProperty(HTMLElement.prototype, "clientHeight", desc.client);
});

const text = "第一行\n\n第二行\n\n第三行\n\n第四行\n\n**请验收**：登录页的改动";

describe("ReceiptSummary expand (#4715)", () => {
  it("offers 展开全文 when the four lines cut it, and opens it in place", () => {
    tall = 200;
    act(() => root.render(<ReceiptSummary text={text} />));
    const clamp = host.querySelector("[data-clamped]");
    expect(clamp?.className).toContain("line-clamp-4");
    const button = host.querySelector("button")!;
    expect(button.textContent).toBe("展开全文");
    expect(button.getAttribute("aria-expanded")).toBe("false");
    act(() => button.click());
    expect(host.querySelector("[data-clamped]")).toBeNull();
    expect(host.innerHTML).not.toContain("line-clamp-4");
    expect(host.textContent).toContain("请验收");
    expect(button.textContent).toBe("收起");
    expect(button.getAttribute("aria-expanded")).toBe("true");
    act(() => button.click());
    expect(host.querySelector("[data-clamped]")?.className).toContain("line-clamp-4");
  });

  it("has no button when the text fits", () => {
    tall = 80;
    act(() => root.render(<ReceiptSummary text="改好了" />));
    expect(host.querySelector("button")).toBeNull();
  });

  it("does the same for a plain start error", () => {
    tall = 200;
    act(() => root.render(<ReceiptSummary text={"没开起来：" + "很长的原因 ".repeat(40)} plain />));
    expect(host.querySelector("button")?.textContent).toBe("展开全文");
  });
});
