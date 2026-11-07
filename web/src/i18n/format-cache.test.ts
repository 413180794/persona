import { afterEach, describe, expect, it, vi } from "vitest";
import { formatDate, formatList, formatLocale, formatNumber, formatRelative, initI18n } from "./index";

// The formatters are cached per (kind, locale, options): rows call them on every push, and building an Intl formatter
// costs far more than using one.

type Ctor = "RelativeTimeFormat" | "DateTimeFormat" | "NumberFormat" | "ListFormat";

/** Replaces Intl[name] with a subclass that counts constructions; afterEach puts the original back. */
function countConstructions(name: Ctor): { count: number } {
  const counter = { count: 0 };
  const Original = Intl[name] as unknown as new (...args: unknown[]) => object;
  const Counting = class extends Original {
    constructor(...args: unknown[]) {
      super(...args);
      counter.count++;
    }
  };
  restores.push(() => Object.defineProperty(Intl, name, { value: Original, configurable: true, writable: true }));
  Object.defineProperty(Intl, name, { value: Counting, configurable: true, writable: true });
  return counter;
}

const restores: (() => void)[] = [];

describe("Intl formatter cache", { timeout: 30_000 }, () => {
  afterEach(async () => {
    while (restores.length) restores.pop()!();
    vi.restoreAllMocks();
    await initI18n("zh-Hans");
  }, 30_000);

  it("builds one formatter for the same locale and options", () => {
    const relative = countConstructions("RelativeTimeFormat");
    const date = countConstructions("DateTimeFormat");
    for (let i = 0; i < 20; i++) {
      formatRelative(-i, "minute", { numeric: "always", style: "narrow" });
      formatDate(i * 86_400_000, { year: "2-digit", month: "narrow" });
    }
    expect(relative.count).toBe(1);
    expect(date.count).toBe(1);
  });

  it("builds separate formatters for different options", () => {
    const relative = countConstructions("RelativeTimeFormat");
    expect(formatRelative(-1, "day", { numeric: "always", style: "short" })).not.toBe(formatRelative(-1, "day", { numeric: "auto", style: "short" }));
    formatRelative(-1, "day", { numeric: "always", style: "short" });
    expect(relative.count).toBe(2);
  });

  it("resolves the locale once while the system language and interface language stay the same", () => {
    // Node's navigator reports the machine's locale, which earlier tests may have cached: start from another one.
    const languages = vi.spyOn(navigator, "languages", "get").mockReturnValue(["en-US"]);
    formatLocale();
    languages.mockReturnValue(["zh-CN"]);
    const canonical = vi.spyOn(Intl, "getCanonicalLocales");
    for (let i = 0; i < 10; i++) formatLocale();
    expect(canonical).toHaveBeenCalledTimes(1);
  });

  it("follows a change of system language or interface language", async () => {
    vi.spyOn(navigator, "languages", "get").mockReturnValue(["en-GB"]);
    await initI18n("en");
    expect(formatLocale()).toBe("en-GB");
    vi.spyOn(navigator, "languages", "get").mockReturnValue(["en-US"]);
    expect(formatLocale()).toBe("en-US");
    await initI18n("ja");
    expect(formatLocale()).toBe("ja");
  });

  it.each(["zh-Hans", "en", "ja"])("formats like a fresh Intl formatter in %s", async (language) => {
    vi.spyOn(navigator, "languages", "get").mockReturnValue([language]);
    await initI18n(language);
    const loc = formatLocale();
    const when = Date.UTC(2026, 10, 6, 9, 30);
    for (let round = 0; round < 2; round++) {
      expect(formatNumber(1234.5)).toBe(new Intl.NumberFormat(loc).format(1234.5));
      expect(formatNumber(0.25, { style: "percent" })).toBe(new Intl.NumberFormat(loc, { style: "percent" }).format(0.25));
      expect(formatDate(when)).toBe(new Intl.DateTimeFormat(loc, { dateStyle: "medium" }).format(when));
      expect(formatDate(when, { timeStyle: "short" })).toBe(new Intl.DateTimeFormat(loc, { timeStyle: "short" }).format(when));
      expect(formatRelative(-3, "hour")).toBe(new Intl.RelativeTimeFormat(loc, { numeric: "auto" }).format(-3, "hour"));
      expect(formatRelative(-1, "day")).toBe(new Intl.RelativeTimeFormat(loc, { numeric: "auto" }).format(-1, "day"));
      expect(formatList(["a", "b", "c"])).toBe(new Intl.ListFormat(loc, { type: "conjunction" }).format(["a", "b", "c"]));
    }
  });

  describe("system time zone", () => {
    const savedTZ = process.env.TZ;
    afterEach(() => {
      if (savedTZ === undefined) delete process.env.TZ;
      else process.env.TZ = savedTZ;
    });

    const when = Date.UTC(2026, 6, 6, 9, 30);
    const hm: Intl.DateTimeFormatOptions = { hour: "2-digit", minute: "2-digit", hourCycle: "h23" };

    it.each([
      ["UTC", "Asia/Shanghai"],
      // Same January offset, different July offset: Berlin has DST, Lagos does not.
      ["Europe/Berlin", "Africa/Lagos"],
    ])("follows a change from %s to %s", (from, to) => {
      process.env.TZ = from;
      expect(formatDate(when, hm)).toBe(new Intl.DateTimeFormat(formatLocale(), hm).format(when));
      process.env.TZ = to;
      expect(formatDate(when, hm)).toBe(new Intl.DateTimeFormat(formatLocale(), hm).format(when));
    });

    it("still builds one formatter while the zone stays the same", () => {
      process.env.TZ = "Europe/Berlin";
      const date = countConstructions("DateTimeFormat");
      for (let i = 0; i < 20; i++) formatDate(when + i * 60_000, { ...hm, second: "2-digit" });
      expect(date.count).toBe(1);
    });

    it("keeps an explicit timeZone formatter across a system zone change", () => {
      const date = countConstructions("DateTimeFormat");
      const utc = { ...hm, timeZone: "UTC" };
      process.env.TZ = "UTC";
      formatDate(when, utc);
      process.env.TZ = "Asia/Shanghai";
      expect(formatDate(when, utc)).toBe(new Intl.DateTimeFormat(formatLocale(), utc).format(when));
      expect(date.count).toBe(2); // one cached, one fresh in the assertion
    });
  });
});
