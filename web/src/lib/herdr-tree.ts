// The sidebar's 按机器 tree, laid out the way herdr arranges agents: machine → workspace (a herdr space) → tab (a
// slot) → panes. A Session is one agent pane; the sessions of one slot are that tab's panes, which the chat shows as
// tabs of their own (PaneTabs). Hosts without tabs (Paseo, botmux, runtimes) list their sessions right under the
// space; sessions outside any space go to the machine's「其他」. Shell panes never reach the hub, so they are not here.
import { splitAgentSessions } from "./agent-groups";
import { stateOf, tabLabel } from "./format";
import { isSpacePinned, type AgentPins } from "./pins";
import type { Route } from "./router";
import type { AgentState, Machine, Session } from "./types";

// docs/agent-states.md「排序优先级」: what a folded row shows for everything under it, and which pane a tab opens on.
const URGENCY: AgentState[] = ["needs_you", "working", "unread", "idle", "unknown", "offline"];

const urgency = (s: Session) => URGENCY.indexOf(stateOf(s));

/** The most urgent of `list` (needs you > working > unread > seen); the first in order among equals. */
export function mostUrgent(list: readonly Session[]): Session | undefined {
  return list.reduce<Session | undefined>((best, s) => (!best || urgency(s) < urgency(best) ? s : best), undefined);
}

export function summaryState(list: readonly Session[]): AgentState | undefined {
  const s = mostUrgent(list);
  return s && stateOf(s);
}

// herdr numbers a tab's panes ("w9:p1", "w9:p2"); other hosts' ids ("botmux:<uuid>") carry no order and go last.
function paneNumber(s: Session): number {
  const m = s.pane_id?.match(/:p(\d+)$/) ?? s.pane_id?.match(/(\d+)$/);
  return m ? Number(m[1]) : Number.MAX_SAFE_INTEGER;
}

/** A tab's panes in their order: the pane's number in its id, then the session id. */
export function paneOrder(a: Session, b: Session): number {
  return paneNumber(a) - paneNumber(b) || a.session_id.localeCompare(b.session_id);
}

const sameSpace = (a: Session, b: Session) => a.machine_id === b.machine_id && (a.space_kind ?? "") === (b.space_kind ?? "") && a.space_id === b.space_id;

/** The agents in the same tab as `s` (same machine, space and slot), in pane order; just `[s]` without a slot. */
export function tabSessions(all: Session[], s: Session): Session[] {
  if (!s.slot_id || !s.space_id) return [s];
  const tab = all.filter((x) => !x.assistant && sameSpace(x, s) && x.slot_id === s.slot_id && (x.session_id === s.session_id || stateOf(x) !== "offline"));
  if (!tab.some((x) => x.session_id === s.session_id)) tab.push(s);
  return tab.sort(paneOrder);
}

/** Where a pane opens: its tab (`/t/…`) when it has one, else the session itself (`/s/…`). */
export function paneRoute(s: Session): Route {
  return s.space_id && s.slot_id
    ? { name: "tab", mid: s.machine_id, space: s.space_id, slot: s.slot_id, sid: s.session_id }
    : { name: "session", mid: s.machine_id, sid: s.session_id };
}

/** What `/t/<mid>/<space>/<slot>[/<sid>]` opens: the asked-for pane when the tab has it, else the session `sid` (the
 *  tab's ids changed with a herdr restart), else the tab's most urgent pane; undefined when none of them is there. */
export function resolveTab(all: Session[], r: { mid: string; space: string; slot: string; sid?: string }): Session | undefined {
  const mine = all.filter((s) => s.machine_id === r.mid && !s.assistant);
  const tab = mine.filter((s) => s.space_id === r.space && s.slot_id === r.slot).sort(paneOrder);
  const asked = r.sid ? mine.find((s) => s.session_id === r.sid) : undefined;
  if (asked && tab.includes(asked)) return asked;
  return asked ?? mostUrgent(tab.filter((s) => stateOf(s) !== "offline")) ?? tab[0];
}

/** The conversation a `/t/…` page shows: the tab's pane, or — only while the session list is not in yet — the session
 *  its link names. Once the list is in and neither is there, none (the page says the tab is gone). */
export function tabPagePane(tabPane: Session | undefined, r: { mid: string; sid?: string }, loaded: boolean): { mid: string; sid: string } | undefined {
  if (tabPane) return { mid: tabPane.machine_id, sid: tabPane.session_id };
  return !loaded && r.sid ? { mid: r.mid, sid: r.sid } : undefined;
}

export interface HerdrTab {
  /** Unique while the tab lives (it has the host's ids): React keys and the sidebar's highlight. */
  key: string;
  spaceId: string;
  slotId: string;
  /** `#N` or the tab's name (format.ts tabLabel); empty when the host gives neither. */
  label: string;
  sessions: Session[];
  /** The pane the row speaks for and opens on: the most urgent. */
  lead: Session;
  state: AgentState;
}

export interface HerdrSpace {
  key: string;
  machineId: string;
  name: string;
  /** Folds are kept by 机器/空间名, like pins: herdr's workspace ids change when it restarts. */
  foldKey: string;
  sessions: Session[];
  tabs: HerdrTab[];
  /** Sessions of a host without tabs, listed right under the space. */
  loose: Session[];
  state?: AgentState;
  pinned: boolean;
}

export interface HerdrMachine {
  key: string;
  machine: Machine;
  foldKey: string;
  spaces: HerdrSpace[];
  /** Sessions outside any space (「其他」), folded by `otherFoldKey`. */
  other: Session[];
  otherFoldKey: string;
  sessions: Session[];
  state?: AgentState;
  /** How many of its agents need you. */
  needs: number;
}

export interface MachineTreeOptions {
  pins?: AgentPins;
  /** Leave out machines with nothing under them (a quick filter is on). */
  hideEmpty?: boolean;
}

/** The live agents (`sessions` may hold any: assistants, offline ones and those of machines that are away are left
 *  out, as in splitAgentSessions) as machine → space → tab, every level in herdr's order; pinned spaces lead their
 *  machine. Machines keep the hub's order; an offline one is listed empty unless `hideEmpty`. */
export function buildMachineTree(machines: Machine[], sessions: Session[], opts: MachineTreeOptions = {}): HerdrMachine[] {
  const { live } = splitAgentSessions(sessions, machines);
  const byMachine = groupBy(live, (s) => s.machine_id);
  const out: HerdrMachine[] = [];
  for (const m of machines) {
    const mine = byMachine.get(m.id) ?? [];
    if (opts.hideEmpty && !mine.length) continue;
    const spaces = groupBy(mine.filter((s) => s.space_id), (s) => `${m.id}/${s.space_kind ?? ""}:${s.space_id}`);
    const built = [...spaces].map(([key, list]) => buildSpace(key, m.id, list, opts.pins));
    const index = (sp: HerdrSpace) => sp.sessions[0]!.space_index ?? Number.MAX_SAFE_INTEGER;
    built.sort((a, b) => Number(b.pinned) - Number(a.pinned) || index(a) - index(b) || a.name.localeCompare(b.name) || a.key.localeCompare(b.key));
    out.push({
      key: `machine:${m.id}`,
      machine: m,
      foldKey: `tree:${m.id}`,
      spaces: built,
      other: mine.filter((s) => !s.space_id).sort(paneOrder),
      otherFoldKey: `tree:${m.id}/其他`,
      sessions: mine,
      state: summaryState(mine),
      needs: mine.filter((s) => stateOf(s) === "needs_you").length,
    });
  }
  return out;
}

function groupBy<T>(list: T[], key: (x: T) => string): Map<string, T[]> {
  const out = new Map<string, T[]>();
  for (const x of list) {
    const k = key(x);
    const at = out.get(k);
    if (at) at.push(x);
    else out.set(k, [x]);
  }
  return out;
}

function buildSpace(key: string, machineId: string, list: Session[], pins?: AgentPins): HerdrSpace {
  const first = list[0]!;
  const name = first.space_name || first.space_id || "";
  const slots = groupBy(list.filter((s) => s.slot_id), (s) => s.slot_id!);
  const loose = list.filter((s) => !s.slot_id);
  const tabs: HerdrTab[] = [...slots].map(([slotId, panes]) => {
    panes.sort(paneOrder);
    const lead = mostUrgent(panes)!;
    return { key: `${key}/${slotId}`, spaceId: first.space_id!, slotId, label: tabLabel(panes[0]!), sessions: panes, lead, state: stateOf(lead) };
  });
  const slotIndex = (t: HerdrTab) => t.sessions[0]!.slot_index ?? Number.MAX_SAFE_INTEGER;
  tabs.sort((a, b) => slotIndex(a) - slotIndex(b) || a.slotId.localeCompare(b.slotId));
  // a space's sessions in the order the sidebar shows them: its tabs' panes, then the ones without a tab
  const sessions = [...tabs.flatMap((t) => t.sessions), ...loose.sort(paneOrder)];
  return {
    key,
    machineId,
    name,
    foldKey: `tree:${machineId}/${name}`,
    sessions,
    tabs,
    loose,
    state: summaryState(sessions),
    pinned: Boolean(pins && isSpacePinned(machineId, name, pins)),
  };
}
