import { describe, expect, it } from "vitest";
import { SANS, SERIF, withFallback } from "./fonts";

describe("withFallback", () => {
  it("puts the system sans after fonts this machine may lack", () => {
    expect(withFallback("Calibri")).toBe(`Calibri,${SANS}`);
    expect(withFallback('"等线", "Microsoft YaHei"')).toBe(`"等线", "Microsoft YaHei",${SANS}`);
  });
  it("keeps serif for fonts that are serif", () => {
    expect(withFallback("'Times New Roman'")).toBe(`'Times New Roman',${SERIF}`);
    expect(withFallback("宋体")).toBe(`宋体,${SERIF}`);
    expect(withFallback("SimSun")).toBe(`SimSun,${SERIF}`);
  });
  it("leaves lists that already end in a generic family", () => {
    expect(withFallback("Arial, sans-serif")).toBe("Arial, sans-serif");
    expect(withFallback("")).toBe("");
  });
  it("puts Chinese sans fonts before the generic one", () => {
    expect(SANS.indexOf("PingFang SC")).toBeLessThan(SANS.indexOf("sans-serif"));
    expect(SANS).toContain("Noto Sans CJK SC");
  });
});
