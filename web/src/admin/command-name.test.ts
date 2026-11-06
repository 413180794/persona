import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { expect, it } from "vitest";

// The CLI is personad; jarvisd is only a compatibility alias. Admin UI text must not tell people to
// run `jarvisd hub …` (the systemd unit name `jarvisd` in journalctl / systemctl hints is fine).
const src = fileURLToPath(new URL("..", import.meta.url));

function files(dir: string, keep: (path: string) => boolean): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return files(path, keep);
    return keep(path) ? [path] : [];
  });
}

it("admin UI text names personad, not jarvisd, for hub commands", () => {
  const admin = files(join(src, "admin"), (p) => /\.(ts|tsx)$/.test(p) && !/\.test\.tsx?$/.test(p));
  const locales = files(join(src, "i18n", "locales"), (p) => /[\\/]admin-[^\\/]*\.json$/.test(p));
  expect(locales.length).toBeGreaterThan(0);
  const offenders = [...admin, ...locales].filter((p) => /jarvisd\s+hub\b/.test(readFileSync(p, "utf8")));
  expect(offenders.map((p) => p.slice(src.length))).toEqual([]);
});
