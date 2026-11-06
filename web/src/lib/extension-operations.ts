// Installing, upgrading and removing an extension in steps (docs/protocol.md "Extension install, upgrade and uninstall progress", #3277): the
// hub runs each as an operation and pushes `extension_operation` as it goes; a page that opens again lists the ones
// in progress. Only with a hub that has `extension_operations`: an older one keeps the synchronous calls.
import { api, enc } from "./api";
import { extRoot } from "./extension-api";
import { changed } from "./extensions";
import { previewConsent } from "./extension-model";
import { bytes } from "./format";
import { getState, hubHas, onHubEvent } from "./store";
import { t } from "../i18n";
import type { MarketPreview } from "./extension-market";

export const OPERATIONS_FEATURE = "extension_operations";

export type OpStepState = "pending" | "running" | "done" | "failed" | "skipped";

/** One command-line program of the tools step (#4507): reused is the machine's own copy. */
export type OpTool = { name: string; state: "pending" | "running" | "done" | "failed" | (string & {}); reused?: boolean };

export type ExtensionOpStep = {
  id: string;
  state: OpStepState;
  /** The download's bytes; for the tools step, of the one program downloading. */
  bytes?: number;
  total?: number;
  cached?: boolean;
  machines?: { done: number; total: number; errors?: Record<string, string> };
  /** The tools step's programs, in the order they install. */
  tools?: OpTool[];
  started_at?: string;
  ended_at?: string;
};

export type ExtensionOperation = {
  id: string;
  kind: "install" | "upgrade" | "uninstall";
  extension: string;
  name?: string;
  market?: string;
  source?: string;
  version?: string;
  from?: string;
  actor?: string;
  state: "running" | "needs_consent" | "succeeded" | "failed" | "canceled";
  step?: string;
  steps: ExtensionOpStep[];
  error?: string;
  error_code?: string;
  preview_id?: string;
  retryable: boolean;
  /** "auth" when it succeeded and the extension still has to be signed in to: the page opens its sign-in. */
  next?: string;
  started_at: string;
  updated_at: string;
  ended_at?: string;
  /** Only from GET of one that waits for consent. */
  preview?: MarketPreview;
};

/** Whether this hub runs installs as operations. */
export const operationsOn = (features: string[] = getState().features) => hubHas(features, OPERATIONS_FEATURE);

// ── the hub ─────────────────────────────────────────────

const OPS = () => `${extRoot()}/-/operations`;
type Accepted = { operation: ExtensionOperation };

export const listOperations = () => api<{ operations?: ExtensionOperation[] }>(OPS()).then((r) => r.operations ?? []);
export const getOperation = (id: string) => api<ExtensionOperation>(`${OPS()}/${enc(id)}`);
export const retryOperation = (id: string) => api<Accepted>(`${OPS()}/${enc(id)}/retry`, { method: "POST" }).then((r) => track(r.operation));
export const cancelOperation = (id: string) => api<unknown>(`${OPS()}/${enc(id)}`, { method: "DELETE" });

/** Starts installing (or upgrading) a market's extension: download and checks in the background. */
export const startFromMarket = (market: string, extension: string) =>
  api<Accepted>(`${extRoot()}/-/catalog/preview`, { method: "POST", json: { market, extension, plugin: extension, async: true } }).then((r) => track(r.operation));
/** The user agreed to the preview: the same operation goes on. */
export const agreeTo = (pv: MarketPreview, upgradeOf?: string) =>
  api<Accepted>(upgradeOf ? `${extRoot()}/${enc(upgradeOf)}/-/upgrade` : `${extRoot()}/-/install`, {
    method: "POST",
    json: { ...previewConsent(pv), async: true },
  }).then((r) => track(r.operation));
export const startUninstall = (id: string, keepData = false, cascade = false, members = false) => {
  const q = new URLSearchParams({ async: "1" });
  if (keepData) q.set("keep_data", "1");
  if (cascade) q.set("cascade", "1");
  if (members) q.set("members", "1");
  return api<Accepted>(`${extRoot()}/${enc(id)}/-/?${q}`, { method: "DELETE" }).then((r) => track(r.operation));
};

// ── what the page knows ─────────────────────────────────

let ops = new Map<string, ExtensionOperation>();
const listeners = new Set<() => void>();
let started = false;
// The lists asked for, numbered: an answer older than one already applied is dropped. Each list on its way notes the
// operations tracked since it went out: the hub read it before those pushes or starts, so its copy of them may be
// older or missing (#4574).
let listing = 0;
let applied = 0;
const listsOut = new Set<Set<string>>();

function emit() {
  listeners.forEach((l) => l());
}

/** Keeps op (newer than what is known), and tells the pages; an operation that just succeeded refreshes the lists. */
export function track(op: ExtensionOperation): ExtensionOperation {
  const was = ops.get(op.id);
  if (was && Date.parse(was.updated_at) > Date.parse(op.updated_at)) return was;
  ops = new Map(ops).set(op.id, op);
  listsOut.forEach((since) => since.add(op.id));
  if (op.state === "succeeded" && was?.state !== "succeeded") changed(null);
  emit();
  return op;
}

/** Lists the hub's operations again (on opening, after a reconnect). What was pushed or started while the list was
 * on its way stays where it is newer than the list's copy; a list answering after a later one was applied is dropped. */
export async function reloadOperations() {
  if (!operationsOn()) return;
  const mine = ++listing;
  const since = new Set<string>();
  listsOut.add(since);
  try {
    const list = await listOperations();
    if (mine < applied) return;
    applied = mine;
    const next = new Map(list.map((o) => [o.id, o]));
    for (const id of since) {
      const known = ops.get(id);
      const listed = next.get(id);
      if (known && (!listed || Date.parse(known.updated_at) > Date.parse(listed.updated_at))) next.set(id, known);
    }
    ops = next;
    emit();
  } catch {
    // a device that may not see them, or the hub is away: keep what is known
  } finally {
    listsOut.delete(since);
  }
}

function start() {
  if (started) return;
  started = true;
  onHubEvent("extension_operation", (msg) => {
    if (msg.operation && typeof msg.operation === "object") track(msg.operation as ExtensionOperation);
  });
  // after hello (its features are in the store by then): events missed while away are not replayed
  onHubEvent("hello", () => void reloadOperations());
  void reloadOperations();
}

/** Subscribes to the operations; the first one starts listening. */
export function watchOperations(l: () => void): () => void {
  start();
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
}

export const operations = (): ExtensionOperation[] => [...ops.values()].sort((a, b) => Date.parse(b.started_at) - Date.parse(a.started_at));
export const forget = (id: string) => {
  ops = new Map(ops);
  ops.delete(id);
  emit();
};

// ── words and rules ─────────────────────────────────────

/** The newest operation of extension id (from market, when given), if it is going on or ended just now. */
export function latestFor(list: ExtensionOperation[], id: string, market?: string): ExtensionOperation | undefined {
  return list.find((o) => o.extension === id && (!market || !o.market || o.market === market));
}

export const isActive = (o?: ExtensionOperation) => o?.state === "running" || o?.state === "needs_consent";

/** The steps worth showing: those it skips are left out. */
export const shownSteps = (o: ExtensionOperation) => o.steps.filter((s) => s.state !== "skipped");

const STEP_KEYS: Record<string, string> = {
  download: "download",
  verify: "verify",
  unpack: "unpack",
  check: "check",
  consent: "consent",
  tools: "tools",
  install: "install",
  start: "start",
  nodes: "nodes",
  stop: "stop",
  remove: "remove",
};

/** A step's name; one this page does not know is shown as its id. */
export function stepLabel(id: string, kind?: ExtensionOperation["kind"]): string {
  if (id === "install" && kind === "upgrade") return t("lib-extension-operations.step_upgrade");
  const k = STEP_KEYS[id];
  return k ? t(`lib-extension-operations.step_${k}`) : id;
}

/** What a step says beside its name: the bytes of a download, the computers done, from the cache. */
export function stepDetail(s: ExtensionOpStep): string {
  if (s.id === "download") {
    if (s.cached) return t("lib-extension-operations.cached");
    if (s.total && s.state === "running") return t("lib-extension-operations.bytes_of", { done: bytes(s.bytes ?? 0), total: bytes(s.total) });
    if (s.bytes) return bytes(s.bytes);
    return "";
  }
  if (s.id === "tools" && s.tools?.length) {
    return t("lib-extension-operations.tools_count", { done: s.tools.filter((x) => x.state === "done").length, total: s.tools.length });
  }
  if (s.machines && s.machines.total > 0) return t("lib-extension-operations.machines", { done: s.machines.done, total: s.machines.total });
  return "";
}

/** One program of the tools step as the page draws it: its state, what it says beside its name, its download's fraction. */
export type ToolLine = { name: string; state: OpTool["state"]; detail: string; fraction?: number };

/**
 * The tools step's programs, one line each: a reused one says it is the machine's own; the one downloading has its
 * bytes and (its size known) a fraction.
 */
export function toolLines(s: ExtensionOpStep): ToolLine[] {
  return (s.tools ?? []).map((x) => {
    const downloading = x.state === "running" && s.state === "running";
    let detail = "";
    if (x.reused) detail = t("lib-extension-operations.tool_reused");
    else if (downloading && s.total) detail = t("lib-extension-operations.bytes_of", { done: bytes(s.bytes ?? 0), total: bytes(s.total) });
    else if (downloading && s.bytes) detail = bytes(s.bytes);
    const fraction = downloading && !x.reused && s.total ? Math.min(1, (s.bytes ?? 0) / s.total) : undefined;
    return { name: x.name, state: x.state, detail, fraction };
  });
}

/** Why a failed operation stopped, in words: the hub's error, led by what its code means when the page knows it. */
export function opError(o: ExtensionOperation): string {
  if (!o.error) return "";
  if (o.error_code === "tools_failed") return t("lib-extension-operations.tools_failed", { error: o.error });
  return t("settings-extensionmarket.op_error", { error: o.error });
}

/** An operation that ended well and leaves the extension to be signed in to (its next step is the sign-in). */
export const wantsSignIn = (o?: ExtensionOperation) => o?.state === "succeeded" && o.next === "auth";

/** A download's fraction (0–1), undefined when its size is not known or it is not downloading. */
export function stepFraction(s: ExtensionOpStep): number | undefined {
  if (s.id === "download" && s.state === "running" && s.total) return Math.min(1, (s.bytes ?? 0) / s.total);
  if (s.machines && s.state === "running" && s.machines.total) return s.machines.done / s.machines.total;
  return undefined;
}

/** The operation as one line: what it is doing now, or how it ended. */
export function opLine(o: ExtensionOperation): string {
  switch (o.state) {
    case "needs_consent":
      return t("lib-extension-operations.needs_consent");
    case "succeeded":
      return o.kind === "uninstall" ? t("lib-extension-operations.uninstalled") : o.kind === "upgrade" ? t("lib-extension-operations.upgraded") : t("lib-extension-operations.installed");
    case "failed":
      return t("lib-extension-operations.failed_at", { step: stepLabel(o.step ?? "", o.kind) });
    case "canceled":
      return t("lib-extension-operations.canceled");
  }
  const s = o.steps.find((x) => x.id === o.step);
  return s ? `${stepLabel(s.id, o.kind)}…` : t("lib-extension-operations.starting");
}

/** Done of the steps it takes (skipped ones left out), for a compact progress. */
export function stepsDone(o: ExtensionOperation): { done: number; total: number } {
  const shown = shownSteps(o);
  return { done: shown.filter((s) => s.state === "done").length, total: shown.length };
}
