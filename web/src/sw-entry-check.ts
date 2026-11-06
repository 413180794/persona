// Build only (vite.config.ts classicWorker): sw.js is registered as a classic script (lib/pwa.ts), which can't import.
// When the page and src/sw.ts both import a value from one module, Rollup splits it into a shared chunk that sw.js
// imports, and the worker never installs; tsc and the unit tests can't see it (docs/lessons/web.md, #5070).

// An import statement at the start of a statement: `import"./a.js"`, `import{b as c}from"./a.js"`, `import d from …`.
const IMPORT_STATEMENT = /(?:^|[\n;}])\s*import\s*(?:["']|[\w$*{][^;"'`]*?\bfrom\s*["'])/;

/** Why the built sw.js can't run as a classic worker, as one sentence for the build to fail with; undefined when it can. */
export function classicWorkerProblem(code: string, imports: readonly string[] = []): string | undefined {
  if (!imports.length && !IMPORT_STATEMENT.test(code)) return undefined;
  const what = imports.length ? imports.join(", ") : "another module";
  return (
    `dist/sw.js imports ${what}, and a classic service worker can't: the page and src/sw.ts import a value from the same module, ` +
    `so it became a shared chunk. Give the page its own copy of that value and import only types across ` +
    `(docs/lessons/web.md, the section on the service worker and the page sharing a module).`
  );
}
