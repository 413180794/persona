// Under Vitest the catalog plugin inlines each catalog as one module (i18n-catalogs.ts). These tests pin that the
// inlined catalog holds exactly the strings the import form builds, so tests see the same keys as the page.
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { catalogFiles, catalogModule, i18nCatalogs } from "./i18n-catalogs";
import { flatten } from "./src/i18n/locales/flatten";

type Catalog = { default: Record<string, string> };

const metas = import.meta.glob("./src/i18n/locales/*/_meta.json");
const client = import.meta.glob<Catalog>("./src/i18n/locales/*/_meta.json", { query: "?catalog=client" });
const admin = import.meta.glob<Catalog>("./src/i18n/locales/*/_meta.json", { query: "?catalog=admin" });
// The namespace files as Vite's plain JSON imports, what the import form's `import nsN from …` resolve to.
const namespaces = import.meta.glob<Record<string, string>>(
  ["./src/i18n/locales/*/*.json", "!./src/i18n/locales/*/_*.json"],
  { eager: true, import: "default" },
);

/** flatten() over a language's namespace files of one kind, keyed like the import form's entries. */
function expected(lang: string, kind: "client" | "admin"): Record<string, string> {
  const prefix = `./src/i18n/locales/${lang}/`;
  const files = Object.entries(namespaces).filter(([path]) =>
    path.startsWith(prefix) && path.slice(prefix.length).startsWith("admin-") === (kind === "admin"));
  return flatten(Object.fromEntries(files.map(([path, entries]) => [`./${path.slice(prefix.length)}`, entries])));
}

const langs = Object.keys(metas).map((path) => path.split("/").at(-2)!);

describe("i18n catalogs under Vitest", () => {
  it("finds every language", () => {
    expect(langs).toEqual(expect.arrayContaining(["en", "zh-Hans"]));
  });

  for (const lang of langs) {
    const key = `./src/i18n/locales/${lang}/_meta.json`;
    it(`${lang}: the inlined client catalog equals flatten() of its namespace files`, async () => {
      const catalog = (await client[key]()).default;
      expect(Object.keys(catalog).length).toBeGreaterThan(0);
      expect(catalog).toEqual(expected(lang, "client"));
    });
    it(`${lang}: the inlined admin catalog equals flatten() of its admin-*.json files`, async () => {
      expect((await admin[key]()).default).toEqual(expected(lang, "admin"));
    });
  }
});

describe("catalogModule", () => {
  const meta = fileURLToPath(new URL("./src/i18n/locales/en/_meta.json", import.meta.url));

  it("emits one import per namespace file outside tests, as the build bundles it", () => {
    const code = catalogModule(meta, "client");
    const files = catalogFiles(meta, "client");
    expect(files.length).toBeGreaterThan(1);
    expect(code.match(/^import ns\d+ from /gm)).toHaveLength(files.length);
    expect(code).toContain(`import { flatten } from `);
    expect(code).toMatch(/^export default flatten\(\{/m);
    expect(code).not.toContain("JSON.parse");
  });

  it("emits a single module without imports when inlined", () => {
    const code = catalogModule(meta, "admin", true);
    expect(code).not.toMatch(/^import /m);
    expect(code).toMatch(/^export default JSON\.parse\(/);
  });

  it("inlines under Vitest and watches every namespace file", () => {
    const watched: string[] = [];
    const load = i18nCatalogs.load as (this: { addWatchFile(file: string): void }, id: string) => string;
    const code = load.call({ addWatchFile: (file) => watched.push(file) }, `\0i18n-catalog:client:${meta}.js`);
    expect(code).toBe(catalogModule(meta, "client", true));
    expect(watched).toEqual([meta, ...catalogFiles(meta, "client")]);
  });

  it("splits client and admin namespaces by the admin- prefix", () => {
    const name = (file: string) => file.replace(/^.*\//, "");
    expect(catalogFiles(meta, "admin").every((file) => name(file).startsWith("admin-"))).toBe(true);
    expect(catalogFiles(meta, "client").some((file) => name(file).startsWith("admin-"))).toBe(false);
    expect(catalogFiles(meta, "client").some((file) => name(file).startsWith("_"))).toBe(false);
  });
});
