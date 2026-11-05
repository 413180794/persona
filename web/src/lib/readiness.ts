// What the main conversation still needs (docs/onboarding.md 4.4, 5.1; docs/protocol.md「主对话就绪度」): the hub works
// it out (GET /api/v1/assistant/readiness) and every client draws the same thing from it: in `setup` a card above the
// composer with the first missing step, the send button saying what to do first, and example questions that open local
// help. A coded refusal of a start or a send (5.2) becomes a card too, not a toast. On a hub without the feature
// (`assistant_readiness`) nothing here shows and the page behaves as before.
import { useEffect, useSyncExternalStore } from "react";
import { t } from "../i18n";
import { api, ApiError, errorText } from "./api";
import { navigate } from "./router";
import { hubHas, useStore } from "./store";

export const READINESS_FEATURE = "assistant_readiness";
export const READINESS_PATH = "/assistant/readiness";

export type ReadinessMode = "full" | "setup" | "chat_only";
export type StepState = "ok" | "todo" | "problem" | "checking" | "unknown" | "skipped";
export type StepId = "computer" | "claude" | "model" | "first_message";

export interface ReadinessAction {
  id: string;
}

export interface ReadinessStep {
  id: StepId | string;
  state: StepState | string;
  code?: string;
  machine?: string;
  launcher?: string;
  version?: string;
  title?: string;
  detail?: string;
  action?: ReadinessAction;
  alt_actions?: ReadinessAction[];
}

export interface ReadinessSuggestion {
  id: string;
  title: string;
  prompt?: string;
  local?: boolean;
  needs?: string[];
}

export interface Readiness {
  version: number;
  mode: ReadinessMode | string;
  ready: boolean;
  next: string | null;
  machine: { id: string; name: string } | null;
  launcher: string;
  steps: ReadinessStep[];
  suggestions?: ReadinessSuggestion[];
  probe?: { ok: boolean; agent?: string; model?: string; answer?: string; millis?: number; error?: string };
  dismissed: boolean;
  checked_at: string;
}

// ── the steps ───────────────────────────────────────────

/** The steps that decide the mode, in order; 试一句 never blocks (5.1). */
const BLOCKING: string[] = ["computer", "claude", "model"];

/**
 * The step the card shows: in `setup`, the first of computer / claude / model that is todo or a problem. A rate limit
 * passes by itself and never turns the send button (the hub keeps `full` for it too); a step in a state this page does
 * not know counts as 「还差一步」 with its title (5 总规矩).
 */
export function blockingStep(r: Readiness | null | undefined): ReadinessStep | null {
  if (!r || r.mode !== "setup") return null;
  for (const id of BLOCKING) {
    const s = r.steps.find((x) => x.id === id);
    if (!s || s.state === "ok" || s.state === "skipped" || s.state === "unknown" || s.state === "checking") continue;
    if (s.code === "rate_limited") continue;
    return s;
  }
  // `setup` with no step this page reads as missing (a newer hub's step): the one `next` names
  if (!r.next || [...BLOCKING, "first_message"].includes(r.next)) return null;
  const next = r.steps.find((x) => x.id === r.next);
  return next && next.state !== "ok" && next.state !== "skipped" ? next : null;
}

/** What the send button says while the main conversation cannot answer (4.4: 先接上模型 / 先添加电脑). */
export function sendLabel(step: ReadinessStep): string {
  if (step.id === "computer") {
    if (step.code === "node_unreachable") return t("lib-readiness.send_wake_computer");
    if (step.code === "machine_pending") return t("lib-readiness.send_confirm_computer");
    if (step.code === "node_outdated") return t("lib-readiness.send_update_computer");
    return t("lib-readiness.send_add_computer");
  }
  return t("lib-readiness.send_connect_model");
}

// ── actions ─────────────────────────────────────────────

/** The help a button or a local example question opens in place (no page of its own on the web yet). */
export type HelpTopic = "add_computer" | "wake_computer" | "install_claude" | "login_claude" | "what_can_you_do" | "how_to_connect_model";

const HELP_TOPICS: string[] = ["add_computer", "wake_computer", "install_claude", "login_claude", "what_can_you_do", "how_to_connect_model"];

/** The help a local example question opens: its own when this page has it, else what Persona does (never sent). */
export function helpTopic(id: string): HelpTopic {
  return (HELP_TOPICS.includes(id) ? id : "what_can_you_do") as HelpTopic;
}

/** The action ids a card can draw a button for (5.1); an id this page does not know is text only (5 总规矩). */
const ACTIONS = new Set(["add_computer", "confirm_computer", "wake_computer", "install_claude", "login_claude", "connect_model", "edit_provider", "test_assistant", "enable_tls", "open_admin"]);

export function knownAction(id: string | undefined): boolean {
  return Boolean(id && ACTIONS.has(id));
}

export function actionLabel(id: string): string {
  switch (id) {
    case "add_computer":
      return t("lib-readiness.action_add_computer");
    case "confirm_computer":
      return t("lib-readiness.action_confirm_computer");
    case "wake_computer":
      return t("lib-readiness.action_wake_computer");
    case "install_claude":
      return t("lib-readiness.action_install_claude");
    case "login_claude":
      return t("lib-readiness.action_login_claude");
    case "connect_model":
      return t("lib-readiness.action_connect_model");
    case "edit_provider":
      return t("lib-readiness.action_edit_provider");
    case "test_assistant":
      return t("lib-readiness.action_test_assistant");
    case "enable_tls":
    case "open_admin":
      return t("lib-readiness.action_open_admin");
  }
  return id;
}

/** The hub's own admin page (served by the hub at /admin, its own sign-in). Adding a computer is there until a signed-in
 *  device can add one itself (5.3, `machines_add`). */
export const ADMIN_MACHINES = "/admin/machines";

/** 设置 › 模型供应商 with the wizard open (`add=1`), or one provider's details (`launcher=`). */
export function providersPath(launcher?: string): string {
  return launcher ? `/settings?tab=models&launcher=${encodeURIComponent(launcher)}` : "/settings?tab=models&add=1";
}

/**
 * What a button does: a help topic to open in place, or done here (a page opened, a check run). `launcher` is the
 * step's, for 改 Key.
 */
export function runAction(id: string, o: { launcher?: string; onHelp: (topic: HelpTopic) => void }): void {
  switch (id) {
    case "add_computer":
    case "wake_computer":
    case "install_claude":
    case "login_claude":
      return o.onHelp(id);
    case "confirm_computer":
      return navigate("/machines");
    case "connect_model":
      return navigate(providersPath());
    case "edit_provider":
      return navigate(o.launcher ? providersPath(o.launcher) : "/settings?tab=models");
    case "test_assistant":
      void refreshReadiness({ probe: true });
      return;
    case "enable_tls":
    case "open_admin":
      window.open("/admin", "_blank", "noopener,noreferrer");
      return;
  }
}

// ── coded failures (5.2) ────────────────────────────────

/** A start or a send the hub refused with a code: what the card shows. */
export interface CodedFailure {
  code: string;
  /** The hub's words, in the page's language. */
  error: string;
  /** The original text (a shell's error…): only under 详情. */
  detail?: string;
  action?: string;
  machine?: string;
  launcher?: string;
  requestId?: string;
}

/** The codes a failure card is drawn for; anything else is the hub's words with no button. */
const CODED = new Set(["no_machine", "machine_pending", "claude_missing", "launcher_missing", "node_unreachable", "assistant_offline", "node_outdated"]);

/** The card for a failed start or send: every failure is one (4.4), with the hub's words, and a button for its code. */
export function codedFailure(e: unknown): CodedFailure {
  if (!(e instanceof ApiError)) return { code: "", error: errorText(e) };
  const b = e.body;
  const str = (k: string) => (typeof b[k] === "string" && b[k] ? (b[k] as string) : undefined);
  const code = e.code && CODED.has(e.code) ? e.code : "";
  let action = str("action");
  if (!action && code === "node_unreachable") action = "wake_computer";
  if (!action && code === "no_machine") action = "add_computer";
  return { code, error: e.message, detail: str("detail"), action, machine: str("machine_name"), launcher: str("launcher"), requestId: str("request_id") ?? e.requestId };
}

/** The assistant restarting answers 503 assistant_offline for a few seconds: tried again quietly (4.6 原则 4). */
export function passesBySelf(e: unknown): boolean {
  return e instanceof ApiError && e.code === "assistant_offline";
}

// ── turn errors (turn_error_code) ───────────────────────

/** A turn that ended on a model error the user must fix: the card's title and its button (4.4「模型出错」). */
export function turnErrorFix(code: string | undefined): { title: string; detail: string; action: string } | null {
  switch (code) {
    case "not_logged_in":
      return { title: t("lib-readiness.turn_not_logged_in"), detail: t("lib-readiness.turn_not_logged_in_detail"), action: "connect_model" };
    case "auth_failed":
      return { title: t("lib-readiness.turn_auth_failed"), detail: t("lib-readiness.turn_auth_failed_detail"), action: "edit_provider" };
    case "model_route":
      return { title: t("lib-readiness.turn_model_route"), detail: t("lib-readiness.turn_model_route_detail"), action: "connect_model" };
  }
  return null;
}

// ── the store ───────────────────────────────────────────

interface ReadinessState {
  readiness: Readiness | null;
  /** The hub answered 404: an older hub, the page as before. */
  unsupported: boolean;
  probing: boolean;
  at: number;
}

let rs: ReadinessState = { readiness: null, unsupported: false, probing: false, at: 0 };
const listeners = new Set<() => void>();
let inflight: Promise<void> | null = null;

function set(patch: Partial<ReadinessState>) {
  rs = { ...rs, ...patch };
  listeners.forEach((l) => l());
}

/** How long a fetched answer is good for before a visit asks again. */
const FRESH_MS = 5_000;

/** Asks the hub again (a visit, a machine coming or going, after a failure, 试一句's probe). */
export function refreshReadiness(o: { force?: boolean; probe?: boolean } = {}): Promise<void> {
  if (!o.probe && !o.force && Date.now() - rs.at < FRESH_MS) return Promise.resolve();
  if (inflight && !o.probe) return inflight;
  if (o.probe) set({ probing: true });
  const p = api<Readiness>(o.probe ? `${READINESS_PATH}?probe=1` : READINESS_PATH)
    .then((r) => set({ readiness: r, unsupported: false, at: Date.now() }))
    .catch((e) => {
      // 404: an older hub; 503 assistant_offline and the rest: keep what was there, ask again next time
      if (e instanceof ApiError && e.status === 404) set({ unsupported: true, readiness: null, at: Date.now() });
    })
    .finally(() => {
      if (o.probe) set({ probing: false });
      if (inflight === p) inflight = null;
    });
  if (!o.probe) inflight = p;
  return p;
}

function subscribe(l: () => void) {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
}

/** For tests: forget everything. */
export function resetReadiness(r: Readiness | null = null) {
  rs = { readiness: r, unsupported: false, probing: false, at: r ? Date.now() : 0 };
  listeners.forEach((l) => l());
}

/**
 * The main conversation's readiness, or null on a hub without it (and until the first answer). Asked on a visit, when
 * the machines change (one added, one going offline or coming back) and when the window comes back into view.
 */
export function useReadiness(enabled = true): { readiness: Readiness | null; probing: boolean } {
  // a page that never shows the card (an agent session, a side panel) does not ask
  const supported = useStore((s) => hubHas(s.features, READINESS_FEATURE)) && enabled;
  const machines = useStore((s) => s.machines.map((m) => `${m.id}:${m.online ? 1 : 0}`).join(","));
  const s = useSyncExternalStore(subscribe, () => rs, () => rs);
  useEffect(() => {
    if (!supported) return;
    void refreshReadiness({ force: true });
  }, [supported, machines]);
  useEffect(() => {
    if (!supported) return;
    const on = () => document.visibilityState === "visible" && void refreshReadiness();
    document.addEventListener("visibilitychange", on);
    window.addEventListener("focus", on);
    return () => {
      document.removeEventListener("visibilitychange", on);
      window.removeEventListener("focus", on);
    };
  }, [supported]);
  if (!supported || s.unsupported) return { readiness: null, probing: false };
  return { readiness: s.readiness, probing: s.probing };
}
