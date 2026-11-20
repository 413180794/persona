import { execFile } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";

const run = promisify(execFile);
const periods = new Map([["5h", ["5h", 300]], ["weekly", ["7d", 10080]]]);
const sampleLifetime = 10 * 60_000;

export function quotaWindows(reply, now) {
  const groups = reply?.command?.data?.groups;
  if (reply?.status !== "SUCCESS" || reply.num_turns !== 0 || !Array.isArray(groups)) return [];
  const sampledAt = now.toISOString();
  const windows = new Map();
  for (const group of groups) {
    const model = typeof group?.name === "string" ? group.name.trim() : "";
    if (!model || model.length > 40 || !Array.isArray(group.buckets)) continue;
    for (const bucket of group.buckets) {
      const period = periods.get(bucket?.window);
      const remaining = bucket?.remaining_fraction;
      const reset = Date.parse(bucket?.reset_time);
      if (!period || !Number.isFinite(remaining) || remaining < 0 || remaining > 1 ||
          !Number.isFinite(reset) || reset <= now.getTime()) continue;
      const [label, windowMinutes] = period;
      const key = `${windowMinutes}:${model}`;
      if (windows.has(key)) continue;
      windows.set(key, {
        label,
        window_minutes: windowMinutes,
        model,
        used_percent: (1 - remaining) * 100,
        resets_at: new Date(reset).toISOString(),
        sampled_at: sampledAt,
      });
    }
  }
  return [...windows.values()].sort((a, b) => a.window_minutes - b.window_minutes || a.model.localeCompare(b.model)).slice(0, 32);
}

export async function readQuotaCommand(bin) {
  // The documented slash command runs without a model turn. An empty workspace keeps local project hooks out.
  const cwd = await mkdtemp(join(tmpdir(), "persona-agy-usage-"));
  try {
    const { stdout } = await run(bin, ["-p", "/usage", "--output-format", "json", "--print-timeout", "25s"], {
      cwd, timeout: 40_000, maxBuffer: 1 << 20,
    });
    return JSON.parse(stdout);
  } finally {
    await rm(cwd, { recursive: true, force: true });
  }
}

/** Prefer explicit CLI buckets, with the existing 5-hour sampler as a fallback and source of the tier name. */
export function withQuotaCommand(nativeSampler, readCommand, now = () => new Date()) {
  let current;
  let refreshing = false;

  async function updateQuota() {
    try {
      const reply = await readCommand();
      const at = now();
      const windows = quotaWindows(reply, at);
      if (!windows.length) throw new Error("no usable quota windows");
      current = { at, windows, stale: false };
    } catch {
      if (current) current.stale = true;
      // Do not log the CLI's stdout, stderr or credentials.
      console.error("AGY /usage did not return usable quota windows");
    }
  }

  async function refresh() {
    if (refreshing) return;
    refreshing = true;
    try {
      await Promise.allSettled([nativeSampler.refresh(), updateQuota()]);
    } finally {
      refreshing = false;
    }
  }

  function read() {
    const native = nativeSampler.read();
    const time = now();
    if (!current || time - current.at >= sampleLifetime) return native;
    const windows = current.windows.filter((window) => Date.parse(window.resets_at) > time.getTime());
    if (!windows.length) return native;
    const plan = native?.accounts?.[0]?.plan;
    return { accounts: [{
      id: "agy-local", provider: "agy", name: "AGY", ...(plan && { plan }),
      sampled_at: current.at.toISOString(), stale: current.stale, windows,
    }] };
  }

  return { refresh, read };
}
