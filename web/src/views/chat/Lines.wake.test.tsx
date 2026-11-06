// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { t } from "../../i18n";
import { workSegment, type Turn } from "../../lib/turns";
import type { Item } from "../../lib/types";
import { ChatLineView } from "./Lines";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
} as unknown as typeof ResizeObserver;
window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;

const wake: Item = { id: "wake#0", seq: 1, role: "system", kind: "notice", text: "Background task done", wake: true };
const answer: Item = { id: "answer#0", seq: 2, role: "assistant", kind: "text", text: "Here are the results." };
const turn: Turn = { id: "turn", users: [], process: [answer], answer, trailing: [], wakes: [wake], afterWake: [wake, answer], ordered: [wake, answer] };
const seg = workSegment("segment", [answer]);
const expandFold = async () => {};
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

function render(live: boolean) {
  act(() => root.render(<ChatLineView line={{ kind: "segment", id: "line", seg, live, wakes: turn }} mid="machine" sid="session" expandFold={expandFold} />));
}

describe("background task drawer across turn status changes", () => {
  it.each([true, false])("stays open when live changes from %s", (live) => {
    render(live);
    const chip = host.querySelector<HTMLButtonElement>(`button[aria-label="${t("chat-lines.wake_open")}"]`)!;
    expect(chip).not.toBeNull();
    act(() => chip.click());
    const dialog = document.querySelector('[role="dialog"][aria-modal="true"]');
    expect(dialog).not.toBeNull();

    render(!live);

    expect(document.querySelector('[role="dialog"][aria-modal="true"]')).not.toBeNull();
    expect(document.querySelector('[role="dialog"]')).toBe(dialog);
    expect(host.querySelector(`button[aria-label="${t("chat-lines.wake_open")}"]`)).toBe(chip);
  });
});
