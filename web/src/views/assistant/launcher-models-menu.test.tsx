// @vitest-environment jsdom
// #5584: the Persona chat's model submenu is one choice of a set: exactly one row is checked, for the eye and for screen
// readers, even though the launcher's own default entry picks the same as "Default model".
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.hoisted(() => {
  window.matchMedia ??= ((query: string) => ({
    matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false,
  })) as typeof window.matchMedia;
});
vi.mock("../../lib/launcher-models", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../lib/launcher-models")>()),
  machineLaunchers: vi.fn(async () => ({ launchers: [{ name: "Claude", agent: "claude", model: "opus" }] })),
  launcherModels: vi.fn(async () => ({
    models: [
      { id: "default", name: "Default (recommended)", description: "Use the default model (currently opus)", default: true },
      { id: "haiku", name: "Haiku", description: "Fastest for quick answers" },
    ],
  })),
}));

import { AssistantLauncherChip } from "./AssistantChat";
import { pickModel } from "../../lib/assistant-chat";
import type { Session } from "../../lib/types";
import { t } from "../../i18n";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const session = { machine_id: "server", session_id: "s-models", assistant: true, agent: "claude" } as unknown as Session;
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
  pickModel("server/s-models", undefined);
});

async function openModels() {
  act(() => root.render(<AssistantLauncherChip session={session} />));
  await act(async () => host.querySelector<HTMLButtonElement>(`button[title="${t("assistant-assistantchat.launcher_chip_title")}"]`)!.click());
  const claude = [...document.querySelectorAll<HTMLButtonElement>('[role="menu"] button')].find((b) => b.textContent?.includes("Claude"))!;
  await act(async () => claude.click());
  return [...document.querySelectorAll<HTMLButtonElement>('[role="menu"] [role="menuitemradio"]')];
}

const checked = (rows: HTMLElement[]) => rows.filter((r) => r.getAttribute("aria-checked") === "true").map((r) => (r.querySelector("span.block") ?? r.querySelector("span"))!.textContent);

describe("Persona chat model submenu (#5584)", () => {
  it("checks only Default model when nothing is picked, not the launcher's default entry too", async () => {
    const rows = await openModels();
    expect(rows).toHaveLength(3);
    expect(checked(rows)).toEqual([t("assistant-assistantchat.default_model")]);
    expect(rows.filter((r) => r.querySelector("svg.text-ink"))).toHaveLength(1);
  });

  it("checks the picked model alone", async () => {
    const rows = await openModels();
    // picking a launcher resets its model, so pick after the submenu is open (as choosing the row does)
    act(() => pickModel("server/s-models", "haiku"));
    expect(checked(rows)).toEqual(["Haiku"]);
  });
});
