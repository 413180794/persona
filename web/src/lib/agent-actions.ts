// What the Agents list's menus do (iOS AgentRowMenu / SpaceMenuItems): pin, mute, hand to Persona. Each change shows at
// once and is put back if the hub refuses it.
import { toast } from "../ui/atoms";
import { agentHandoffText } from "./agent-handoff";
import { api, errorText } from "./api";
import { askPersona } from "./outbox";
import { isPinned, isSpacePinned, spaceKey, togglePin, toggleSpacePin, toggled, type AgentPins } from "./pins";
import { isMuted, toggleMute } from "./session-actions";
import { getState, setMutes, setPins } from "./store";
import type { Session } from "./types";

async function putPins(next: AgentPins, done: string) {
  const before = getState().pins;
  setPins(next);
  try {
    await api("/pins", { method: "PUT", json: next });
    toast(done, "ok");
  } catch (e) {
    setPins(before);
    toast(`没改成：${errorText(e)}`, "error");
  }
}

export function flipPin(s: Session) {
  const pins = getState().pins;
  return putPins(togglePin(s, pins), isPinned(s, pins) ? "已取消置顶" : "已置顶");
}

export function flipSpacePin(machineId: string, spaceName: string) {
  const pins = getState().pins;
  return putPins(toggleSpacePin(machineId, spaceName, pins), isSpacePinned(machineId, spaceName, pins) ? "已取消置顶空间" : "已置顶空间");
}

async function putMutes(next: ReturnType<typeof getState>["mutes"], done: string) {
  const before = getState().mutes;
  setMutes(next);
  try {
    await api("/mutes", { method: "PUT", json: next });
    toast(done, "ok");
  } catch (e) {
    setMutes(before);
    toast(`没改成：${errorText(e)}`, "error");
  }
}

/** One agent's notifications: its own switch sits on top of its space's. */
export function flipMute(s: Session) {
  const next = toggleMute(s, getState().mutes);
  return putMutes(next, isMuted(s, next) ? "已屏蔽通知" : "已恢复通知");
}

/** A space's notifications (the agents turned back on inside it keep theirs). */
export function flipSpaceMute(machineId: string, spaceName: string) {
  const m = getState().mutes;
  const key = spaceKey(machineId, spaceName);
  return putMutes({ ...m, spaces: toggled(m.spaces, key) }, m.spaces.includes(key) ? "已恢复这个空间的通知" : "已屏蔽这个空间");
}

/** 交给 Persona: the agent goes to Persona's main conversation (a new one becomes it when there is none), and the page
 * moves there (the app's askPersona). */
export async function handToPersona(s: Session, machineName?: string) {
  try {
    await askPersona(agentHandoffText(s, machineName));
  } catch (e) {
    toast(`没发出去：${errorText(e)}`, "error");
  }
}

// Source compatibility for integrations written before the product rename.
export const handToJarvis = handToPersona;
