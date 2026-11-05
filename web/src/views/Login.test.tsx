// @vitest-environment jsdom
// The login page (#3356): the master-key form starts folded, each form signs in with its own field, and a rejected
// token says which kind of thing was wrong (a link vs. the hub's master key).
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const fake = vi.hoisted(() => ({ pair: vi.fn<(tok: string) => Promise<void>>() }));
vi.mock("../lib/auth", () => ({ pair: (tok: string) => fake.pair(tok) }));
vi.mock("../lib/hubs", () => ({ here: () => "", rememberHub: () => {} }));
vi.mock("./Hubs", () => ({ OtherHubs: () => null }));

import { t } from "../i18n";
import { Login } from "./Login";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  fake.pair.mockReset();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const input = (label: string) => host.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!;
const advancedToggle = () => host.querySelector<HTMLButtonElement>('button[aria-controls="login-advanced"]')!;
const alert = () => host.querySelector('[role="alert"]')!.textContent;

function type(el: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
  act(() => {
    setter.call(el, value);
    el.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

async function submit(el: HTMLInputElement) {
  await act(async () => {
    el.form!.requestSubmit();
  });
}

describe("Login", () => {
  it("starts with the master-key section folded", () => {
    act(() => root.render(<Login onDone={() => {}} />));
    expect(advancedToggle().getAttribute("aria-expanded")).toBe("false");
    act(() => advancedToggle().click());
    expect(advancedToggle().getAttribute("aria-expanded")).toBe("true");
  });

  it("signs in with the pasted link and reports a bad link as a bad link", async () => {
    const onDone = vi.fn();
    fake.pair.mockRejectedValueOnce(new Error("invalid token"));
    act(() => root.render(<Login onDone={onDone} />));
    type(input(t("login.link_placeholder")), "  https://hub.example/#token=jp_abc  ");
    await submit(input(t("login.link_placeholder")));
    expect(fake.pair).toHaveBeenCalledWith("https://hub.example/#token=jp_abc");
    expect(alert()).toBe(t("login.invalid_link"));
    expect(onDone).not.toHaveBeenCalled();

    fake.pair.mockResolvedValueOnce();
    await submit(input(t("login.link_placeholder")));
    expect(onDone).toHaveBeenCalledTimes(1);
  });

  it("signs in with the master key on its own field and reports its own error", async () => {
    fake.pair.mockRejectedValueOnce(new Error("invalid token"));
    act(() => root.render(<Login onDone={() => {}} />));
    type(input(t("login.link_placeholder")), "not-this-one");
    act(() => advancedToggle().click());
    type(input(t("login.master_placeholder")), "master-key");
    await submit(input(t("login.master_placeholder")));
    expect(fake.pair).toHaveBeenCalledWith("master-key");
    expect(alert()).toBe(t("login.invalid_token"));
  });
});
