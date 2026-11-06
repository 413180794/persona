import { describe, expect, it } from "vitest";
import { foldedNow } from "./fold";

describe("foldedNow", () => {
  it("follows the default until the user chooses", () => {
    expect(foldedNow(null, false)).toBe(false);
    // the todos arrived after the first render: "Unassigned"'s default became folded (#2249)
    expect(foldedNow(null, true)).toBe(true);
  });

  it("keeps what the user chose, whatever the default is now", () => {
    expect(foldedNow("1", false)).toBe(true);
    expect(foldedNow("0", true)).toBe(false);
  });
});
