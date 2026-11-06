import { afterEach, expect, it, vi } from "vitest";
import { addServiceMachine, adminApi, repairConnector, setCsrf } from "./api";
import { installMock } from "./mock";

afterEach(() => {
  setCsrf("");
  vi.useRealTimers();
});

it("the admin mock accepts both connector and peer input aliases but keeps legacy output", async () => {
  vi.useFakeTimers();
  for (const scheme of ["persona"]) {
    installMock("center");
    setCsrf("mock-csrf");
    const pairLink = `${scheme}://connector?addr=192.0.2.30%3A18663&fp=fixture-pin&code=fixture-code`;
    const machine = addServiceMachine({ id: "fixture-machine", name: "Fixture", icon: "server", pair_link: pairLink });
    await vi.runAllTimersAsync();
    expect((await machine).machine.id).toBe("fixture-machine");
    const repaired = repairConnector("example-workstation", pairLink);
    await vi.runAllTimersAsync();
    expect((await repaired).ok).toBe(true);
    const peer = adminApi("/peers", "POST", { code: `${scheme}://peer?hub=fixture&fp=fixture-pin&code=fixture-code` });
    await vi.runAllTimersAsync();
    expect(await peer).toHaveProperty("hub_id");
  }
  const output = adminApi<{ code: string }>("/peers/code", "POST");
  await vi.runAllTimersAsync();
  expect((await output).code.startsWith("persona://peer?")).toBe(true);
});
