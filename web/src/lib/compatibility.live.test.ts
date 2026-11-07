import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "./api";
import { compatibleSession, setCompatibilityFeatures } from "./compatibility";
import { isPersonaTool, parseUserText } from "./turns";
import { linkRoute } from "./links";

// Run only against the coordinator's disposable hub. Credentials are environment input, never fixtures or logs.
// PERSONA_COMPAT_HUB=http://127.0.0.1:<port> PERSONA_COMPAT_TOKEN=... npm exec vitest run src/lib/compatibility.live.test.ts
// Set PERSONA_COMPAT_MACHINE / SESSION to exercise message idempotency; the hub must have a disposable test runtime.
const hub = process.env.PERSONA_COMPAT_HUB;
const token = process.env.PERSONA_COMPAT_TOKEN;
const machine = process.env.PERSONA_COMPAT_MACHINE;
const session = process.env.PERSONA_COMPAT_SESSION;
const nativeFetch = globalThis.fetch;
afterEach(() => { vi.unstubAllGlobals(); setCompatibilityFeatures([]); });

describe.skipIf(!hub || !token)("real isolated hub compatibility probe", () => {
  it("uses the new web REST client against the baseline hub", async () => {
    const origin = new URL(hub!);
    expect(["127.0.0.1", "localhost", "[::1]"]).toContain(origin.hostname);
    vi.stubGlobal("fetch", (path: string, init: RequestInit) => nativeFetch(`${hub}${path}`, init));
    const health = await api<{ features?: string[] }>("/health", { token });
    setCompatibilityFeatures(health.features ?? []);
    const list = await api<{ sessions: Record<string, unknown>[] }>("/sessions", { token });
    expect(Array.isArray(list.sessions)).toBe(true);
    for (const row of list.sessions) {
      if (typeof row.started_by_jarvis === "boolean" && row.started_by_persona === undefined) {
        expect(compatibleSession(row).started_by_persona).toBe(row.started_by_jarvis);
      }
    }
    // Baseline transcripts retain their wire spelling; render rather than rewriting the underlying message.
    const message = '<jarvis-context kind="workspace" title="Project" url="jarvis://session/test-machine/test-session" machine="test-machine" cwd="/workspace">Notes</jarvis-context>\nProbe\n<jarvis-file kind="video" path="/workspace/demo.mp4" name="Demo" size="12"/>';
    expect(parseUserText(message).text).toBe("Probe");
    expect(linkRoute("jarvis://session/test-machine/test-session", false).kind).toBe("session");
    expect(isPersonaTool("mcp__jarvis__agent_read")).toBe(true);
    if (machine && session) {
      const id = crypto.randomUUID();
      const path = `/sessions/${encodeURIComponent(machine)}/${encodeURIComponent(session)}/messages`;
      const json = { text: '<persona-context kind="context" title="Compatibility probe" url="">Synthetic baseline context</persona-context>\nCompatibility probe', client_msg_id: id };
      const init = { method: "POST", json, token, headers: { "X-Persona-Op-Id": id } };
      const first = await api<Record<string, unknown>>(path, init);
      const repeat = await api<Record<string, unknown>>(path, init);
      expect(repeat).toEqual(first);
      const receipt = await api<{ state: string }>(`/ops/${id}`, { token });
      expect(receipt.state).toBe("done");
    }
    console.info(`Compatibility probe PASS: REST sessions=${list.sessions.length}; message replay=${Boolean(machine && session)}`);
  }, 60_000);
});
