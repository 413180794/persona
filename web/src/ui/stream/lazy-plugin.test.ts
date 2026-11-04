import { describe, expect, it, vi } from "vitest";
import { lazyPlugin } from "./lazy-plugin";

describe("a plugin loaded when a reply first needs it", () => {
  it("loads once, however many ask", async () => {
    const importer = vi.fn(async () => "p");
    const lp = lazyPlugin(importer);
    expect(await Promise.all([lp.load(), lp.load()])).toEqual(["p", "p"]);
    expect(await lp.load()).toBe("p");
    expect(importer).toHaveBeenCalledTimes(1);
  });

  it("is asked for again after its chunk failed (the hub redeployed under the page)", async () => {
    const importer = vi.fn().mockRejectedValueOnce(new Error("chunk")).mockResolvedValueOnce("p");
    const lp = lazyPlugin(importer);
    await expect(lp.load()).rejects.toThrow("chunk");
    expect(await lp.load()).toBe("p");
    expect(importer).toHaveBeenCalledTimes(2);
  });
});
