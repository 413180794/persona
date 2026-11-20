import assert from "node:assert/strict";
import { test } from "node:test";
import { quotaWindows, withQuotaCommand } from "./agy-usage-quota.mjs";

const at = new Date("2026-11-20T08:08:50Z");
const five = { window: "5h", reset_time: "2026-11-20T09:51:50Z", remaining_fraction: 0.78 };
const week = { window: "weekly", reset_time: "2026-11-25T09:45:19Z", remaining_fraction: 0.33 };
const reply = (groups) => ({ status: "SUCCESS", num_turns: 0, command: { data: { groups } } });
const quota = reply([
  { name: "Gemini Models", buckets: [week, five] },
  { name: "Claude and GPT models", buckets: [{ ...five, remaining_fraction: 1 }, { ...week, remaining_fraction: 0.99 }] },
]);

test("maps both explicit periods in each quota group with a measured time and preserves zero usage", () => {
  const windows = quotaWindows(quota, at);
  assert.deepEqual(windows.map((w) => [w.label, w.window_minutes, w.model, Math.round(w.used_percent)]), [
    ["5h", 300, "Claude and GPT models", 0],
    ["5h", 300, "Gemini Models", 22],
    ["7d", 10080, "Claude and GPT models", 1],
    ["7d", 10080, "Gemini Models", 67],
  ]);
  assert.equal(windows[3].sampled_at, at.toISOString());
  assert.equal(windows[3].resets_at, "2026-11-25T09:45:19.000Z");
});

test("ignores expired, malformed or unrecognized buckets and deduplicates a group and period", () => {
  const group = { name: "Gemini", buckets: [
    { ...week, remaining_fraction: "0.33" }, { ...week, remaining_fraction: null }, { ...week, remaining_fraction: 1.1 },
    { ...five, window: "daily" }, { ...week, reset_time: "yesterday" }, { ...week, reset_time: at.toISOString() },
    week, week,
  ] };
  assert.deepEqual(quotaWindows(reply([group]), at).map((w) => w.label), ["7d"]);
  assert.deepEqual(quotaWindows({ ...quota, num_turns: 1 }, at), []);
  assert.deepEqual(quotaWindows({ ...quota, status: "ERROR" }, at), []);
});

test("retains a recent quota on command failure and falls back to the native sampler after expiry", async () => {
  let now = at;
  let failing = false;
  let nativeCalls = 0;
  const native = { accounts: [{ plan: "Google AI Ultra", windows: [{ label: "5h" }] }] };
  const sampler = withQuotaCommand({
    read: () => native,
    refresh: async () => { nativeCalls++; },
  }, async () => {
    if (failing) throw new Error("test failure");
    return quota;
  }, () => now);
  assert.equal(sampler.read(), native);
  await sampler.refresh();
  const [account] = sampler.read().accounts;
  assert.equal(account.plan, "Google AI Ultra");
  assert.equal(account.windows.length, 4);
  assert.equal(account.stale, false);
  failing = true;
  await sampler.refresh();
  assert.equal(sampler.read().accounts[0].stale, true);
  assert.equal(sampler.read().accounts[0].sampled_at, at.toISOString());
  now = new Date(+at + 10 * 60_000);
  assert.equal(sampler.read(), native);
  assert.equal(nativeCalls, 2);
});
