// "Remove from list" for personal agents: the row leaves at once, the toast offers Undo, a refusal puts it back.
import { describe, expect, it, vi } from "vitest";
import { normalizePrefs, removeAgentSession, restoreAgentSession, emptyPrefs, type AgentSession, type HideDeps } from "./agents";

const row: AgentSession = { id: "gw:a", source: "gw", sessionKey: "a", provider: "muse", title: "Weekly notes", canSend: true, unreadCount: 2, rankTimeMs: 1 };

function deps(over: Partial<HideDeps> = {}) {
  const notes: { text: string; tone: string; action?: { label: string; run: () => void } }[] = [];
  const d: HideDeps = {
    hide: vi.fn().mockResolvedValue({ ok: true }),
    unhide: vi.fn().mockResolvedValue({ ok: true }),
    refresh: vi.fn().mockResolvedValue(undefined),
    notify: (text, tone, action) => void notes.push({ text, tone, action }),
    ...over,
  };
  return { d, notes };
}

describe("removing a personal agent from the list", () => {
  it("asks the hub, re-pulls the list and offers Undo, which restores it", async () => {
    const { d, notes } = deps();
    expect(await removeAgentSession(row, d)).toBe(true);
    expect(d.hide).toHaveBeenCalledWith("gw:a");
    expect(d.refresh).toHaveBeenCalled();
    expect(notes[0].tone).toBe("ok");
    expect(notes[0].text).toContain("Weekly notes");
    expect(notes[0].action?.label).toBeTruthy();
    notes[0].action!.run();
    await vi.waitFor(() => expect(d.unhide).toHaveBeenCalledWith("gw:a"));
    await vi.waitFor(() => expect(notes[1]?.tone).toBe("ok"));
  });

  it("says why when the hub refuses, and still re-pulls so the row comes back", async () => {
    const { d, notes } = deps({ hide: vi.fn().mockRejectedValue(new Error("no session")) });
    expect(await removeAgentSession(row, d)).toBe(false);
    expect(notes).toHaveLength(1);
    expect(notes[0].tone).toBe("error");
    expect(notes[0].text).toContain("no session");
    expect(d.refresh).toHaveBeenCalled();
  });

  it("restores by hand, and reports a failed restore", async () => {
    const ok = deps();
    expect(await restoreAgentSession(row, ok.d)).toBe(true);
    expect(ok.notes[0].tone).toBe("ok");
    const bad = deps({ unhide: vi.fn().mockRejectedValue(new Error("offline")) });
    expect(await restoreAgentSession(row, bad.d)).toBe(false);
    expect(bad.notes[0].tone).toBe("error");
  });
});

describe("hidden in the shared prefs", () => {
  it("keeps what the hub sends and leaves it out when absent", () => {
    expect(normalizePrefs({ ...emptyPrefs(), hidden: { "gw:a": 5 } }).hidden).toEqual({ "gw:a": 5 });
    expect("hidden" in normalizePrefs(emptyPrefs())).toBe(false);
  });
});
