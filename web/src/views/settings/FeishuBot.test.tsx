// @vitest-environment jsdom
// The Feishu bot dialog keeps App ID / App Secret edits that are not saved yet: flipping a switch, pairing and the
// pairing poll only take the hub's other fields, and only "verify and save" puts the hub's credentials back in the form.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FeishuBotStatus } from "../../lib/feishu-bot";

type Call = { resolve: (s: FeishuBotStatus) => void };
const calls = vi.hoisted(() => ({ status: [] as Call[], update: [] as (Call & { body: unknown })[] }));
vi.mock("../../lib/feishu-bot", async (original) => ({
  ...(await original<typeof import("../../lib/feishu-bot")>()),
  botStatus: () => new Promise<FeishuBotStatus>((resolve) => calls.status.push({ resolve })),
  updateBot: (body: unknown) => new Promise<FeishuBotStatus>((resolve) => calls.update.push({ resolve, body })),
  pairBot: () => new Promise<FeishuBotStatus>(() => {}),
}));
vi.mock("../../ui/atoms", async (original) => ({ ...(await original<typeof import("../../ui/atoms")>()), toast: () => {} }));
import { t } from "../../i18n";
import { FeishuBot } from "./FeishuBot";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const saved = (over: Partial<FeishuBotStatus> = {}): FeishuBotStatus => ({
  enabled: true,
  app_id: "cli_saved",
  domain: "https://open.feishu.cn",
  has_secret: true,
  from_config: false,
  features: { main: true, cards: true, all_cards: false, pushes: false, sync: false, agents: false, reports: true, board: false },
  connected: true,
  owner: null,
  pairing: null,
  ...over,
});

let host: HTMLDivElement;
let root: Root;
const input = (label: string) => document.body.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!;
const type = (label: string, value: string) =>
  act(() => {
    const el = input(label);
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(el, value);
    el.dispatchEvent(new Event("input", { bubbles: true }));
  });
const switches = () => [...document.body.querySelectorAll<HTMLButtonElement>('[role="switch"]')];

// Draws the row, answers its status read, and opens the dialog.
async function open(status: FeishuBotStatus) {
  act(() => root.render(<FeishuBot />));
  await act(async () => calls.status[0].resolve(status));
  await act(async () => host.querySelector<HTMLButtonElement>("button")!.click());
}

beforeEach(() => {
  calls.status.length = 0;
  calls.update.length = 0;
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.useRealTimers();
});

describe("Feishu bot dialog", () => {
  it("keeps a typed App ID when a feature switch is flipped", async () => {
    await open(saved());
    type("App ID", "cli_typed");
    // The enable switch comes first, then one switch per feature.
    await act(async () => switches()[2].click());
    expect(calls.update).toHaveLength(1);
    expect(calls.update[0].body).toHaveProperty("features");
    await act(async () => calls.update[0].resolve(saved({ features: { ...saved().features, cards: false } })));
    expect(input("App ID").value).toBe("cli_typed");
  });

  it("keeps a typed App ID when the bot is switched off", async () => {
    await open(saved());
    type("App ID", "cli_typed");
    await act(async () => switches()[0].click());
    expect(calls.update[0].body).toEqual({ enabled: false });
    await act(async () => calls.update[0].resolve(saved({ enabled: false })));
    expect(input("App ID").value).toBe("cli_typed");
  });

  it("keeps a typed App Secret while the pairing poll answers", async () => {
    vi.useFakeTimers();
    await open(saved({ pairing: { code: "123456", expires_at: Math.floor(Date.now() / 1000) + 600 } }));
    type("App Secret", "typed-secret");
    await act(async () => void vi.advanceTimersByTime(3000));
    expect(calls.status).toHaveLength(2);
    await act(async () => calls.status[1].resolve(saved({ pairing: { code: "123456", expires_at: Math.floor(Date.now() / 1000) + 597 } })));
    expect(input("App Secret").value).toBe("typed-secret");
  });

  it("takes the hub's answer after verify and save", async () => {
    await open(saved());
    type("App ID", " cli_new ");
    type("App Secret", "new-secret");
    const save = [...document.body.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent === t("settings-feishubot.verify_save"));
    await act(async () => save!.click());
    expect(calls.update[0].body).toEqual({ app_id: "cli_new", domain: "feishu", app_secret: "new-secret" });
    await act(async () => calls.update[0].resolve(saved({ app_id: "cli_new" })));
    expect(input("App ID").value).toBe("cli_new");
    expect(input("App Secret").value).toBe("");
  });

  it("fills the form from a switch's answer when nothing was typed", async () => {
    await open(saved());
    await act(async () => switches()[0].click());
    await act(async () => calls.update[0].resolve(saved({ enabled: false, app_id: "cli_elsewhere" })));
    expect(input("App ID").value).toBe("cli_elsewhere");
  });
});
