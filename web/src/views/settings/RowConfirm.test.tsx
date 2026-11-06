// @vitest-environment jsdom
// RowConfirm (#4768): opening the in-row confirm moves the focus to "Cancel" instead of dropping it to <body> with the
// unmounted trigger, closing hands it back to the trigger, and Escape cancels.
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { t } from "../../i18n";
import { RowConfirm } from "./SettingsParts";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
const button = (text: string) => [...host.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent === text)!;
const click = (el: HTMLElement) => act(() => (el.focus(), el.click()));
const escape = () => act(() => void document.activeElement!.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true })));

// The ShortcutSettings shape: several controls in the trigger, the one that opens it not the first
function Harness({ onCancel, extra = false }: { onCancel?: () => void; extra?: boolean }) {
  const [open, setOpen] = useState(false);
  return (
    <div>
      <RowConfirm
        open={open}
        confirm="Revoke now"
        onCancel={() => (onCancel?.(), setOpen(false))}
        onConfirm={() => setOpen(false)}
        trigger={
          <>
            {extra && <button type="button">Reset</button>}
            <button type="button" onClick={() => setOpen(true)}>
              Revoke
            </button>
            {extra && <button type="button">Toggle</button>}
          </>
        }
      />
      <button type="button" onClick={() => setOpen(false)}>
        Elsewhere
      </button>
    </div>
  );
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

describe("RowConfirm focus", () => {
  it("moves the focus to 取消 when it opens and back to the trigger when 取消 closes it", () => {
    act(() => root.render(<Harness />));
    click(button("Revoke"));
    expect(document.activeElement).toBe(button(t("common.cancel")));
    click(button(t("common.cancel")));
    expect(document.activeElement).toBe(button("Revoke"));
  });

  it("returns the focus to the trigger control that opened it, not the first one", () => {
    act(() => root.render(<Harness extra />));
    click(button("Revoke"));
    expect(document.activeElement).toBe(button(t("common.cancel")));
    click(button(t("common.cancel")));
    expect(document.activeElement).toBe(button("Revoke"));
  });

  it("closes on Escape through onCancel, the focus back on the trigger", () => {
    const onCancel = vi.fn();
    act(() => root.render(<Harness onCancel={onCancel} />));
    click(button("Revoke"));
    escape();
    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(document.activeElement).toBe(button("Revoke"));
  });

  it("leaves the focus alone when it was moved out of the row before closing", () => {
    act(() => root.render(<Harness />));
    click(button("Revoke"));
    click(button("Elsewhere"));
    expect(document.activeElement).toBe(button("Elsewhere"));
  });
});
