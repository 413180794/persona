// Language catalogs are resources, not hand-maintained TypeScript entry points. Vite bundles one client catalog
// and one admin catalog per language from the namespace JSON beside _meta.json; adding a directory needs no code.
import { readdirSync, readFileSync } from "node:fs";
import { basename, dirname, join } from "node:path";
import type { Plugin } from "vite";
import { flatten } from "./src/i18n/locales/flatten";

/** The namespace JSON files of one catalog, sorted, as absolute paths. */
export function catalogFiles(metaPath: string, kind: "client" | "admin"): string[] {
  const dir = dirname(metaPath);
  return readdirSync(dir).filter((name) =>
    name.endsWith(".json") && !name.startsWith("_") && (name.startsWith("admin-") === (kind === "admin")),
  ).sort().map((name) => join(dir, name));
}

// The default form imports every namespace JSON, so the bundle and the dev server treat each as its own module.
// The inline form (tests) reads and flattens them here into one module: under Vitest every test file loads the
// catalogs, and ~300 JSON modules per language through the module runner were most of the suite's setup time.
export function catalogModule(metaPath: string, kind: "client" | "admin", inline = false): string {
  if (inline) {
    const files = Object.fromEntries(catalogFiles(metaPath, kind).map((file) =>
      [`./${basename(file)}`, JSON.parse(readFileSync(file, "utf8")) as Record<string, string>]));
    return `export default JSON.parse(${JSON.stringify(JSON.stringify(flatten(files)))});`;
  }
  const dir = dirname(metaPath);
  const namespaces = catalogFiles(metaPath, kind).map((file) => basename(file));
  const imports = namespaces.map((name, i) => `import ns${i} from ${JSON.stringify(join(dir, name).replace(/\\/g, "/"))};`);
  const entries = namespaces.map((name, i) => `${JSON.stringify(`./${name}`)}: ns${i}`);
  return [
    ...imports,
    `import { flatten } from ${JSON.stringify(join(dir, "../flatten.ts").replace(/\\/g, "/"))};`,
    `export default flatten({${entries.join(",")}});`,
  ].join("\n");
}

export const i18nCatalogs: Plugin = {
  name: "i18n-resource-catalogs",
  enforce: "pre",
  resolveId(source, importer) {
    const [path, query] = source.split("?");
    if (!path.endsWith("/_meta.json")) return;
    const kind = new URLSearchParams(query).get("catalog");
    const absolute = path.startsWith(".") && importer ? join(dirname(importer.split("?")[0]), path) : path;
    if (kind === "client" || kind === "admin") return `\0i18n-catalog:${kind}:${absolute}.js`;
  },
  load(id) {
    const match = /^\0i18n-catalog:(client|admin):(.+)\.js$/.exec(id);
    if (!match) return;
    const [, kind, path] = match;
    this.addWatchFile(path);
    const inline = Boolean(process.env.VITEST);
    // Inlined JSON is no longer an import, so watch each file for Vitest's watch mode to rerun on an edit.
    if (inline) for (const file of catalogFiles(path, kind as "client" | "admin")) this.addWatchFile(file);
    return catalogModule(path, kind as "client" | "admin", inline);
  },
};
