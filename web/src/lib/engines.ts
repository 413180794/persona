// "Engine" on a machine's page (docs/engines.md, iOS EngineSelectionModel / EngineSelectionView): which terminal engine the
// computer's sessions come from. The hub keeps one per machine (herdr when never chosen) and takes a new one with
// PUT /api/v1/machines/{mid}/engine, also while the machine is offline (applied when it connects).
import { t } from "../i18n";
import type { DetectedEngine, Machine } from "./types";

/** The engines this client knows (the hub's proto.EngineNames), in the order they are listed when the node reports none. */
export const KNOWN_ENGINES: { id: string; name: string; detail: string }[] = [
  { id: "herdr", name: "herdr", detail: t("lib-engines.detail_herdr") },
  { id: "herdr-core", name: "Herdr App", detail: t("lib-engines.detail_herdr_core") },
  { id: "herdr-both", name: "herdr + Herdr App", detail: t("lib-engines.detail_herdr_both") },
  { id: "orca", name: "Orca", detail: t("lib-engines.detail_orca") },
  { id: "paseo", name: "Paseo", detail: t("lib-engines.detail_paseo") },
];

const known = (id: string) => KNOWN_ENGINES.find((e) => e.id === id);

type EngineMachine = Pick<Machine, "engine" | "engines" | "online">;

/** The machine's engine: herdr when it was never chosen (an engine is always picked; nothing is guessed from detection). */
export function currentEngine(m: Pick<Machine, "engine">): string {
  return m.engine || "herdr";
}

/** An engine's name: the node's words first, then ours, then its id (a future engine). */
export function engineName(id: string, m?: Pick<Machine, "engines">): string {
  return m?.engines?.find((e) => e.id === id)?.name || known(id)?.name || id;
}

/** One line on what the engine is; none for an engine this client does not know. */
export function engineDetail(id: string): string | undefined {
  return known(id)?.detail;
}

/** The choices: what the node reports (names, notes and engines newer than this client), else — an older hub, an
 * offline machine — every known engine, which can still be configured. */
export function engineOptions(m: Pick<Machine, "engines">): DetectedEngine[] {
  if (m.engines?.length) return m.engines;
  return KNOWN_ENGINES.map((e) => ({ id: e.id, name: e.name, available: false }));
}

/** A known engine can always be chosen (offline too, or not detected yet: it applies once there); one this client does
 * not know only while the node reports it available. */
export function canSelectEngine(id: string, m: Pick<Machine, "engines">): boolean {
  return !!known(id) || !!m.engines?.some((e) => e.id === id && e.available);
}

/** What the node found of the engine: "Available" (with its version) / "Not detected"; nothing to say while offline. */
export function detectionText(id: string, m: EngineMachine): string {
  if (!m.online) return t("lib-engines.detection_offline");
  const found = m.engines?.find((e) => e.id === id);
  if (!found) return t("lib-engines.detection_none");
  if (!found.available) return t("lib-engines.detection_not_found");
  return found.version ? t("lib-engines.detection_available_version", { version: found.version }) : t("lib-engines.detection_available");
}

/** The line under the list: what the choice decides, and when it applies. */
export function engineFooter(selected: string, online: boolean): string {
  if (selected === "herdr-both")
    return online ? t("lib-engines.footer_both") : t("lib-engines.footer_both_offline");
  return online ? t("lib-engines.footer_single") : t("lib-engines.footer_one_offline");
}

/** Whether "Save" is offered: a change the hub can take. */
export function canSaveEngine(selected: string, m: Pick<Machine, "engine" | "engines">): boolean {
  return selected !== currentEngine(m) && canSelectEngine(selected, m);
}

export function engineSaveError(message: string): string {
  return t("lib-engines.save_failed", { message });
}
