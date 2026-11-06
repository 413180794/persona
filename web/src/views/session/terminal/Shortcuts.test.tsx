// @vitest-environment jsdom
// The terminal shortcuts dialog's "New" field has a name a screen reader reads, not only a placeholder.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { t } from "../../../i18n";
import { ShortcutsEditor } from "./Shortcuts";

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

describe("ShortcutsEditor", () => {
  it("names the new-shortcut field", () => {
    act(() => root.render(<ShortcutsEditor open onClose={() => {}} />));
    const input = document.querySelector<HTMLInputElement>("form input");
    expect(input?.getAttribute("aria-label")).toBe(t("session-terminal-shortcuts.new_placeholder"));
  });
});
