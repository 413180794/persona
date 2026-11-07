// The assistant's memory (docs/assistant/assistant.md, docs/protocol/protocol.md `/assistant/memory`): one Markdown file per memory with a
// frontmatter (name, description, type), plus the index MEMORY.md whose lines "- [title](file.md) — note" give friendlier
// titles than the slugs. The iOS app's AssistantMemoryEntry / AssistantMemoryView, as pure functions.

import { t } from "../i18n";

export const MEMORY_INDEX = "MEMORY.md";

export interface MemoryFile {
  name: string;
  content: string;
  updated_at?: string;
}

export interface ParsedMemory {
  name?: string;
  description?: string;
  type?: string;
  /** The text after the frontmatter (the whole file when there is none). */
  body: string;
}

/** Splits a memory file into its frontmatter and body. `type` may sit under `metadata:` (indented); older files have it
 *  at the top level. Quoted values lose their quotes; empty ones are left out. */
export function parseMemory(content: string): ParsedMemory {
  const lines = content.replace(/\r\n/g, "\n").split("\n");
  if (lines[0]?.trim() !== "---") return { body: content.trim() };
  const end = lines.findIndex((l, i) => i > 0 && l.trim() === "---");
  if (end < 0) return { body: content.trim() };
  const out: ParsedMemory = { body: lines.slice(end + 1).join("\n").trim() };
  for (const line of lines.slice(1, end)) {
    const colon = line.indexOf(":");
    if (colon < 0) continue;
    const key = line.slice(0, colon).trim();
    let value = line.slice(colon + 1).trim();
    if (value.length >= 2 && (value[0] === '"' || value[0] === "'") && value[value.length - 1] === value[0]) value = value.slice(1, -1);
    if (!value) continue;
    if (key === "name" || key === "description" || key === "type") out[key] = value;
  }
  return out;
}

/** The index's lines by file: "- [Morning brief format](morning-format.md) — one screen at most" → { "morning-format.md": { title, hook } }. */
export function indexTitles(index: string): Record<string, { title: string; hook?: string }> {
  const out: Record<string, { title: string; hook?: string }> = {};
  for (const line of index.split("\n")) {
    const m = /\[([^\]]+)\]\(([^)\s]+)\)(.*)$/.exec(line);
    if (!m) continue;
    const hook = m[3].replace(/^[\s—–\-:：]+/, "").trim(); // i18n-ignore: separators written in the index
    out[m[2]] = { title: m[1].trim(), hook: hook || undefined };
  }
  return out;
}

export interface MemoryEntry extends ParsedMemory {
  file: MemoryFile;
  /** The index's title, else the frontmatter's name, else the file name. */
  title: string;
  /** The frontmatter's description, else the index line's hook. */
  detail?: string;
}

/** The memories (not the index), newest first, titled. */
export function memoryEntries(files: MemoryFile[]): MemoryEntry[] {
  const index = files.find((f) => f.name === MEMORY_INDEX);
  const titles = index ? indexTitles(index.content) : {};
  const time = (f: MemoryFile) => (f.updated_at ? Date.parse(f.updated_at) || 0 : 0);
  return files
    .filter((f) => f.name !== MEMORY_INDEX)
    .sort((a, b) => time(b) - time(a) || a.name.localeCompare(b.name))
    .map((file) => {
      const p = parseMemory(file.content);
      const t = titles[file.name];
      return { ...p, file, title: t?.title || p.name || file.name, detail: p.description || t?.hook };
    });
}

export type MemoryTone = "accent" | "orange" | "green" | "neutral";

/** "User" / "Feedback" / "Project" / "Reference", or the raw type. */
export function memoryTypeLabel(type?: string): string | undefined {
  switch (type) {
    case "user":
      return t("lib-memory.type_user");
    case "feedback":
      return t("lib-memory.type_feedback");
    case "project":
      return t("lib-memory.type_project");
    case "reference":
      return t("lib-memory.type_reference");
    default:
      return type || undefined;
  }
}

export function memoryTypeTone(type?: string): MemoryTone {
  return type === "user" ? "accent" : type === "feedback" ? "orange" : type === "project" ? "green" : "neutral";
}
