import { api, ApiError, enc, errorText, sessionPath } from "./api";
import { parseUserText } from "./turns";
import { imagePayload, quotePath, releaseAttachments, type LocalAttachment } from "./attachments";
import { uuid } from "./format";
import { href, navigate } from "./router";
import { getState, putLaunch, upsertSession } from "./store";
import type { Session } from "./types";

export interface AgentLaunch {
  id: string;
  machineId: string;
  cwd: string;
  launcher: string;
  text: string;
  attachments: LocalAttachment[];
  phase: "creating" | "uploading" | "sending" | "failed" | "ready";
  session?: Session;
  outgoing?: { text: string; images: { mime: string; data: string }[] };
  uploaded: Record<string, string>;
  error?: string;
  uncertain?: boolean;
  retryNeedsNewId?: boolean;
  messageId?: string;
  clearable?: string;
  draft?: string;
}

export function launchAgent(input: Pick<AgentLaunch, "machineId" | "cwd" | "launcher" | "text" | "attachments">) {
  const launch: AgentLaunch = { ...input, id: `local-${uuid()}`, phase: "creating", uploaded: {} };
  putLaunch(launch);
  navigate({ name: "session", mid: launch.machineId, sid: launch.id });
  // Let the local conversation paint before serialization, uploads and remote startup.
  setTimeout(() => void deliverLaunch(launch.id), 0);
  return launch.id;
}

export function acknowledgeLaunch(mid: string, sid: string, user: { text: string; images: unknown[] }) {
  for (const launch of Object.values(getState().launches)) {
    if (launch.phase !== "ready" || launch.machineId !== mid || launch.session?.session_id !== sid || !launch.outgoing) continue;
    if (parseUserText(launch.outgoing.text).text === user.text && user.images.length >= launch.outgoing.images.length) putLaunch({ ...launch, outgoing: undefined });
  }
}

const delivering = new Set<string>();

export async function deliverLaunch(id: string, clearDraft?: string) {
  let launch: AgentLaunch = getState().launches[id];
  if (!launch || delivering.has(id) || (launch.phase !== "failed" && launch.phase !== "creating")) return;
  delivering.add(id);
  const controller = new AbortController();
  const deadline = setTimeout(() => controller.abort(), 240_000);
  const update = (patch: Partial<AgentLaunch>) => {
    launch = { ...launch, ...patch };
    if (getState().launches[id]) putLaunch(launch);
  };
  update({ phase: launch.session ? "uploading" : "creating", error: undefined, uncertain: false, clearable: undefined, draft: undefined });
  try {
    const imageBytes = launch.attachments.filter((a) => a.mime.startsWith("image/")).reduce((total, a) => total + Math.ceil(a.file.size / 3) * 4, 0);
    if (imageBytes + new TextEncoder().encode(launch.text).byteLength > 31 * 1024 * 1024) throw new Error("附件总大小超过消息上限，请减少图片后再启动。");
    if (!launch.session) {
      let session: Session;
      try {
        const r = await api<{ session: Session }>("/sessions", { method: "POST", signal: controller.signal, json: { machine_id: launch.machineId, cwd: launch.cwd, launcher: launch.launcher || undefined, async: true, name_hint: launch.text || undefined } });
        session = r.session;
      } catch (e) {
        if (!(e instanceof ApiError) || e.code !== "codex_needs_prompt") throw e;
        if (launch.attachments.length) throw new Error("这个启动器暂不支持首条消息带附件，请选择其他启动器；也可以先用文字启动，再在会话里发送附件。");
        if (!launch.text) throw new Error("这个启动器需要第一条消息，请填写后再启动。");
        const r = await api<{ session: Session }>("/sessions", { method: "POST", signal: controller.signal, json: { machine_id: launch.machineId, cwd: launch.cwd, launcher: launch.launcher || undefined, prompt: launch.text } });
        session = r.session;
        update({ outgoing: { text: "", images: [] } });
      }
      if (!getState().launches[id]) return;
      update({ session });
      upsertSession(session);
    }
    if (!launch.outgoing) {
      update({ phase: "uploading" });
      const paths: string[] = [];
      const images: { mime: string; data: string }[] = [];
      for (const att of launch.attachments) {
        if (att.mime.startsWith("image/")) {
          images.push(await imagePayload(att.file));
        } else {
          let path = launch.uploaded[att.id];
          if (!path) {
            const r = await api<{ path: string }>(`/machines/${enc(launch.machineId)}/files?name=${enc(att.name)}`, { method: "POST", signal: controller.signal, body: att.file, headers: { "Content-Type": "application/octet-stream" } });
            path = r.path;
            update({ uploaded: { ...launch.uploaded, [att.id]: path } });
          }
          paths.push(quotePath(path));
        }
      }
      update({ outgoing: { text: [launch.text, ...paths].filter(Boolean).join("\n"), images } });
    }
    if (!getState().launches[id]) return;
    controller.signal.throwIfAborted();
    const out = launch.outgoing!;
    if (out.text || out.images.length) {
      update({ phase: "sending" });
      const payload = { ...out, client_msg_id: launch.messageId ?? launch.id, clear_draft: clearDraft };
      if (new TextEncoder().encode(JSON.stringify(payload)).byteLength > 32 * 1024 * 1024) throw new Error("附件总大小超过消息上限，请减少图片后重试。");
      await api(`${sessionPath(launch.machineId, launch.session!.session_id)}/messages`, {
        method: "POST", signal: controller.signal, json: payload,
      });
    }
    update({ phase: "ready" });
    releaseAttachments(launch.attachments);
    update({ attachments: [] });
    const local = { name: "session" as const, mid: launch.machineId, sid: launch.id };
    if (location.pathname === href(local)) navigate({ ...local, sid: launch.session!.session_id }, true);
  } catch (e) {
    const phase: AgentLaunch["phase"] = getState().launches[id]?.phase ?? "failed";
    const sending = phase === "sending";
    const creating = phase === "creating";
    const uncertain = (creating || sending) && (e instanceof TypeError || (e instanceof DOMException && e.name === "AbortError") || (e instanceof ApiError && (e.body.delivered === "unknown" || (e.status >= 500 && e.body.delivered !== "none"))));
    update({ phase: "failed", uncertain, retryNeedsNewId: e instanceof ApiError && e.body.delivered === "unknown",
      error: `${creating ? "没启动起来" : sending ? "第一条消息没送达" : "附件处理失败"}：${errorText(e)}`,
      clearable: e instanceof ApiError && e.code === "input_draft" && typeof e.body.clearable === "string" ? e.body.clearable : undefined,
      draft: e instanceof ApiError && e.code === "input_draft" ? String(e.body.draft ?? "") : undefined,
    });
  } finally {
    clearTimeout(deadline);
    delivering.delete(id);
  }
}
