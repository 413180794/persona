// @vitest-environment jsdom
// The provider wizard's fields tell a screen reader what is wrong: a TextField with an error is aria-invalid and
// described by the error line, one with only a hint is described by the hint, and a SecretField's hint is linked too.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { SecretField, TextField } from "./ProviderFields";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const render = (node: React.ReactNode) => act(() => root.render(node));
/** The text of the element the field's aria-describedby points at, or null when it points nowhere. */
const description = (field: Element) => {
  const id = field.getAttribute("aria-describedby");
  return id ? document.getElementById(id)?.textContent ?? null : null;
};
const noop = () => {};

describe("TextField", () => {
  it("marks an invalid value and points at the error, which wins over the hint", () => {
    render(<TextField label="名字" value="a b" onChange={noop} hint="只用字母和数字" error="名字里不能有空格" />);
    const input = host.querySelector("input")!;
    expect(input.getAttribute("aria-invalid")).toBe("true");
    expect(description(input)).toBe("名字里不能有空格");
  });

  it("is described by its hint and not marked invalid when there is no error", () => {
    render(<TextField label="名字" value="ok" onChange={noop} hint="只用字母和数字" />);
    const input = host.querySelector("input")!;
    expect(input.hasAttribute("aria-invalid")).toBe(false);
    expect(description(input)).toBe("只用字母和数字");
  });

  it("has no aria-describedby when there is nothing to describe it", () => {
    render(<TextField label="名字" value="ok" onChange={noop} />);
    const input = host.querySelector("input")!;
    expect(input.hasAttribute("aria-describedby")).toBe(false);
    expect(input.hasAttribute("aria-invalid")).toBe(false);
  });
});

describe("SecretField", () => {
  it("links its hint to the masked input", () => {
    render(<SecretField label="API key" value="" onChange={noop} hint="只存在这台 hub 上" />);
    expect(description(host.querySelector("input")!)).toBe("只存在这台 hub 上");
  });

  it("links its hint to the multiline headers", () => {
    render(<SecretField label="请求头" value="" onChange={noop} hint="一行一个" multiline />);
    expect(description(host.querySelector("textarea")!)).toBe("一行一个");
  });

  it("has no aria-describedby without a hint", () => {
    render(<SecretField label="API key" value="" onChange={noop} />);
    expect(host.querySelector("input")!.hasAttribute("aria-describedby")).toBe(false);
  });
});
