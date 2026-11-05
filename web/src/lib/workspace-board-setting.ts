// 设置 › 通用 › Agent › 工作流看板 (#2475): the board is off unless turned on. The switch is the hub's
// (GET/PUT /workspace-board/settings), so every browser agrees; a hub that has never stored it answers whether the board
// was already in use. Off only hides the board: its columns and cards stay on the hub.
import { useSyncExternalStore } from "react";
import { api, ApiError } from "./api";
import { onHubEvent } from "./store";

export interface WorkspaceBoardSetting {
  /** null until the hub answered: nothing shows the board (or says it is off) before that. */
  enabled: boolean | null;
  canWrite: boolean;
}

let state: WorkspaceBoardSetting = { enabled: null, canWrite: false };
let loading: Promise<void> | null = null;
const listeners = new Set<() => void>();

function set(next: WorkspaceBoardSetting) {
  if (next.enabled === state.enabled && next.canWrite === state.canWrite) return;
  state = next;
  listeners.forEach((l) => l());
}

export function getWorkspaceBoardSetting(): WorkspaceBoardSetting {
  return state;
}

/** Reads the switch again (on every hello: another hub, or one that came back, may say otherwise). */
export function refreshWorkspaceBoardSetting(): Promise<void> {
  loading ??= api<{ enabled?: boolean; can_write?: boolean }>("/workspace-board/settings")
    .then((r) => set({ enabled: r.enabled === true, canWrite: r.can_write === true }))
    .catch((e) => {
      // a hub from before the switch showed the board to everyone and has nothing to turn it off with
      if (e instanceof ApiError && e.status === 404) set({ enabled: true, canWrite: false });
      // anything else: keep what we had, the next hello asks again
    })
    .finally(() => {
      loading = null;
    });
  return loading;
}

/** Turns the board on or off for this hub; shown at once, put back (and thrown) when the hub refuses. */
export async function setWorkspaceBoardEnabled(on: boolean): Promise<void> {
  const before = state;
  set({ ...state, enabled: on });
  try {
    const r = await api<{ enabled?: boolean; can_write?: boolean }>("/workspace-board/settings", { method: "PUT", json: { enabled: on } });
    set({ enabled: r.enabled === true, canWrite: r.can_write !== false });
  } catch (e) {
    set(before);
    throw e;
  }
}

onHubEvent("hello", () => void refreshWorkspaceBoardSetting());
// another browser or device changed it
onHubEvent("workspace_board_settings", (e) => {
  if (typeof e.enabled === "boolean") set({ ...state, enabled: e.enabled });
});

const subscribe = (l: () => void) => {
  listeners.add(l);
  if (state.enabled === null) void refreshWorkspaceBoardSetting();
  return () => {
    listeners.delete(l);
  };
};

export function useWorkspaceBoardSetting(): WorkspaceBoardSetting {
  return useSyncExternalStore(subscribe, getWorkspaceBoardSetting, getWorkspaceBoardSetting);
}

/** Only for tests: back to "not answered yet". */
export function resetWorkspaceBoardSettingForTest() {
  state = { enabled: null, canWrite: false };
  loading = null;
}
