import { describe, expect, it } from "vitest";
import { accountOf, screenMachine, screenSocket, screenTarget } from "./screen";

describe("the live screen's page", () => {
  it("is /screen/<machine>", () => {
    expect(screenMachine("/screen/macbook")).toBe("macbook");
    expect(screenMachine("/screen/mac%20mini/")).toBe("mac mini");
    expect(screenMachine("/screen")).toBeNull();
    expect(screenMachine("/machines/macbook")).toBeNull();
  });

  it("takes the app's token from the fragment, out of the address bar", () => {
    const { target, rest } = screenTarget("http://hub.example:18660/screen/macbook#token=t0k&embed=1", () => "stored");
    expect(target).toEqual({ mid: "macbook", token: "t0k", embedded: true });
    expect(rest).toBe("/screen/macbook");
  });

  it("uses the browser's own token without one", () => {
    expect(screenTarget("https://hub.example/screen/macbook", () => "stored").target).toEqual({ mid: "macbook", token: "stored", embedded: false });
    expect(screenTarget("https://hub.example/inbox#token=x", () => "stored").target).toBeNull();
  });

  it("connects through the hub's port relay", () => {
    expect(screenSocket({ protocol: "http:", host: "hub.example:18660" }, "mac book", "a&b")).toBe(
      "ws://hub.example:18660/api/v1/machines/mac%20book/ports/5900/connect?token=a%26b",
    );
    expect(screenSocket({ protocol: "https:", host: "hub.example" }, "m", "t")).toMatch(/^wss:\/\//);
  });

  it("guesses the account from the home directory", () => {
    expect(accountOf("/Users/me")).toBe("me");
    expect(accountOf("/home/me")).toBe("");
    expect(accountOf(undefined)).toBe("");
  });
});
