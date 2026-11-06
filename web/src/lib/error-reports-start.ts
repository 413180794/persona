// Wires the page's error reporter (lib/error-reports.ts) to the hub: reports go to POST /api/v1/error-reports, and the
// hello's `features` say whether to send them at all.
import { api, ApiError } from "./api";
import { startErrorReports, type SendResult } from "./error-reports";
import type { ErrorReport } from "./extension-wire";
import { uuid } from "./format";
import { getState, subscribe } from "./store";

export async function sendErrorReports(reports: ErrorReport[]): Promise<SendResult> {
  try {
    const r = await api<{ enabled?: boolean }>("/error-reports", { method: "POST", json: { reports } });
    return { ok: true, enabled: r.enabled !== false };
  } catch (e) {
    return { ok: false, status: e instanceof ApiError ? e.status : 0 };
  }
}

startErrorReports({
  send: sendErrorReports,
  subscribe,
  features: () => {
    const s = getState();
    return { online: s.conn === "online", features: s.features };
  },
  uuid,
});
