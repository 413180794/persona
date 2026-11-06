import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { expect, it } from "vitest";

// The admin UI uses the one canonical server command.
const src = fileURLToPath(new URL("..", import.meta.url));

function files(dir: string, keep: (path: string) => boolean): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return files(path, keep);
    return keep(path) ? [path] : [];
  });
}

it("admin UI text names the canonical server command", () => {
  const admin = files(join(src, "admin"), (p) => /\.(ts|tsx)$/.test(p) && !/\.test\.tsx?$/.test(p));
  const locales = files(join(src, "i18n", "locales"), (p) => /[\\/]admin-[^\\/]*\.json$/.test(p));
  expect(locales.length).toBeGreaterThan(0);
  const commands = [...admin, ...locales].flatMap((p) => [...readFileSync(p, "utf8").matchAll(/\b(personad?)\s+hub\b/g)].map((m) => m[1]));
  expect(commands.length).toBeGreaterThan(0);
  expect([...new Set(commands)]).toEqual(["personad"]);
});
