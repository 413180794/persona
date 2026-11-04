import { describe, expect, it } from "vitest";
import { parseCookies, picked, siteOf, sitesOf } from "./cookies";

describe("parseCookies", () => {
  it("reads Netscape cookies.txt, with #HttpOnly_ lines", () => {
    const txt = [
      "# Netscape HTTP Cookie File",
      "",
      ".example.com\tTRUE\t/\tTRUE\t2000000000\tsid\tabc\tdef",
      "#HttpOnly_www.example.org\tFALSE\t/app\tFALSE\t0\tpref\tx",
      "broken line",
    ].join("\n");
    const r = parseCookies(txt);
    expect(r.format).toBe("netscape");
    expect(r.bad).toBe(1);
    expect(r.cookies).toEqual([
      { name: "sid", value: "abc\tdef", domain: ".example.com", path: "/", secure: true, expires: 2000000000, http_only: false, host_only: false },
      { name: "pref", value: "x", domain: "www.example.org", path: "/app", secure: false, expires: undefined, http_only: true, host_only: true },
    ]);
  });

  it("reads Cookie-Editor / EditThisCookie JSON", () => {
    const r = parseCookies(
      JSON.stringify([
        { domain: ".example.com", expirationDate: 2000000000.5, hostOnly: false, httpOnly: true, name: "sid", path: "/", sameSite: "lax", secure: true, session: false, value: "v" },
        { domain: "shop.example.com", hostOnly: true, name: "cart", path: "/", sameSite: "no_restriction", secure: true, session: true, value: "1" },
        { domain: "x.test", expires: 2000000000000, name: "ms", value: "1" }, // milliseconds
        { name: "no domain", value: "1" },
      ]),
    );
    expect(r).toMatchObject({ format: "json", bad: 1 });
    expect(r.cookies[0]).toMatchObject({ name: "sid", expires: 2000000000, http_only: true, same_site: "Lax", secure: true, host_only: false });
    expect(r.cookies[1]).toMatchObject({ name: "cart", expires: undefined, same_site: "None", host_only: true });
    expect(r.cookies[2].expires).toBe(2000000000);
    expect(parseCookies(JSON.stringify({ cookies: [{ name: "a", value: "b", domain: "c.d" }] })).cookies).toHaveLength(1);
  });

  it("explains what is no cookie file", () => {
    expect(() => parseCookies("{oops")).toThrow("读不懂");
    expect(() => parseCookies("[]")).toThrow("没有 cookie");
    expect(() => parseCookies("hello\nworld")).toThrow("cookies.txt");
  });
});

describe("sites", () => {
  it("groups by site, counts the expired apart, and picks the ticked", () => {
    expect([siteOf(".www.example.com"), siteOf("mail.example.co.uk"), siteOf("a.b.example.com.cn"), siteOf("localhost"), siteOf("198.51.100.7")]).toEqual([
      "example.com",
      "example.co.uk",
      "example.com.cn",
      "localhost",
      "198.51.100.7",
    ]);
    const now = 1_900_000_000;
    const sites = sitesOf(
      [
        { name: "a", value: "1", domain: ".example.com", expires: 2_000_000_000 },
        { name: "b", value: "2", domain: "www.example.com" },
        { name: "old", value: "3", domain: "example.com", expires: 1_000 },
        { name: "c", value: "4", domain: "shop.example.org", expires: 1_950_000_000 },
      ],
      now,
    );
    expect(sites.map((s) => [s.site, s.cookies.length, s.expired, s.expires])).toEqual([
      ["example.com", 2, 1, 2_000_000_000],
      ["example.org", 1, 0, 1_950_000_000],
    ]);
    expect(picked(sites, new Set(["example.org"])).map((c) => c.name)).toEqual(["c"]);
  });
});
