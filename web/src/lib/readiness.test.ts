// What the main conversation still needs (docs/onboarding.md 4.4, 5.1, 5.2): which step the card shows, what the send
// button says, a refused start's card, a turn's model error that the user must fix.
import { describe, expect, it } from "vitest";
import { ApiError } from "./api";
import { parseAPIError } from "./api-errors";
import { providerName } from "./model-config";
import { blockingStep, codedFailure, helpTopic, knownAction, passesBySelf, providersPath, sendLabel, turnErrorFix, type Readiness, type ReadinessStep } from "./readiness";

const step = (id: string, state: string, o: Partial<ReadinessStep> = {}): ReadinessStep => ({ id, state, ...o });
const ready = (mode: string, steps: ReadinessStep[], next: string | null = null): Readiness => ({
  version: 1, mode, ready: mode === "full", next, machine: null, launcher: "claude", steps, dismissed: false, checked_at: "2026-11-06T09:00:00Z",
});

describe("blockingStep", () => {
  it("is the first missing step in setup", () => {
    const r = ready("setup", [step("computer", "todo", { code: "no_machine", action: { id: "add_computer" } }), step("claude", "unknown"), step("model", "unknown"), step("first_message", "unknown")], "computer");
    expect(blockingStep(r)?.code).toBe("no_machine");
  });

  it("skips ok, skipped, unknown and checking steps", () => {
    const r = ready("setup", [step("computer", "ok"), step("claude", "skipped"), step("model", "problem", { code: "not_logged_in" }), step("first_message", "todo")], "model");
    expect(blockingStep(r)?.id).toBe("model");
  });

  it("never shows in full, and a rate limit never blocks", () => {
    expect(blockingStep(ready("full", [step("computer", "ok"), step("claude", "ok"), step("model", "unknown", { code: "model_unverified" }), step("first_message", "todo")], "model"))).toBeNull();
    expect(blockingStep(ready("setup", [step("computer", "ok"), step("claude", "ok"), step("model", "problem", { code: "rate_limited" })], "model"))).toBeNull();
  });

  it("is never 试一句, and nothing on a hub without readiness", () => {
    expect(blockingStep(ready("setup", [step("computer", "ok"), step("claude", "ok"), step("model", "ok"), step("first_message", "problem", { code: "auth_failed" })], "first_message"))).toBeNull();
    expect(blockingStep(null)).toBeNull();
  });

  it("takes `next` for a step this page does not know", () => {
    const r = ready("setup", [step("computer", "ok"), step("claude", "ok"), step("model", "ok"), step("vpn", "brand_new", { title: "接上 VPN" })], "vpn");
    expect(blockingStep(r)?.title).toBe("接上 VPN");
  });
});

describe("sendLabel", () => {
  it("says what to do first", () => {
    expect(sendLabel(step("computer", "todo", { code: "no_machine" }))).toBe("先添加电脑");
    expect(sendLabel(step("computer", "problem", { code: "node_unreachable" }))).toBe("先唤醒电脑");
    expect(sendLabel(step("claude", "problem", { code: "claude_missing" }))).toBe("先接上模型");
    expect(sendLabel(step("model", "problem", { code: "auth_failed" }))).toBe("先接上模型");
  });
});

describe("codedFailure", () => {
  it("keeps the hub's words, the action and the original for 详情", () => {
    const e = new ApiError(409, "Persona 要在你的一台电脑上干活，现在还没接上电脑。", { code: "no_machine", error: "…", detail: "assistant machine is not configured", action: "add_computer", request_id: "r1" });
    expect(codedFailure(e)).toEqual({ code: "no_machine", error: e.message, detail: "assistant machine is not configured", action: "add_computer", machine: undefined, launcher: undefined, requestId: "r1" });
  });

  it("gives an offline computer its button and leaves other errors without a code", () => {
    expect(codedFailure(new ApiError(502, "小林的 MacBook 现在不在线", { code: "node_unreachable" })).action).toBe("wake_computer");
    expect(codedFailure(new ApiError(500, "boom", { code: "internal" }))).toMatchObject({ code: "", error: "boom", action: undefined });
    expect(codedFailure(new TypeError("Failed to fetch")).code).toBe("");
  });

  it("knows the restart that passes by itself", () => {
    expect(passesBySelf(new ApiError(503, "x", { code: "assistant_offline" }))).toBe(true);
    expect(passesBySelf(new ApiError(409, "x", { code: "no_machine" }))).toBe(false);
  });
});

describe("actions and help", () => {
  it("draws buttons only for known actions", () => {
    expect(knownAction("connect_model")).toBe(true);
    expect(knownAction("launch_rocket")).toBe(false);
    expect(knownAction(undefined)).toBe(false);
  });

  it("opens the provider wizard or one provider", () => {
    expect(providersPath()).toBe("/settings?tab=models&add=1");
    expect(providersPath("my key")).toBe("/settings?tab=models&launcher=my%20key");
  });

  it("opens help for a local question, never sending it", () => {
    expect(helpTopic("how_to_connect_model")).toBe("how_to_connect_model");
    expect(helpTopic("something_new")).toBe("what_can_you_do");
  });
});

describe("turn errors", () => {
  it("says what to fix for the user's codes only", () => {
    expect(turnErrorFix("not_logged_in")?.action).toBe("connect_model");
    expect(turnErrorFix("auth_failed")?.action).toBe("edit_provider");
    expect(turnErrorFix("model_route")?.action).toBe("connect_model");
    expect(turnErrorFix("rate_limited")).toBeNull();
    expect(turnErrorFix(undefined)).toBeNull();
  });

  it("reads Claude Code's own refusals as model errors (the hub's markers)", () => {
    // i18n-ignore: Claude Code's words
    expect(parseAPIError("Not logged in · Please run /login")).toMatchObject({ code: "not_logged_in", transient: false });
    expect(parseAPIError("Invalid API key · Fix external API key")).toMatchObject({ code: "auth_failed", transient: false });
    expect(parseAPIError("There's an issue with the selected model (x). It may not exist")?.code).toBe("model_route");
    expect(parseAPIError("API Error: 401 Failed to authenticate")?.code).toBe("auth_failed");
    // a reply that talks about being logged out is a reply
    expect(parseAPIError("Not logged in to GitHub, so I can't push")).toBeNull();
  });
});

describe("providerName", () => {
  it("hides the hub's old provider ids", () => {
    expect(providerName("legacy.kb", [])).toBeUndefined();
    expect(providerName("legacy.kb", [{ id: "legacy.kb", name: "hub 知识 API" } as never])).toBe("hub 知识 API");
    expect(providerName("mine", [])).toBe("mine");
  });
});
