// Machines › Add a computer (docs/onboarding.md 4.2 "Your computer ↔ hub", docs/protocol.md "Computer pairing codes"): a signed-in device adds a
// computer with POST /api/v1/auth/pair-codes {"add"}, hands its Persona Link the pairing link the hub returns, waits for
// it to come online and then says whether it is the user's (POST /api/v1/machines/{id}/confirm | reject).
import { t } from "../i18n";
import { ApiError, errorText } from "./api";
import { hubHas } from "./store";
import type { Machine } from "./types";

export const MACHINES_ADD_FEATURE = "machines_add";

/** Machines carry `trust` and take POST /machines/{id}/confirm | reject (docs/protocol.md "Computer trust"). */
export const MACHINE_TRUST_FEATURE = "machine_trust";

/** The systems the dialog has instructions for; the hub takes the same words (`add.os`). */
export type ComputerOS = "mac" | "linux" | "windows";

export const COMPUTER_OSES: readonly ComputerOS[] = ["mac", "linux", "windows"];

/** How to build and install Persona Link on a Mac until there is a download (docs/onboarding.md decision 1). */
export const PERSONA_LINK_INSTALL_DOC = "https://github.com/nwverse/persona/blob/master/docs/persona-link-install.md";

/** How to install jarvis-link on a headless Linux when the hub has no install script (docs/link.md "Linux (headless)"). */
export const LINK_LINUX_DOC = "https://github.com/nwverse/persona/blob/master/docs/link.md#linuxheadless";

// The hub does not keep the system a computer was added as (only its node reports one, once it connects), so the
// dialog remembers it per machine id: a new code for a computer that never connected then shows the same instructions.
const OS_KEY = "persona.addComputer.os.";

export function rememberOS(machine: string, os: ComputerOS): void {
  try {
    localStorage.setItem(OS_KEY + machine, os);
  } catch {
    // private mode or no storage: the dialog falls back to asking
  }
}

export function rememberedOS(machine: string): ComputerOS | null {
  try {
    const v = localStorage.getItem(OS_KEY + machine);
    return COMPUTER_OSES.includes(v as ComputerOS) ? (v as ComputerOS) : null;
  } catch {
    return null;
  }
}

/** The system of a computer: what its node said once it connected, else what this browser remembered, else null. */
export function knownOS(reported: string | undefined, machine: string): ComputerOS | null {
  const os = (reported ?? "").toLowerCase();
  if (os.startsWith("linux")) return "linux";
  if (os.startsWith("windows")) return "windows";
  if (os.startsWith("darwin") || os.startsWith("mac")) return "mac";
  return rememberedOS(machine);
}

/** The reply to a computer's code: the fields an app's code has (without `qr`) and the computer's own. */
export interface ComputerCodeReply {
  token: string;
  expires_at: string;
  pair_url: string;
  machine: string;
  name?: string;
  trust?: "pending" | "confirmed";
  /** This request added it (omitted when an existing pending one was reused). */
  added?: boolean;
  /** `jarvis-link pair '<pair_url>'`. */
  command?: string;
  /** One line that installs and pairs; only when the hub knows the site it was published from. */
  install?: { linux?: string };
}

/** Whether this hub lets a signed-in device add a computer; an old hub (or one that sent no features) does not. */
export function canAddComputer(features: string[]): boolean {
  return hubHas(features, MACHINES_ADD_FEATURE, false);
}

/** Whether this hub says which computers wait to be confirmed and takes "It's mine" / "Not mine". */
export function canConfirmComputer(features: string[]): boolean {
  return hubHas(features, MACHINE_TRUST_FEATURE, false);
}

export function addBody(name: string, os: ComputerOS) {
  return { add: { name: name.trim(), os } };
}

/** The line that pairs an installed jarvis-link; the reply's own when the hub gave one. */
export function pairCommand(reply: Pick<ComputerCodeReply, "pair_url" | "command">): string {
  return reply.command || `jarvis-link pair ${quoted(reply.pair_url)}`;
}

/** What a Linux computer runs: the one line that installs and pairs, or only the pairing when the hub has no site. */
export function linuxCommand(reply: Pick<ComputerCodeReply, "pair_url" | "command" | "install">): { line: string; installs: boolean } {
  const line = reply.install?.linux;
  return line ? { line, installs: true } : { line: pairCommand(reply), installs: false };
}

/** What Windows runs in PowerShell, in the folder with jarvis-link.exe and personad.exe (docs/windows.md). */
export function windowsCommand(pairURL: string): string {
  return `.\\jarvis-link.exe install --jarvisd .\\personad.exe --pair ${quoted(pairURL)}`;
}

// A pairing link in single quotes for sh and PowerShell; it has none of its own (it is URL-encoded).
function quoted(s: string): string {
  return `'${s.replace(/'/g, "%27")}'`;
}

export function isPending(m: Pick<Machine, "trust"> | undefined): boolean {
  return m?.trust === "pending";
}

/** A computer added here that has not come online once (the hub's error_code never_connected, or one waiting to be
 *  confirmed that the hub has never seen): it is not "Offline", it was never there. */
export function neverConnected(m: Pick<Machine, "online" | "trust" | "error_code" | "last_seen">): boolean {
  if (m.online) return false;
  return m.error_code === "never_connected" || (isPending(m) && !m.last_seen);
}

/** Where adding a computer is, from what the machine list says of it now. `offline`: only when confirming one with no
 *  code made here (it waits to be confirmed but is not online to be looked at). */
export type AddStage = "waiting" | "expired" | "joined" | "connected" | "gone" | "offline";

/** `seen`: the list has had it since the code was made (a new one may not be in the list the page has yet). */
export function addStage(m: Machine | undefined, secondsLeft: number, seen: boolean): AddStage {
  if (!m && seen) return "gone"; // rejected elsewhere, or removed after waiting too long
  if (!m) return secondsLeft > 0 ? "waiting" : "expired";
  if (m.online) return isPending(m) ? "joined" : "connected";
  return secondsLeft > 0 ? "waiting" : "expired";
}

/** Confirming a computer the user did not just add here (no code): asked only once it is online. */
export function confirmStage(m: Machine | undefined): AddStage {
  if (!m) return "gone";
  if (!isPending(m)) return "connected"; // confirmed elsewhere meanwhile
  return m.online ? "joined" : "offline";
}

/**
 * The computer "Confirm computer" opens: `preferred` (the step's machine) while it waits to be confirmed, else the first one waiting
 * that is online (it can be asked about now), else the first one waiting; undefined when none waits.
 */
export function pendingToConfirm(machines: Machine[], preferred?: string): Machine | undefined {
  const waiting = machines.filter(isPending);
  return waiting.find((m) => preferred && m.id === preferred) ?? waiting.find((m) => m.online) ?? waiting[0];
}

/** A machine's error_code (docs/onboarding.md 5.3) in words; "" for a code this page does not know. */
export function machineErrorText(code: string | undefined): string {
  switch (code) {
    case "node_service_down":
      return t("lib-add-computer.code_node_service_down");
    case "link_offline":
      return t("lib-add-computer.code_link_offline");
    case "never_connected":
      return t("lib-add-computer.code_never_connected");
    case "node_outdated":
      return t("lib-add-computer.code_node_outdated");
    default:
      return "";
  }
}

/** Why the hub did not make a computer's code, in words, by its code; the hub's own words for any other. */
export function addErrorText(e: unknown): string {
  const code = e instanceof ApiError ? e.code : undefined;
  switch (code) {
    case "tls_required":
      return t("lib-add-computer.err_tls_required");
    case "mesh_member":
      return t("lib-add-computer.err_mesh_member");
    case "too_many_pending":
      return t("lib-add-computer.err_too_many_pending");
    case "forbidden":
      return t("lib-add-computer.err_forbidden");
    case "invalid_name":
      return t("lib-add-computer.err_invalid_name");
    default:
      return errorText(e);
  }
}
