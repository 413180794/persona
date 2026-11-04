// 消息 (docs/lark-connector-migration.md「入口」, the narrow exception of #392): every Feishu private and group chat in one
// list, shown only when this browser turned on 设置 › 连接器 › 飞书 › 首页显示消息 (off by default, kept per browser like
// the app keeps it per device). The list reads the hub's account-bound snapshots (`account_bound=1`): conversations a
// page at a time and the pinned conversations on top. A chat opens in the inbox's Feishu chat view (LarkThread).
// Nothing is written to disk: the last list lives in memory for this tab only.
import { useSyncExternalStore } from "react";
import { api, ApiError, enc, errorText } from "./api";

// ── 首页显示消息 ─────────────────────────────────────────

const HOME_KEY = "lark.homeMessages";
const homeListeners = new Set<() => void>();

export function getLarkHome(): boolean {
  try {
    return localStorage.getItem(HOME_KEY) === "1";
  } catch {
    return false;
  }
}

export function setLarkHome(on: boolean) {
  if (on) localStorage.setItem(HOME_KEY, "1");
  else localStorage.removeItem(HOME_KEY);
  homeListeners.forEach((l) => l());
}

if (typeof window !== "undefined") {
  window.addEventListener("storage", (e) => {
    if (e.key === HOME_KEY || e.key === null) homeListeners.forEach((l) => l());
  });
}

const subscribeHome = (l: () => void) => {
  homeListeners.add(l);
  return () => {
    homeListeners.delete(l);
  };
};

/** Whether this browser shows the 消息 page (off unless turned on). */
export function useLarkHome(): boolean {
  return useSyncExternalStore(subscribeHome, getLarkHome, () => false);
}

// ── the conversations ────────────────────────────────────

/** GET /api/v1/lark/conversations: one chat (server/internal/lark/feed.go Conversation). */
export interface LarkConversation {
  id: string;
  name: string;
  avatar?: string;
  is_group: boolean;
  is_topic?: boolean;
  unread: number;
  marked_unread?: boolean;
  at_me: boolean;
  at_all?: boolean;
  muted: boolean;
  pinned?: boolean;
  tags?: string[];
  preview: string;
  preview_sender?: string;
  is_mine: boolean;
  updated_at: string;
  read_position?: string;
  mention_position?: string;
}

export interface LarkConversationPage {
  account_id?: string;
  conversations: LarkConversation[];
  next_cursor?: string;
}

export const CONVERSATION_PAGE = 60;

/** One copy per id, in the order first seen (a later page never moves a row up). */
export function mergeConversations(have: readonly LarkConversation[], batch: readonly LarkConversation[]): LarkConversation[] {
  const seen = new Set<string>();
  const out: LarkConversation[] = [];
  for (const c of [...have, ...batch]) {
    if (!c?.id || seen.has(c.id)) continue;
    seen.add(c.id);
    out.push(c);
  }
  return out;
}

/** The search covers what is loaded: chat names, and the last message (and its sender). */
export function filterConversations(list: readonly LarkConversation[], query: string): LarkConversation[] {
  const q = query.trim().toLowerCase();
  if (!q) return [...list];
  return list.filter((c) => [c.name, c.preview, c.preview_sender].some((s) => s?.toLowerCase().includes(q)));
}

/** The second line of a row: who wrote the last message in a group, or 我 for mine. */
export function conversationPreview(c: Pick<LarkConversation, "preview" | "preview_sender" | "is_mine" | "is_group">): string {
  const text = c.preview?.trim() ?? "";
  if (!text) return "";
  if (c.is_mine) return `我：${text}`;
  if (c.is_group && c.preview_sender) return `${c.preview_sender}：${text}`;
  return text;
}

/** The badge of a row: unread count, or a dot for 标为未读 without a count. */
export function unreadBadge(c: Pick<LarkConversation, "unread" | "marked_unread" | "muted">): { count?: number; dot?: boolean; muted: boolean } {
  if (c.unread > 0) return { count: c.unread, muted: c.muted };
  if (c.marked_unread) return { dot: true, muted: c.muted };
  return { muted: c.muted };
}

/** Pinned chats on top, then the rest without repeating them. */
export function arrangeConversations(pinned: readonly LarkConversation[], rows: readonly LarkConversation[]): { pinned: LarkConversation[]; rest: LarkConversation[] } {
  const top = mergeConversations([], pinned);
  const ids = new Set(top.map((c) => c.id));
  return { pinned: top, rest: rows.filter((c) => !ids.has(c.id)) };
}

/** What a failed read means for the page. */
export type LarkListProblem = { kind: "not_logged_in" | "not_configured" | "other"; text: string };

export function listProblem(e: unknown): LarkListProblem {
  if (e instanceof ApiError) {
    if (e.code === "lark_not_logged_in") return { kind: "not_logged_in", text: "hub 连接的 lark-connector 没有登录飞书。在 hub 所在的机器上登录后再试。" };
    if (e.status === 404 && !e.code) return { kind: "not_configured", text: "hub 还没接飞书：在 hub 所在的机器上运行 lark-connector，并在 hub.json 的 lark 里填上它。" };
  }
  return { kind: "other", text: `读不到飞书会话：${errorText(e)}` };
}

// ── the store ────────────────────────────────────────────

export interface LarkChatsState {
  account?: string;
  rows: LarkConversation[];
  pinned: LarkConversation[];
  cursor?: string;
  /** How many pages the list shows: a refresh reads that many again. */
  pages: number;
  loaded: boolean;
  loading: boolean;
  loadingMore: boolean;
  problem?: LarkListProblem;
  moreProblem?: string;
}

export interface LarkChatsDeps {
  page: (cursor: string | undefined, account: string | undefined) => Promise<LarkConversationPage>;
  pinned: (account: string | undefined) => Promise<LarkConversationPage>;
}

export const liveChatsDeps: LarkChatsDeps = {
  page: (cursor, account) =>
    api<LarkConversationPage>(`/lark/conversations?scope=chats&limit=${CONVERSATION_PAGE}&account_bound=1${cursor ? `&cursor=${enc(cursor)}` : ""}${account ? `&account_id=${enc(account)}` : ""}`),
  pinned: (account) => api<LarkConversationPage>(`/lark/pinned?account_bound=1${account ? `&account_id=${enc(account)}` : ""}`),
};

const isAccountChanged = (e: unknown) => e instanceof ApiError && e.code === "lark_account_changed";

/** The list's state and its reads. Refreshes read the loaded window again and replace it whole only when every page
 * came back (a chat that left the list goes then); a failed one keeps what is shown. Another account's rows are
 * dropped the moment the hub says the account changed, and a late reply for the old account is not merged in. */
export class LarkChatsStore {
  private s: LarkChatsState = { rows: [], pinned: [], pages: 1, loaded: false, loading: false, loadingMore: false };
  private listeners = new Set<() => void>();
  private revision = 0;
  private again = false;
  constructor(private deps: LarkChatsDeps = liveChatsDeps) {}

  get state(): LarkChatsState {
    return this.s;
  }
  subscribe = (l: () => void) => {
    this.listeners.add(l);
    return () => {
      this.listeners.delete(l);
    };
  };
  private set(patch: Partial<LarkChatsState>) {
    this.s = { ...this.s, ...patch };
    this.listeners.forEach((l) => l());
  }

  /** Forget another account's rows at once. */
  private dropAccount() {
    this.revision += 1;
    this.set({ account: undefined, rows: [], pinned: [], cursor: undefined, pages: 1, loaded: false });
  }

  async refresh(): Promise<void> {
    if (this.s.loading || this.s.loadingMore) {
      this.again = true;
      return;
    }
    let retried = false;
    do {
      this.again = false;
      const request = ++this.revision;
      this.set({ loading: true });
      try {
        let account = this.s.account;
        let rows: LarkConversation[] = [];
        let cursor: string | undefined;
        const seen = new Set<string>();
        let read = 0;
        do {
          const p = await this.deps.page(cursor, account);
          if (account && p.account_id && p.account_id !== account) throw new ApiError(409, "飞书账号已切换", { code: "lark_account_changed" });
          account = p.account_id || account;
          rows = mergeConversations(rows, p.conversations ?? []);
          read += 1;
          cursor = p.next_cursor || undefined;
          if (cursor && seen.has(cursor)) cursor = undefined; // a repeated cursor ends the list instead of looping
          if (cursor) seen.add(cursor);
        } while (cursor && read < this.s.pages);
        let pinned = this.s.pinned;
        try {
          const p = await this.deps.pinned(account);
          if (!(account && p.account_id && p.account_id !== account)) pinned = p.conversations ?? [];
        } catch (e) {
          if (isAccountChanged(e)) throw e;
          // the shelf keeps its last copy; the list itself came back
        }
        if (request !== this.revision) {
          this.set({ loading: false });
          continue;
        }
        this.set({ account, rows, pinned, cursor, pages: Math.max(read, 1), loaded: true, loading: false, problem: undefined, moreProblem: undefined });
      } catch (e) {
        if (request !== this.revision) {
          this.set({ loading: false });
          continue;
        }
        if (isAccountChanged(e) && !retried) {
          retried = true;
          this.dropAccount();
          this.set({ loading: false });
          this.again = true;
          continue;
        }
        const problem = listProblem(e);
        if (problem.kind === "not_logged_in") this.dropAccount();
        this.set({ loading: false, loaded: true, problem });
      }
    } while (this.again);
  }

  async more(): Promise<void> {
    const { cursor, account } = this.s;
    if (!cursor || this.s.loading || this.s.loadingMore) return;
    const request = this.revision;
    this.set({ loadingMore: true, moreProblem: undefined });
    try {
      const p = await this.deps.page(cursor, account);
      if (request !== this.revision) {
        this.set({ loadingMore: false });
        return;
      }
      if (account && p.account_id && p.account_id !== account) throw new ApiError(409, "飞书账号已切换", { code: "lark_account_changed" });
      const next = p.next_cursor && p.next_cursor !== cursor ? p.next_cursor : undefined;
      this.set({ rows: mergeConversations(this.s.rows, p.conversations ?? []), cursor: next, pages: this.s.pages + 1, loadingMore: false });
    } catch (e) {
      this.set({ loadingMore: false });
      if (request !== this.revision) return;
      if (isAccountChanged(e)) {
        this.dropAccount();
        await this.refresh();
        return;
      }
      this.set({ moreProblem: errorText(e) });
    }
    if (this.again) await this.refresh();
  }
}

/** The tab's one list: opening 消息 again shows the last copy at once, then reads it again. */
export const larkChats = new LarkChatsStore();

export function useLarkChats(): LarkChatsState {
  return useSyncExternalStore(larkChats.subscribe, () => larkChats.state);
}

/** The signed-in Feishu account as the hub last said (an add-Pin write carries it). */
export async function larkAccount(): Promise<string> {
  if (larkChats.state.account) return larkChats.state.account;
  const p = await api<LarkConversationPage>("/lark/conversations?scope=chats&limit=1&account_bound=1");
  if (!p.account_id) throw new ApiError(426, "升级 Hub 后可使用 Pin 消息。请在设置中打开 Hub，检查更新。", { code: "lark_hub_upgrade_required" });
  return p.account_id;
}
