// What the shell shows on every page (restart banner, member banner, TLS, hub name), refreshed by whichever page last
// heard it.
import { useSyncExternalStore } from "react";
import { AdminError, getMesh } from "./api";
import type { MeshView, Restart, TLSInfo } from "./types";

interface Shared {
  restart: Restart;
  canRestart: boolean;
  tls: TLSInfo | null | undefined; // undefined: not known yet
  hubName: string;
  startedAt: string;
  hubId: string;
  mesh: MeshView | null | undefined; // undefined: not known yet; null: this hub has no group API (older hub)
}

let shared: Shared = { restart: { required: false, reasons: [] }, canRestart: false, tls: undefined, hubName: "", startedAt: "", hubId: "", mesh: undefined };
const listeners = new Set<() => void>();

export function share(patch: Partial<Shared>) {
  shared = { ...shared, ...patch };
  listeners.forEach((l) => l());
}

export function useShared(): Shared {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => shared,
  );
}

// refreshMesh asks the hub which role it has in its group. A hub without the group API (404 / 501) has none.
export function refreshMesh(): Promise<void> {
  return getMesh().then(
    (mesh) => share({ mesh }),
    (e) => {
      if (e instanceof AdminError && (e.status === 404 || e.status === 501)) share({ mesh: null });
    },
  );
}
