import { afterEach, describe, expect, it, vi } from "vitest";
import { fileDownloadURL, fileThumbURL } from "./api";

describe("machine file URLs", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("carries the token in the query, since media elements send no header", () => {
    vi.stubGlobal("localStorage", { getItem: () => "jd test" });
    expect(fileDownloadURL("m 1", "a b.mp4", "/w")).toBe("/api/v1/machines/m%201/file/download?path=a%20b.mp4&cwd=%2Fw&token=jd%20test");
    expect(fileDownloadURL("m1", "/x.pdf", undefined, { inline: true })).toBe(
      "/api/v1/machines/m1/file/download?path=%2Fx.pdf&cwd=&inline=1&token=jd%20test",
    );
    expect(fileThumbURL("m1", "~/p.heic", "", 128)).toBe("/api/v1/machines/m1/file/thumb?path=~%2Fp.heic&cwd=&size=128&token=jd%20test");
  });
});
