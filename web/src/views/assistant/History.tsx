// 全部对话 (docs/assistant.md「管理自己的对话」, the iOS app's sidebar + ArchivedConversationsView): every conversation
// Persona has, the main one first, the rest newest first by day; search on the title and the last message; a row's menu
// renames it, copies its id, archives it (the main one can't be). The archived ones are folded at the end.
import { useCallback, useEffect, useMemo, useState } from "react";
import { api, enc, errorText } from "../../lib/api";
import { ago, stateOf } from "../../lib/format";
import { href, navigate, onLinkClick, type Route } from "../../lib/router";
import { refreshAssistant, useStore } from "../../lib/store";
import type { AssistantInfo, Session } from "../../lib/types";
import { Button, Collapse, copyText, cx, Dialog, IconButton, Loading, Menu, StatePill, toast, useMenu, useNow, type MenuItem } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { arrange, conversationTitle, filterConversations, mergeConversations, splitTitle, type Conversation } from "./conversations";
import { Empty, inputClass, SectionTitle } from "./parts";

const NONE: Session[] = [];

export function History({ onToggleSidebar }: { onToggleSidebar: () => void }) {
  const assistant = useStore((s) => s.assistant);
  const mid = assistant?.machine_id;
  const live = useStore((s) => (mid ? s.sessions[mid] ?? NONE : NONE));
  const [fetched, setFetched] = useState<Conversation[] | null>(null);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [showArchived, setShowArchived] = useState(false);
  const [renaming, setRenaming] = useState<Conversation | null>(null);
  /** Titles just given here: the sessions pushed by the hub may still carry the old one for a while. */
  const [titles, setTitles] = useState<Record<string, string>>({});
  const nowMs = useNow(60_000);

  const load = useCallback(async () => {
    try {
      const a = await api<AssistantInfo & { conversations: Conversation[] }>("/assistant?archived=1");
      setFetched(a.conversations ?? []);
      setError("");
    } catch (e) {
      setError(errorText(e));
    }
  }, []);
  useEffect(() => {
    void load();
  }, [load]);

  const all = useMemo(
    () => mergeConversations(fetched ?? [], assistant?.conversations ?? [], live.filter((s) => s.assistant)).map((s) => (titles[s.session_id] ? { ...s, title: titles[s.session_id] } : s)),
    [fetched, assistant?.conversations, live, titles],
  );
  const hits = useMemo(() => filterConversations(all, query), [all, query]);
  const { main, days, archived } = useMemo(() => arrange(hits, assistant?.main, new Date(nowMs)), [hits, assistant?.main, nowMs]);
  const total = all.filter((s) => !s.archived).length;
  const searching = query.trim() !== "";

  const after = async () => {
    await Promise.allSettled([refreshAssistant(), load()]);
  };

  const archive = async (s: Conversation, on: boolean) => {
    // shown at once; what the hub lists next is what stays
    setFetched((list) => (list ?? []).map((x) => (x.session_id === s.session_id ? { ...x, archived: on } : x)));
    try {
      await api(`/assistant/conversations/${enc(s.session_id)}/${on ? "archive" : "unarchive"}`, { method: "POST", json: {} });
      toast(on ? `已归档「${conversationTitle(s)}」` : `已取消归档「${conversationTitle(s)}」`, "ok");
    } catch (e) {
      toast(errorText(e), "error");
    }
    await after();
  };

  const loading = fetched === null && !error;

  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line px-3 sm:px-4">
        <IconButton icon="sidebar" label="侧边栏" onClick={onToggleSidebar} className="lg:hidden" />
        <h1 className="min-w-0 flex-1 truncate text-[14px] font-semibold text-ink">
          Persona 的对话
          {total > 0 && <span className="ml-2 text-[12px] font-normal text-ink-3 tabular-nums">{total}</span>}
        </h1>
        <IconButton icon="retry" label="刷新" onClick={() => void after()} />
        <Button tone="primary" icon="plus" onClick={() => navigate({ name: "persona-new" })}>新对话</Button>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto max-w-[760px] px-4 py-5 sm:px-8 sm:py-6">
          <label className="mb-5 flex h-9 items-center gap-2 rounded-control bg-surface px-3 shadow-btn focus-within:shadow-[inset_0_0_0_1px_var(--accent)]">
            <Icon name="search" size={13} stroke={2} className="text-ink-3" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => e.key === "Escape" && setQuery("")}
              placeholder="搜索标题或最后一句"
              aria-label="搜索对话"
              className="min-w-0 flex-1 bg-transparent text-[13px] text-ink outline-none placeholder:text-ink-3"
            />
            {query && <IconButton icon="x" label="清除搜索" size={22} iconSize={12} onClick={() => setQuery("")} />}
          </label>

          {loading && (
            <div className="flex justify-center py-16">
              <Loading />
            </div>
          )}
          {error && fetched === null && (
            <Empty icon="info" title="读不到对话">
              {error}
            </Empty>
          )}
          {!loading && !error && all.length === 0 && (
            <Empty icon="chat" title="还没有对话">
              在 Persona 里说句话，对话就会出现在这里。
            </Empty>
          )}
          {!loading && searching && hits.length === 0 && all.length > 0 && <Empty icon="search" title={`没有和「${query.trim()}」相关的对话`} />}

          {main && (
            <section className="mb-5">
              <div className="overflow-hidden rounded-card bg-surface shadow-card">
                <ConvRow s={main} main now={nowMs} onRename={setRenaming} onArchive={archive} />
              </div>
            </section>
          )}

          {days.map((d) => (
            <section key={d.title} className="mb-5">
              <SectionTitle>{d.title}</SectionTitle>
              <div className="divide-y divide-line overflow-hidden rounded-card bg-surface shadow-card">
                {d.items.map((s) => (
                  <ConvRow key={s.session_id} s={s} now={nowMs} onRename={setRenaming} onArchive={archive} />
                ))}
              </div>
            </section>
          ))}

          {archived.length > 0 && (
            <section className="mb-5">
              <button
                type="button"
                aria-expanded={showArchived || searching}
                onClick={() => setShowArchived((v) => !v)}
                className="mb-1.5 flex items-center gap-1.5 whitespace-nowrap rounded-[6px] px-1 text-[12px] font-medium text-ink-3 transition-colors hover:text-ink-2"
              >
                <Icon name="archive" size={13} />
                已归档（{archived.length}）
                <Icon name="chevron" size={12} stroke={2.2} className={cx("transition-transform duration-200", !(showArchived || searching) && "-rotate-90")} />
              </button>
              <Collapse open={showArchived || searching}>
                {/* folded: out of the tab order and the accessibility tree */}
                <div inert={!(showArchived || searching)}>
                <div className="divide-y divide-line overflow-hidden rounded-card bg-surface shadow-card">
                  {archived.map((s) => (
                    <ConvRow key={s.session_id} s={s} now={nowMs} onRename={setRenaming} onArchive={archive} />
                  ))}
                </div>
                <p className="mt-1.5 px-1 text-[11.5px] leading-relaxed text-ink-3">归档只是不在列表里显示，对话还在。在归档的对话里再发消息，它会自动回到列表。</p>
                </div>
              </Collapse>
            </section>
          )}
        </div>
      </div>

      {renaming && (
        <RenameDialog
          s={renaming}
          onClose={() => setRenaming(null)}
          onDone={(title) => {
            setTitles((t) => ({ ...t, [renaming.session_id]: title }));
            void after();
          }}
        />
      )}
    </div>
  );
}

const MARK_TONES = ["bg-accent-tint text-accent-ink", "bg-orange-tint text-orange", "bg-green-tint text-green"];

/** A soft color picked by the id, so neighbours differ (the iOS app's ConversationMark). */
function markTone(id: string): string {
  let h = 0;
  for (const c of id) h = (h * 31 + c.charCodeAt(0)) & 0xffff;
  return MARK_TONES[h % MARK_TONES.length];
}

function plain(text?: string): string {
  return (text ?? "").replace(/[`*_#>|]/g, "").replace(/\s+/g, " ").trim();
}

function ConvRow({ s, main, now, onRename, onArchive }: { s: Conversation; main?: boolean; now: number; onRename: (s: Conversation) => void; onArchive: (s: Conversation, on: boolean) => void }) {
  const menu = useMenu();
  const route: Route = { name: "jarvis", sid: s.session_id };
  const title = conversationTitle(s);
  const split = splitTitle(title);
  const st = stateOf(s);
  const flagged = st === "needs_you" || st === "working" || st === "unread";
  const last = plain(s.last_text);
  const items: MenuItem[] = [
    ...(!main ? [{ key: "rename", label: "重命名…", icon: "pencil" as const, onSelect: () => onRename(s) }] : []),
    { key: "copy", label: "拷贝会话 ID", icon: "copy", onSelect: () => void copyText(s.session_id) },
    ...(!main
      ? [s.archived
          ? { key: "unarchive", label: "取消归档", icon: "archive" as const, onSelect: () => onArchive(s, false) }
          : { key: "archive", label: "归档", icon: "archive" as const, onSelect: () => onArchive(s, true) }]
      : []),
  ];
  return (
    <div className={cx("group relative flex items-center gap-1 pr-2 transition-colors hover:bg-hover", menu.anchor && "bg-hover")}>
      <a
        href={href(route)}
        onClick={(e) => onLinkClick(e, route)}
        title={[title, last].filter(Boolean).join("\n")}
        className="flex min-w-0 flex-1 items-center gap-3 py-2.5 pl-4 focus-visible:outline-none"
      >
        <span className={cx("flex size-8 shrink-0 items-center justify-center rounded-full", main ? "bg-ink text-surface" : markTone(s.session_id))}>
          <Icon name={main ? "spark" : "chat"} size={main ? 13 : 14} />
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex min-w-0 items-center gap-2">
            <span className={cx("truncate text-[13.5px]", st === "unread" ? "font-semibold text-ink" : "font-medium text-ink")}>
              {split ? (
                <>
                  <span className="font-normal text-ink-3">{split.head}</span>
                  {split.topic}
                </>
              ) : (
                title
              )}
            </span>
            {main && <span className="inline-flex h-5 shrink-0 items-center whitespace-nowrap rounded-full bg-accent-tint px-2 text-[11px] font-medium text-accent-ink">主对话</span>}
          </span>
          {last && <span className="mt-0.5 block truncate text-[12px] text-ink-3">{last}</span>}
        </span>
        <span className="flex shrink-0 flex-col items-end gap-1">
          <span className="text-[11px] text-ink-3 tabular-nums">{ago(s.updated_at, now)}</span>
          {flagged && <StatePill state={st} />}
        </span>
      </a>
      <IconButton
        icon="more"
        label="更多"
        onClick={menu.open}
        active={!!menu.anchor}
        className={menu.anchor ? "" : "sm:opacity-0 sm:group-hover:opacity-100 sm:focus-visible:opacity-100"}
      />
      {menu.anchor && <Menu anchor={menu.anchor} onClose={menu.close} items={items} align="right" width={180} />}
    </div>
  );
}

function RenameDialog({ s, onClose, onDone }: { s: Conversation; onClose: () => void; onDone: (title: string) => void }) {
  const [title, setTitle] = useState(conversationTitle(s) === "新对话" ? "" : s.title);
  const [busy, setBusy] = useState(false);
  const trimmed = title.trim();
  const save = async () => {
    if (!trimmed || busy) return;
    setBusy(true);
    try {
      await api(`/sessions/${enc(s.machine_id)}/${enc(s.session_id)}/rename`, { method: "POST", json: { title: trimmed } });
      toast("已重命名", "ok");
      onDone(trimmed);
      onClose();
    } catch (e) {
      toast(errorText(e), "error");
      setBusy(false);
    }
  };
  return (
    <Dialog
      open
      onClose={() => !busy && onClose()}
      title="重命名对话"
      width={420}
      footer={
        <>
          <Button tone="quiet" disabled={busy} onClick={onClose}>取消</Button>
          <Button tone="primary" busy={busy} disabled={!trimmed || trimmed === s.title} onClick={() => void save()}>保存</Button>
        </>
      }
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <input className={inputClass} value={title} autoFocus onFocus={(e) => e.currentTarget.select()} onChange={(e) => setTitle(e.target.value)} placeholder="对话标题" aria-label="对话标题" />
        <p className="mt-1.5 text-[11.5px] leading-relaxed text-ink-3">你起的标题不会再被自动标题覆盖。</p>
      </form>
    </Dialog>
  );
}
