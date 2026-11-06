// Language catalogs are resources, not hand-maintained TypeScript entry points. Vite bundles one client catalog
// and one admin catalog per language from the namespace JSON beside _meta.json; adding a directory needs no code.
import { readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import type { Plugin } from "vite";

export function catalogModule(metaPath: string, kind: "client" | "admin"): string {
  const dir = dirname(metaPath);
  const namespaces = readdirSync(dir).filter((name) =>
    name.endsWith(".json") && !name.startsWith("_") && (name.startsWith("admin-") === (kind === "admin")),
  ).sort();
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
    return catalogModule(path, kind as "client" | "admin");
  },
};
