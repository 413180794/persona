// Settings › Devices' two lists, as on iOS (DeviceCredentials.swift): GET /auth/tokens returns the revoked credentials too,
// with revoked_at. The ones in use come first (this browser, then by last use); the revoked ones go under "Revoked", the
// latest first, and are not counted as devices or offered "Revoke" again (#1764).
import { byTime } from "./times";

export interface DeviceTimes {
  id: string;
  last_seen?: string;
  revoked_at?: string;
}

export function splitDevices<T extends DeviceTimes>(tokens: T[], current?: string): { active: T[]; revoked: T[] } {
  const active = tokens
    .filter((t) => !t.revoked_at)
    .sort((a, b) => (a.id === current ? -1 : b.id === current ? 1 : byTime(b.last_seen, a.last_seen)));
  const revoked = tokens.filter((t) => t.revoked_at).sort((a, b) => byTime(b.revoked_at, a.revoked_at));
  return { active, revoked };
}
