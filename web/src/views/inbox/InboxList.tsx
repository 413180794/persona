// The inbox list (docs/inbox.md「App」): filters by source and kind, search, then 需要你 (agents waiting on you first),
// 知悉即可, 已折叠 and 已处理, each a section that opens / closes from its title.
import { useEffect, useMemo, useState } from "react";
import {
  agentDraft, agentWorking, conversationRoute, fromLine, interactionAgentTitle, interactionAsk, interactionTitle, isDone, isLark,
  bulkScope, connectorSources, KIND_FILTERS, kindLabel, labelIsUrgent, mailAccounts, mailShort, sections, sendReply, setAllDone, setDone, shortTime, TIERS,
  useInbox, waitingAgents, type InboxItem, type Tier,
} from "../../lib/inbox";
import { useMailAccounts } from "../../lib/mail";
import { hubTranslation, useTranslatePrefs } from "../../lib/translate";
import { machineWithHub } from "../../lib/mesh";
import { href, onLinkClick } from "../../lib/router";
import { useStore } from "../../lib/store";
import type { Interaction } from "../../lib/types";
import { Collapse, cx, IconButton, Loading } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { DraftDialog } from "./InboxDialogs";
import { AgentWorking, Count, DraftCard, Glyph, ItemAvatar, Spark, StatusMenu, Tag, type GlyphName } from "./InboxParts";

function useStored(key: string): [string, (v: string) => void] {
  const [v, setV] = useState(() => localStorage.getItem(key) ?? "");
  return [v, (x: string) => {
    setV(x);
    if (x) localStorage.setItem(key, x);
    else localStorage.removeItem(key);
  }];
}

export function InboxList({ selected }: { selected?: string }) {
  const items = useInbox((s) => s.items);
  const loaded = useInbox((s) => s.loaded);
  const loading = useInbox((s) => s.loading);
  const error = useInbox((s) => s.error);
  const interactions = useStore((s) => s.interactions);
  const sessions = useStore((s) => s.sessions);
  const mutes = useStore((s) => s.mutes);
  const storeLoaded = useStore((s) => s.loaded); // the waiting agents come with it and push the rows down
  const [source, setSource] = useStored("inbox.source");
  const [kind, setKind] = useStored("inbox.kind");
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState<Set<string>>(() => new Set(["action", "fyi"]));
  const [editing, setEditing] = useState<InboxItem | null>(null);
  const accounts = useMailAccounts();

  // The opened item stays in sight in the list (a link straight to it, a click in the detail).
  useEffect(() => {
    if (!selected) return;
    document.querySelector(`[data-item="${CSS.escape(selected)}"]`)?.scrollIntoView({ block: "nearest" });
  }, [selected, loaded, storeLoaded]);

  const s = useMemo(() => sections(items, { source, kind, query }), [items, source, kind, query]);
  const agents = useMemo(() => waitingAgents(interactions, sessions, mutes, { source, kind, query }), [interactions, sessions, mutes, source, kind, query]);
  const chips = mailAccounts(accounts, items);
  const connectors = useMemo(() => connectorSources(items), [items]);
  const empty = !s.tiers.action.length && !s.tiers.fyi.length && !s.tiers.noise.length && !agents.length;
  // 全部标为已处理: the open items the list shows (its filters and search); the agents waiting are not items.
  const openInView = kind === "agent" ? [] : [...s.tiers.action, ...s.tiers.fyi, ...s.tiers.noise];
  const filterNames = [
    source === "feishu" ? "飞书" : connectors.find((c) => c.id === source)?.name ?? mailShort(chips.find((a) => a.id === source)?.email ?? ""),
    KIND_FILTERS.find((k) => k.key === kind)?.label ?? "",
  ];
  const scope = bulkScope(filterNames, query, openInView.length);
  const toggle = (k: string) => setOpen((o) => {
    const n = new Set(o);
    if (n.has(k)) n.delete(k);
    else n.add(k);
    return n;
  });
  // Agents have no source: picking one leaves the Agent filter, and the Agent filter leaves the source.
  const pickSource = (v: string) => {
    setSource(source === v ? "" : v);
    if (kind === "agent") setKind("");
  };
  const pickKind = (v: string) => {
    setKind(kind === v ? "" : v);
    if (v === "agent") setSource("");
  };

  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div className="mx-auto max-w-[760px] px-3 pt-3 pb-16 sm:px-4">
        <label className="flex h-8 items-center gap-2 rounded-control bg-surface px-2.5 shadow-btn focus-within:shadow-[inset_0_0_0_1px_var(--accent)]">
          <Icon name="search" size={13} stroke={2} className="shrink-0 text-ink-3" />
          <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="搜索标题、发件人、内容" aria-label="搜索收件箱" className="min-w-0 flex-1 bg-transparent text-[12.5px] text-ink outline-none placeholder:text-ink-3" />
          {query && <IconButton icon="x" label="清除" size={20} iconSize={11} onClick={() => setQuery("")} />}
        </label>
        <div className="mt-2.5 flex flex-wrap items-center gap-1.5" role="toolbar" aria-label="筛选">
          <Chip on={!source && !kind} onClick={() => { setSource(""); setKind(""); }}>全部</Chip>
          <Chip on={source === "feishu"} onClick={() => pickSource("feishu")}>飞书</Chip>
          {chips.map((a) => (
            <Chip key={a.id} on={source === a.id} onClick={() => pickSource(a.id)} title={a.email}>{mailShort(a.email)}</Chip>
          ))}
          {connectors.map((c) => (
            <Chip key={`c:${c.id}`} on={source === c.id} onClick={() => pickSource(c.id)}>{c.name}</Chip>
          ))}
          <span aria-hidden className="mx-0.5 h-4 w-px bg-line-strong" />
          {KIND_FILTERS.map((k) => (
            <Chip key={k.key} on={kind === k.key} onClick={() => pickKind(k.key)}>{k.label}</Chip>
          ))}
          <button
            type="button"
            disabled={!openInView.length}
            title={`全部标为已处理：${scope}`}
            onClick={() => void setAllDone(openInView, true)}
            className="ml-auto inline-flex h-7 items-center gap-1.5 rounded-control bg-surface px-2.5 text-[12px] whitespace-nowrap text-ink-2 shadow-btn transition-colors duration-150 hover:bg-hover hover:text-ink disabled:pointer-events-none disabled:opacity-45"
          >
            <Icon name="check" size={12} stroke={2.6} />
            全部标为已处理
            <span className="text-ink-3">{scope}</span>
          </button>
        </div>

        {error && !items.length && <p className="mt-4 text-[12.5px] text-red">{error}</p>}
        {!loaded && <div className="mt-6 px-1"><Loading /></div>}
        {loaded && empty && (
          <div className="mt-10 flex flex-col items-center gap-2 text-center">
            <span className="flex size-10 items-center justify-center rounded-full bg-green-tint text-green">
              <Icon name="check" size={17} stroke={2.6} />
            </span>
            <p className="text-[13.5px] font-medium text-ink">{query ? "没有匹配的" : "都处理完了"}</p>
            <p className="text-[12.5px] text-ink-3">{query ? "换个关键词试试。" : "新的飞书消息和邮件会出现在这里。"}</p>
          </div>
        )}

        {TIERS.map((t) => {
          const list = s.tiers[t.key];
          const waiting = t.key === "action" ? agents : [];
          if (!list.length && !waiting.length) return null;
          const clear = t.key === "fyi" ? "全部看过" : t.key === "noise" ? "全部清除" : undefined;
          return (
            <Section key={t.key} k={t.key} title={t.title} count={list.length + waiting.length} open={open.has(t.key)} onToggle={() => toggle(t.key)}
              action={clear && list.length ? { label: clear, run: () => setAllDone(list, true) } : undefined}>
              {waiting.map((ix) => <AgentRow key={ix.id} ix={ix} />)}
              {list.map((it) => <ItemRow key={it.id} it={it} selected={it.id === selected} showAccount={chips.length > 1} onEdit={() => setEditing(it)} />)}
            </Section>
          );
        })}
        {s.done.length > 0 && (
          <Section k="done" title="已处理" count={s.done.length} open={open.has("done")} onToggle={() => toggle("done")}>
            {s.done.map((it) => <ItemRow key={it.id} it={it} selected={it.id === selected} showAccount={chips.length > 1} onEdit={() => setEditing(it)} />)}
          </Section>
        )}
        {loading && loaded && <span className="sr-only">正在刷新</span>}
      </div>
      {editing && <DraftDialog item={editing} onClose={() => setEditing(null)} />}
    </div>
  );
}

function Chip({ on, onClick, children, title }: { on: boolean; onClick: () => void; children: React.ReactNode; title?: string }) {
  return (
    <button
      type="button"
      title={title}
      aria-pressed={on}
      onClick={onClick}
      className={cx("h-7 rounded-control px-2.5 text-[12px] whitespace-nowrap transition-colors duration-150", on ? "bg-ink font-medium text-surface" : "bg-surface text-ink-2 shadow-btn hover:bg-hover hover:text-ink")}
    >
      {children}
    </button>
  );
}

function Section({ k, title, count, open, onToggle, action, children }: { k: Tier | "done"; title: string; count: number; open: boolean; onToggle: () => void; action?: { label: string; run: () => void }; children: React.ReactNode }) {
  return (
    <section className="mt-4">
      <div className="flex items-center gap-2 px-1 pb-1.5">
        <button type="button" onClick={onToggle} aria-expanded={open} className="flex min-w-0 flex-1 items-center gap-1.5 text-left">
          <span className={cx("text-[12px] font-medium whitespace-nowrap", k === "action" ? "text-ink" : "text-ink-3")}>{title}</span>
          <span className="text-[11.5px] text-ink-3 tabular-nums">{count}</span>
          <Icon name="chevronRight" size={11} stroke={2.4} className="text-ink-3 transition-transform duration-200" style={{ transform: open ? "rotate(90deg)" : undefined }} />
        </button>
        {action && (
          <button type="button" onClick={action.run} className="h-6 rounded-[6px] px-1.5 text-[11.5px] whitespace-nowrap text-ink-3 transition-colors hover:bg-hover hover:text-ink">
            {action.label}
          </button>
        )}
      </div>
      <Collapse open={open}>
        <div inert={!open} className="flex flex-col gap-1.5 pb-0.5">{children}</div>
      </Collapse>
    </section>
  );
}

function ItemRow({ it, selected, showAccount, onEdit }: { it: InboxItem; selected: boolean; showAccount: boolean; onEdit: () => void }) {
  const to = { name: "inbox", id: it.id } as const;
  const done = isDone(it);
  const label = kindLabel(it);
  const draft = !done && agentDraft(it);
  const working = !done && agentWorking(it);
  const rows = !done ? it.doc?.rows ?? [] : [];
  const first = rows[0];
  const translated = hubTranslation(it, useTranslatePrefs());
  const from = fromLine(it, translated?.preview);
  return (
    <div data-item={it.id} className={cx("group relative scroll-my-3 rounded-[14px] transition-[background-color,box-shadow] duration-150 hover:bg-inset", selected ? "bg-inset shadow-[0_0_0_1.5px_var(--accent)]" : "bg-surface shadow-card")}>
      <a href={href(to)} onClick={(e) => onLinkClick(e, to)} aria-current={selected ? "page" : undefined} className="flex gap-3 rounded-[14px] p-3 outline-offset-[-2px]">
        <ItemAvatar it={it} />
        <span className="min-w-0 flex-1">
          <span className="flex min-w-0 items-center gap-1.5">
            <span className={cx("truncate text-[13.5px]", done ? "text-ink-2" : "font-semibold text-ink")}>{translated?.subject || it.title}</span>
            {translated && <span role="img" aria-label="已翻译" title="已翻译" className="shrink-0 text-ink-3"><Icon name="translate" size={12} /></span>}
            {label && <Tag urgent={labelIsUrgent(it)}>{label}</Tag>}
            <span className="flex-1" />
            <span className="shrink-0 text-[11.5px] text-ink-3 tabular-nums group-focus-within:invisible group-hover:invisible">{shortTime(it.updated_at)}</span>
          </span>
          <span className="mt-0.5 flex items-start gap-2">
            <span className="min-w-0 flex-1">
              {it.summary ? (
                <>
                  <span className={cx("line-clamp-2 text-[12.5px] leading-[1.5]", done ? "text-ink-2" : "text-ink")}><Spark />{it.summary}</span>
                  <span className="mt-0.5 block truncate text-[11.5px] text-ink-2">{from}</span>
                </>
              ) : (
                <span className="line-clamp-2 text-[12.5px] leading-[1.5] text-ink-2">{from}</span>
              )}
              {showAccount && !isLark(it) && !it.connector && <span className="mt-0.5 block truncate text-[11px] text-ink-3">{it.account_name}</span>}
            </span>
            {it.count > 1 && !done && <Count n={it.count} accent={it.kind === "mention"} />}
          </span>
        </span>
      </a>
      {(first || draft || working) && (
        <div className="-mt-1 flex flex-col gap-2 pr-3 pb-3 pl-[64px]">
          {first && (
            <div className="flex min-w-0 items-center gap-1.5 text-[11.5px] text-ink-2">
              <Glyph name="reply" size={12} className="shrink-0 -scale-y-100 text-ink-3" />
              <span className="min-w-0 truncate">{first.title}</span>
              {first.status?.[0] && <StatusMenu it={it} row={first} status={first.status[0]} />}
              {rows.length > 1 && <span className="shrink-0 text-ink-3">+{rows.length - 1} 行</span>}
            </div>
          )}
          {draft && <DraftCard it={it} onEdit={onEdit} onSend={() => void sendReply(it, draft, it.agent?.changes ?? [])} />}
          {working && <AgentWorking it={it} />}
        </div>
      )}
      <button
        type="button"
        onClick={() => void setDone(it, !done)}
        className="absolute top-2.5 right-2.5 flex h-6 items-center gap-1 rounded-[7px] bg-surface px-2 text-[11.5px] font-medium whitespace-nowrap text-ink-2 opacity-0 shadow-btn transition-opacity duration-150 group-focus-within:opacity-100 group-hover:opacity-100 hover:text-ink focus-visible:opacity-100"
        title={done ? "移回收件箱" : "标为已处理"}
      >
        {done ? <Glyph name="undo" size={12} stroke={2.2} /> : <Icon name="check" size={12} stroke={2.6} />}
        {done ? "移回" : "已处理"}
      </button>
    </div>
  );
}

const IX_GLYPH: Record<Interaction["kind"], GlyphName | "list" | "terminal"> = { question: "question", plan: "list", permission: "hand", screen: "terminal" };

/** An agent waiting on you: which agent, what it asks, then what it waits for (red) and where. */
function AgentRow({ ix }: { ix: Interaction }) {
  const sessions = useStore((s) => s.sessions);
  const assistant = useStore((s) => s.assistant);
  const machines = useStore((s) => s.machines);
  const to = conversationRoute(ix.machine_id, ix.session_id, sessions, assistant);
  const ask = interactionAsk(ix);
  const glyph = IX_GLYPH[ix.kind];
  const where = to.name === "jarvis" ? "Persona" : machineWithHub(ix.machine_id, machines);
  return (
    <a href={href(to)} onClick={(e) => onLinkClick(e, to)} className="flex gap-3 rounded-[14px] bg-surface p-3 shadow-card transition-colors duration-150 hover:bg-inset">
      <span className="flex size-10 shrink-0 items-center justify-center rounded-[10px] bg-red-tint text-red">
        {glyph === "list" || glyph === "terminal" ? <Icon name={glyph} size={18} stroke={2} /> : <Glyph name={glyph} size={18} stroke={2} />}
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex min-w-0 items-center gap-1.5">
          <span className="truncate text-[13.5px] font-semibold text-ink">{interactionAgentTitle(ix)}</span>
          <span className="flex-1" />
          <span className="shrink-0 text-[11.5px] text-ink-3 tabular-nums">{shortTime(ix.created_at)}</span>
        </span>
        {ask ? (
          <>
            <span className="mt-0.5 line-clamp-2 text-[12.5px] leading-[1.5] text-ink">{ask}</span>
            <span className="mt-0.5 block truncate text-[11.5px]">
              <span className="font-medium text-red">{interactionTitle(ix)}</span>
              <span className="text-ink-3"> · {where}</span>
            </span>
          </>
        ) : (
          <>
            <span className="mt-0.5 block truncate text-[12.5px] font-medium text-red">{interactionTitle(ix)}</span>
            <span className="mt-0.5 block truncate text-[11.5px] text-ink-3">{where}</span>
          </>
        )}
      </span>
    </a>
  );
}
