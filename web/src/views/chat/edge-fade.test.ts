import { describe, expect, it } from "vitest";
import { edgeMask } from "./edge-fade";

describe("a sideways row's fade", () => {
  it("fades the edges that have more, none when it all fits", () => {
    expect(edgeMask({ left: false, right: false })).toBeUndefined();
    expect(edgeMask({ left: false, right: true })).toBe("linear-gradient(90deg,#000 calc(100% - 44px),transparent)");
    expect(edgeMask({ left: true, right: false })).toBe("linear-gradient(90deg,transparent,#000 28px)");
    expect(edgeMask({ left: true, right: true })).toContain("transparent,#000 28px,#000 calc(100% - 44px),transparent");
  });
});
