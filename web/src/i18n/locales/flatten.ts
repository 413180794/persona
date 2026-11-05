/** `{"./settings.json": {"title": "…"}}` → `{"settings.title": "…"}`. */
export function flatten(files: Record<string, Record<string, string>>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [path, entries] of Object.entries(files)) {
    const ns = path.replace(/^.*\//, "").replace(/\.json$/, "");
    for (const [name, text] of Object.entries(entries)) out[`${ns}.${name}`] = text;
  }
  return out;
}
