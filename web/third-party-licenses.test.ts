// The web build's third-party notices cover every package it can ship: a new dependency fails here until its license
// is accepted, its choice of license made, or its missing license file acknowledged (third-party-licenses.ts).
import { describe, expect, it } from "vitest";
import { ACCEPTED, CHOSEN, licenseTexts, NO_LICENSE_FILE, notices, shippedPackages, sourceUrl, UNDECLARED, withoutEmails } from "./third-party-licenses";

const packages = shippedPackages();
const named = (name: string) => packages.filter((p) => p.name === name);

describe("third-party licenses of the web build", () => {
  it("uses every package under an accepted license, choosing one where it offers several", () => {
    const refused = packages.filter((p) => !p.license?.split(" AND ").every((l) => ACCEPTED.has(l)));
    expect(refused.map((p) => `${p.name}@${p.version}: ${p.declared ?? "no license declared"}`)).toEqual([]);
  });

  it("keeps no stale choice or undeclared license", () => {
    for (const [name, license] of Object.entries(CHOSEN)) {
      expect(named(name).length, name).toBeGreaterThan(0);
      for (const p of named(name)) expect(p.declared?.split(" OR "), name).toContain(license);
    }
    for (const name of Object.keys(UNDECLARED)) {
      expect(named(name).length, name).toBeGreaterThan(0);
      for (const p of named(name)) expect(p.declared, name).toBeUndefined();
    }
  });

  it("has each package's license text, or knows it is published without one", () => {
    expect(new Set(packages.filter((p) => licenseTexts(p.dir).length === 0).map((p) => p.name))).toEqual(NO_LICENSE_FILE);
  });

  it("lists every package with an https source and no email address", () => {
    const text = notices(packages);
    for (const p of packages) expect(text).toContain(`\n${p.name} ${p.version}\nLicense: ${p.license}`);
    for (const [name, license] of Object.entries(CHOSEN)) expect(text).toMatch(new RegExp(`\\n${name} \\S+\\nLicense: ${license} \\(offered: `));
    expect(text).not.toMatch(/^Source: (?!https:\/\/)/m);
    expect(text.match(/[\w.%+-]+@[\w-]+(?:\.[\w-]+)*\.[A-Za-z]{2,}/g)).toBeNull();
  });

  it("leaves out email addresses and keeps the rest of the line", () => {
    expect(withoutEmails("Copyright (c) 2015 A B <a@example.com>")).toBe("Copyright (c) 2015 A B");
    expect(withoutEmails("Copyright (c) A B <mailto:a@example.com>")).toBe("Copyright (c) A B");
    expect(withoutEmails("Copyright (c) A B <a@example.com> (example.com)")).toBe("Copyright (c) A B (example.com)");
    expect(withoutEmails("Copyright (c) A B (a@example.com)")).toBe("Copyright (c) A B");
    expect(withoutEmails("Copyright (c) 2013 A B (a@example.com, https://example.com/ab)")).toBe("Copyright (c) 2013 A B (https://example.com/ab)");
    expect(withoutEmails("Write to a@example.com.")).toBe("Write to.");
  });

  it("turns package.json repository forms into browsable URLs", () => {
    expect(sourceUrl("git+ssh://git@example.com/a/b.git", "b", "1.0.0")).toBe("https://example.com/a/b");
    expect(sourceUrl({ type: "git", url: "git://github.com/a/b.git" }, "b", "1.0.0")).toBe("https://github.com/a/b");
    expect(sourceUrl("git@example.com:a/b.git", "b", "1.0.0")).toBe("https://example.com/a/b");
    expect(sourceUrl("http://github.com/a/b", "b", "1.0.0")).toBe("https://github.com/a/b");
    expect(sourceUrl("github:a/b", "b", "1.0.0")).toBe("https://github.com/a/b");
    expect(sourceUrl("a/b", "b", "1.0.0")).toBe("https://github.com/a/b");
    expect(sourceUrl(undefined, "@s/b", "2.0.0")).toBe("https://www.npmjs.com/package/@s/b/v/2.0.0");
  });
});
