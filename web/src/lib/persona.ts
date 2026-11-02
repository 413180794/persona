// Jarvis' persona (docs/persona.md): its name, its face and the animated dot, from a package on the hub
// (GET /personas, files under /personas/{id}/{file}). The choice is this browser's (the phone keeps its own, as the
// app does), and the built-in is the web's own mark: with no package chosen nothing on the page changes.
// The pure parts (checks, which file, which state) are here; the views and the list are views/assistant/Persona.tsx.

export const BUILTIN_PERSONA = "jarvis";
/** The phone's other built-in drawing (汇流): a package may not take its id either. */
const RESERVED = [BUILTIN_PERSONA, "jarvis-circuit"];

export type PersonaState = "idle" | "thinking" | "working" | "needs_you" | "offline";

/** The five states in the order the phone shows them (PersonaState.allCases), with their names. */
export const PERSONA_STATES: { id: PersonaState; title: string }[] = [
  { id: "idle", title: "空闲" },
  { id: "thinking", title: "思考中" },
  { id: "working", title: "工作中" },
  { id: "needs_you", title: "等你处理" },
  { id: "offline", title: "离线" },
];

/** persona.json, format 1. */
export interface PersonaManifest {
  format: number;
  id: string;
  name: string;
  subtitle?: string;
  avatar: { sizes: Record<string, string>; safe?: number };
  dot: { size?: number; states: Record<string, { file: string; fps?: number; frames?: number }> };
  colors?: { light?: Record<string, string>; dark?: Record<string, string> };
}

/** One entry of GET /personas. */
export interface PersonaInfo {
  id: string;
  rev: string;
  bytes?: number;
  manifest: PersonaManifest;
}

/** Why a package can't be used, or null (the hub checks the same: server/internal/hub/persona.go, PersonaManifest.problem). */
export function personaProblem(p: PersonaInfo): string | null {
  const m = p.manifest;
  if (!m || typeof m !== "object") return "no manifest";
  if (m.format !== 1) return `format ${m.format}`;
  if (m.id !== p.id || RESERVED.includes(m.id) || !/^[a-z0-9][a-z0-9-]{0,31}$/.test(m.id)) return `id ${m.id}`;
  if (!m.name?.trim()) return "no name";
  if (!m.avatar?.sizes || !Object.keys(m.avatar.sizes).length) return "no avatar";
  if (!m.dot?.states?.idle) return "no idle dot";
  for (const f of personaFiles(m)) {
    if (!f || f.startsWith("/") || f.split("/").some((x) => x === ".." || x.startsWith("."))) return `file ${f}`;
  }
  return null;
}

/** Every file the manifest names. */
export function personaFiles(m: PersonaManifest): string[] {
  return [...new Set([...Object.values(m.avatar?.sizes ?? {}), ...Object.values(m.dot?.states ?? {}).map((s) => s.file)])].sort();
}

/** The avatar file for a face `px` pixels wide: the smallest at least that big, else the largest. */
export function avatarFile(m: PersonaManifest, px: number): string | undefined {
  const sizes = Object.entries(m.avatar?.sizes ?? {})
    .map(([k, v]) => [Number(k), v] as const)
    .filter(([n]) => Number.isFinite(n))
    .sort((a, b) => a[0] - b[0]);
  return (sizes.find(([n]) => n >= px) ?? sizes[sizes.length - 1])?.[1];
}

/** The state's frames (an APNG, or a still PNG), idle's for a state the package lacks. */
export function dotFile(m: PersonaManifest, s: PersonaState): string | undefined {
  return (m.dot?.states?.[s] ?? m.dot?.states?.idle)?.file;
}

/** Where an <img> gets a package file: it can't send a header, so the token goes in the query (the hub never logs it). */
export function personaFileURL(p: Pick<PersonaInfo, "id" | "rev">, file: string, token: string): string {
  const path = file.split("/").map(encodeURIComponent).join("/");
  return `/api/v1/personas/${encodeURIComponent(p.id)}/${path}?rev=${encodeURIComponent(p.rev)}&token=${encodeURIComponent(token)}`;
}

/** The packages that check out, in the hub's order. */
export function usablePersonas(list: PersonaInfo[] | null | undefined): PersonaInfo[] {
  return (list ?? []).filter((p) => p && personaProblem(p) === null);
}

/**
 * What the face shows for a Jarvis conversation (AppModel.personaState, docs/persona.md「状态」): 离线 while the hub or
 * Jarvis' computer is out of reach, 等你处理 when it waits on you, 思考中 / 工作中 while a turn runs (thinking until it
 * writes or calls a tool), else 空闲.
 */
export function personaState(o: {
  online: boolean;
  machineOnline?: boolean;
  state?: string;
  interactions?: number;
  last?: { role: string; kind: string } | null;
}): PersonaState {
  if (!o.online || o.machineOnline === false) return "offline";
  if (o.state === "needs_you" || (o.interactions ?? 0) > 0) return "needs_you";
  if (o.state !== "working") return "idle";
  return !o.last || o.last.role === "user" || o.last.kind === "thinking" ? "thinking" : "working";
}
