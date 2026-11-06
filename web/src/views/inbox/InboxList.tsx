// The inbox list (docs/inbox.md「App」): filters by source and kind, search, then 需要你 (agents waiting on you first),
// 知悉即可, 已折叠 and 已处理, each a section that opens / closes from its title.
import { useEffect, useMemo, useState } from "react";
import {
  agentDraft, agentWorking, conversationRoute, fromLine, interactionAgentTitle, interactionAsk, interactionTitle, isDone, isLark,
  listInboxMutes, muteItem, muteLine, muteShortName, muteTitle, unmuteInbox,
  BULK_CONFIRM_AT, bulkScope, bulkScopeName, connectorSources, KIND_FILTERS, kindLabel, labelIsUrgent, mailAccounts, mailShort, reloadInbox, sections, sendReply, setAllDone, setDone, shortTime, TIERS,
  useInbox, waitingAgents, type BulkAction, type InboxItem, type InboxMute, type SectionKey,
} from "../../lib/inbox";
import { useMailAccounts } from "../../lib/mail";
import { hubTranslation, useTranslatePrefs } from "../../lib/translate";
import { machineWithHub } from "../../lib/mesh";
import { href, navigate, onLinkClick } from "../../lib/router";
import { useInboxSources } from "../../lib/inbox-sources";
import { useStore } from "../../lib/store";
import type { Interaction } from "../../lib/types";
import { t } from "../../i18n";
import { Button, Collapse, cx, Dialog, IconButton, Loading, Menu, toast, useMenu, type MenuItem } from "../../ui/atoms";
import { errorText } from "../../lib/api";
import { contextMenuProps } from "../../ui/context-menu";
import type { MenuAt } from "../../ui/menu-model";
import { Icon } from "../../ui/icons";
import { isNoHover } from "../../ui/media-query";
import { DraftDialog } from "./InboxDialogs";
import { TriageBanner } from "./TriageBanner";
import { EventMark } from "./InboxEvent";
import { AgentWorking, Chip, Count, DraftCard, Glyph, ItemAvatar, Spark, StatusMenu, Tag, type GlyphName } from "./InboxParts";

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
  const triage = useInbox((s) => s.triage);
  const interactions = useStore((s) => s.interactions);
  const sessions = useStore((s) => s.sessions);
  const mutes = useStore((s) => s.mutes);
  const storeLoaded = useStore((s) => s.loaded);
  // The waiting agents come with the store, often after the items: painting the items first and then the agents over
  // them pushed every row down (a layout shift of 600px on a phone), so the list waits for both and paints once.
  const ready = loaded && storeLoaded;
  const [source, setSource] = useStored("inbox.source");
  const [kind, setKind] = useStored("inbox.kind");
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState<Set<string>>(() => new Set(["action", "working", "fyi"]));
  const [editing, setEditing] = useState<InboxItem | null>(null);
  const accounts = useMailAccounts();
  const more = useMenu();
  const [mutesOpen, setMutesOpen] = useState(false);
  // 全部已处理 / 全部忽略 over many items asks first; the items are the ones shown when it was asked.
  const [confirm, setConfirm] = useState<{ action: Exclude<BulkAction, "reopen">; items: InboxItem[]; scope: string } | null>(null);

  // The opened item stays in sight in the list (a link straight to it, a click in the detail).
  useEffect(() => {
    if (!selected) return;
    document.querySelector(`[data-item="${CSS.escape(selected)}"]`)?.scrollIntoView({ block: "nearest" });
  }, [selected, loaded, storeLoaded]);

  const s = useMemo(() => sections(items, { source, kind, query }), [items, source, kind, query]);
  const agents = useMemo(() => waitingAgents(interactions, sessions, mutes, { source, kind, query }), [interactions, sessions, mutes, source, kind, query]);
  const chips = mailAccounts(accounts, items);
  const connectors = useMemo(() => connectorSources(items), [items]);
  const failed = Boolean(error) && !items.length;
  const empty = !s.tiers.action.length && !s.tiers.working.length && !s.tiers.fyi.length && !s.tiers.noise.length && !agents.length;
  // nothing ever came in, nothing filtered: is anything connected at all? (still unknown: the plain 都处理完了)
  const sources = useInboxSources(ready && empty && !failed && !items.length && !query && !source && !kind);
  const unconnected = sources && !sources.connected ? sources : null;
  // 全部标为已处理: the open items the list shows (its filters and search); the agents waiting are not items.
  const openInView = kind === "agent" ? [] : [...s.tiers.action, ...s.tiers.working, ...s.tiers.fyi, ...s.tiers.noise];
  const filterNames = [
    source === "feishu" ? t("inbox-inboxlist.source_lark") : connectors.find((c) => c.id === source)?.name ?? mailShort(chips.find((a) => a.id === source)?.email ?? ""),
    KIND_FILTERS.find((k) => k.key === kind)?.label ?? "",
  ];
  const scope = bulkScope(filterNames, query, openInView.length);
  const runBulk = (action: Exclude<BulkAction, "reopen">) => {
    if (!openInView.length) return;
    if (openInView.length >= BULK_CONFIRM_AT) setConfirm({ action, items: openInView, scope: bulkScopeName(filterNames, query) });
    else void setAllDone(openInView, action);
  };
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
          <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t("inbox-inboxlist.search_placeholder")} aria-label={t("inbox-inboxlist.search_label")} className="min-w-0 flex-1 bg-transparent text-[12.5px] text-ink outline-none placeholder:text-ink-3" />
          {query && <IconButton icon="x" label={t("inbox-inboxlist.clear")} size={20} iconSize={11} onClick={() => setQuery("")} />}
        </label>
        {/* A phone: the chips in one row that scrolls sideways, 全部标为已处理 in the ⋯ menu at its end. */}
        <div className="mt-2.5 flex items-center gap-1.5">
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1.5 max-sm:-my-1 max-sm:flex-nowrap max-sm:overflow-x-auto max-sm:py-1 max-sm:scrollbar-none max-sm:[&>*]:shrink-0" role="toolbar" aria-label={t("inbox-inboxlist.filters")}>
          <Chip on={!source && !kind} onClick={() => { setSource(""); setKind(""); }}>{t("inbox-inboxlist.all")}</Chip>
          <Chip on={source === "feishu"} onClick={() => pickSource("feishu")}>{t("inbox-inboxlist.source_lark")}</Chip>
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
            title={t("inbox-inboxlist.mark_all_done_scope", { scope })}
            onClick={() => runBulk("done")}
            className="ml-auto inline-flex h-7 items-center gap-1.5 rounded-control bg-surface px-2.5 text-[12px] whitespace-nowrap text-ink-2 shadow-btn transition-colors duration-150 hover:bg-hover hover:text-ink disabled:pointer-events-none disabled:opacity-45 max-sm:hidden"
          >
            <Icon name="check" size={12} stroke={2.6} />
            {t("inbox-inboxlist.mark_all_done")}
            <span className="text-ink-3">{scope}</span>
          </button>
        </div>
        <IconButton icon="more" label={t("inbox-inboxlist.more")} onClick={more.open} active={Boolean(more.anchor)} />
        </div>
        {more.anchor && (
          <Menu anchor={more.anchor} onClose={more.close} align="right" width={240}
            items={[
              { key: "all-done", label: t("inbox-inboxlist.mark_all_done"), hint: scope, icon: "check", disabled: !openInView.length, onSelect: () => runBulk("done") },
              { key: "all-dismiss", label: t("inbox-inboxlist.dismiss_all"), hint: scope, icon: "x", disabled: !openInView.length, onSelect: () => runBulk("dismiss") },
              { key: "muted", label: t("inbox-inboxlist.muted_list"), icon: "bellOff", divider: true, onSelect: () => setMutesOpen(true) },
            ]} />
        )}
        {mutesOpen && <MutesDialog onClose={() => setMutesOpen(false)} />}
        <BulkConfirm confirm={confirm} onClose={() => setConfirm(null)} />

        <TriageBanner triage={triage} />
        {/* never loaded (an error and no items): its own empty state with 重试, not 都处理完了 under the error (#2970);
            with agents waiting the list is not empty, so the reason stays a line over it */}
        {ready && failed && !empty && <p className="mt-4 text-[12.5px] text-red">{error}</p>}
        {!ready && <div className="mt-6 px-1"><Loading /></div>}
        {ready && failed && empty && (
          <div className="mt-10 flex flex-col items-center gap-2 text-center" role="status" data-inbox-failed>
            <span className="flex size-10 items-center justify-center rounded-full bg-field text-ink-3">
              <Icon name="inbox" size={17} />
            </span>
            <p className="text-[13.5px] font-medium text-ink">{t("inbox-inboxlist.failed_title")}</p>
            <p className="max-w-80 text-[12.5px] text-ink-3">{t("inbox-inboxlist.failed_hint")}</p>
            <p className="max-w-80 text-[11.5px] break-words text-ink-3">{error}</p>
            <Button tone="secondary" icon="retry" busy={loading} onClick={() => void reloadInbox()} className="mt-2">
              {loading ? t("inbox-inboxlist.retrying") : t("inbox-inboxlist.retry")}
            </Button>
          </div>
        )}
        {ready && empty && !failed && unconnected && (
          <div className="mt-10 flex flex-col items-center gap-2 text-center" data-inbox-no-sources>
            <span className="flex size-10 items-center justify-center rounded-full bg-field text-ink-3">
              <Icon name="inbox" size={17} />
            </span>
            <p className="text-[13.5px] font-medium text-ink">{t("inbox-inboxlist.no_sources_title")}</p>
            <p className="max-w-80 text-[12.5px] leading-relaxed text-ink-3">{t("inbox-inboxlist.no_sources_hint")}</p>
            <div className="mt-2 flex flex-wrap justify-center gap-1.5">
              {unconnected.offers.map((o, i) => (
                <Button key={o.id} tone={i === 0 ? "primary" : "secondary"} icon={i === 0 ? "plus" : undefined} onClick={() => navigate(o.href)}>
                  {t("inbox-inboxlist.no_sources_connect", { name: o.name })}
                </Button>
              ))}
            </div>
          </div>
        )}
        {ready && empty && !failed && !unconnected && (
          <div className="mt-10 flex flex-col items-center gap-2 text-center">
            <span className="flex size-10 items-center justify-center rounded-full bg-green-tint text-green">
              <Icon name="check" size={17} stroke={2.6} />
            </span>
            <p className="text-[13.5px] font-medium text-ink">{query ? t("inbox-inboxlist.empty_no_match") : t("inbox-inboxlist.empty_all_done")}</p>
            <p className="text-[12.5px] text-ink-3">{query ? t("inbox-inboxlist.empty_no_match_hint") : t("inbox-inboxlist.empty_all_done_hint")}</p>
          </div>
        )}

        {ready && TIERS.map((tier) => {
          const list = s.tiers[tier.key];
          const waiting = tier.key === "action" ? agents : [];
          if (!list.length && !waiting.length) return null;
          const clear = tier.key === "fyi" ? t("inbox-inboxlist.clear_fyi") : tier.key === "noise" ? t("inbox-inboxlist.clear_noise") : undefined;
          return (
            <Section key={tier.key} k={tier.key} title={tier.title} count={sectionCount(list.length, waiting.length)} open={open.has(tier.key)} onToggle={() => toggle(tier.key)}
              action={clear && list.length ? { label: clear, run: () => setAllDone(list, true) } : undefined}>
              {waiting.map((ix) => <AgentRow key={ix.id} ix={ix} />)}
              {list.map((it) => <ItemRow key={it.id} it={it} selected={it.id === selected} showAccount={chips.length > 1} onEdit={() => setEditing(it)} />)}
            </Section>
          );
        })}
        {ready && s.done.length > 0 && (
          <Section k="done" title={t("inbox-inboxlist.done")} count={sectionCount(s.done.length, 0)} open={open.has("done")} onToggle={() => toggle("done")}>
            {s.done.map((it) => <ItemRow key={it.id} it={it} selected={it.id === selected} showAccount={chips.length > 1} onEdit={() => setEditing(it)} />)}
          </Section>
        )}
        {loading && loaded && <span className="sr-only">{t("inbox-inboxlist.refreshing")}</span>}
      </div>
      {editing && <DraftDialog item={editing} onClose={() => setEditing(null)} />}
    </div>
  );
}

/**
 * A section title's count, saying what it counts (#3288): the sidebar's 收件箱 badge counts messages only, while 需要你
 * also lists the agents waiting on you (those are 总览's count), so the two are written apart: 「4 条 · 7 个 Agent」, each
 * with its unit, which a phone has no hover to show (#4146).
 */
export function sectionCount(items: number, agents: number): { text: string; label: string } {
  const parts = [items > 0 && t("inbox-inboxlist.count_items", { count: items }), agents > 0 && t("inbox-inboxlist.count_agents", { count: agents })].filter((x): x is string => Boolean(x));
  return {
    text: items > 0 && agents > 0 ? `${t("inbox-inboxlist.count_items_short", { count: items })} · ${t("inbox-inboxlist.count_agents", { count: agents })}` : agents > 0 ? t("inbox-inboxlist.count_agents", { count: agents }) : String(items),
    label: parts.join(t("inbox-inboxlist.count_separator")),
  };
}

function Section({ k, title, count, open, onToggle, action, children }: { k: SectionKey | "done"; title: string; count: { text: string; label: string }; open: boolean; onToggle: () => void; action?: { label: string; run: () => void }; children: React.ReactNode }) {
  return (
    <section className="mt-4">
      <div className="flex items-center gap-2 px-1 pb-1.5">
        <button type="button" onClick={onToggle} aria-expanded={open} className="flex min-w-0 flex-1 items-center gap-1.5 text-left">
          <span className={cx("text-[12px] font-medium whitespace-nowrap", k === "action" ? "text-ink" : "text-ink-3")}>{title}</span>
          <span title={count.label} className="text-[11.5px] whitespace-nowrap text-ink-3 tabular-nums"><span aria-hidden>{count.text}</span><span className="sr-only">{count.label}</span></span>
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

/** Room for the row's 已处理 check, always shown on a touch screen (#4141), right of the time. */
const TOUCH_DONE_ROOM = "[@media(hover:none)]:mr-8";

/** The row's 已处理 / 移回: on a touch screen marking done says so with 撤销, since a tap there can be a slip. */
export async function markFromRow(it: InboxItem, done: boolean, touch = isNoHover()): Promise<void> {
  // after the request: 撤销 cannot race it, and a failure shows its own error instead of this
  const ok = await setDone(it, done);
  if (ok && done && touch) toast(t("inbox-inboxlist.marked_done"), "ok", { label: t("lib-inbox.undo"), run: () => void setDone(it, false) });
}

function ItemRow({ it, selected, showAccount, onEdit }: { it: InboxItem; selected: boolean; showAccount: boolean; onEdit: () => void }) {
  const to = { name: "inbox", id: it.id } as const;
  const done = isDone(it);
  const label = kindLabel(it);
  // a suggestion takes the AI 分身's draft's place, and the event's handling the AI 分身's 正在处理
  const draft = !done && !it.suggestion && agentDraft(it);
  const working = !done && !it.handling && agentWorking(it);
  const mark = !done && (it.suggestion || it.handling) ? <EventMark it={it} /> : null;
  const rows = !done ? it.doc?.rows ?? [] : [];
  const first = rows[0];
  const translated = hubTranslation(it, useTranslatePrefs());
  const from = fromLine(it, translated?.preview);
  // a right click: 已处理 / 移回, then what it can be muted by (iOS: the row's long press)
  // (the menu key / Shift+F10 open it next to the row, see ui/context-menu.ts)
  const [menu, setMenu] = useState<MenuAt | null>(null);
  return (
    <div data-item={it.id} {...contextMenuProps((at) => setMenu(at))} className={cx("group relative scroll-my-3 rounded-[14px] transition-[background-color,box-shadow] duration-150 hover:bg-inset", selected ? "bg-inset shadow-[0_0_0_1.5px_var(--accent)]" : "bg-surface shadow-card")}>
      <a href={href(to)} onClick={(e) => onLinkClick(e, to)} aria-current={selected ? "page" : undefined} className="flex gap-3 rounded-[14px] p-3 outline-offset-[-2px]">
        <ItemAvatar it={it} />
        <span className="min-w-0 flex-1">
          <span className="flex min-w-0 items-center gap-1.5">
            <span className={cx("truncate text-[13.5px]", done ? "text-ink-2" : "font-semibold text-ink")}>{translated?.subject || it.title}</span>
            {translated && <span role="img" aria-label={t("inbox-inboxlist.translated")} title={t("inbox-inboxlist.translated")} className="shrink-0 text-ink-3"><Icon name="translate" size={12} /></span>}
            {label && <Tag urgent={labelIsUrgent(it)}>{label}</Tag>}
            <span className="flex-1" />
            <span className={cx("shrink-0 text-[11.5px] text-ink-3 tabular-nums group-focus-within:invisible group-hover:invisible", TOUCH_DONE_ROOM)}>{shortTime(it.updated_at)}</span>
          </span>
          {/* on a touch screen the check's 44 pt tap area reaches this line: keep the summary and count clear of it */}
          <span className="mt-0.5 flex items-start gap-2 [@media(hover:none)]:pr-8">
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
      {(first || draft || working || mark) && (
        <div className="-mt-1 flex flex-col gap-2 pr-3 pb-3 pl-[64px]">
          {first && (
            <div className="flex min-w-0 items-center gap-1.5 text-[11.5px] text-ink-2">
              <Glyph name="reply" size={12} className="shrink-0 -scale-y-100 text-ink-3" />
              <span className="min-w-0 truncate">{first.title}</span>
              {first.status?.[0] && <StatusMenu it={it} row={first} status={first.status[0]} />}
              {rows.length > 1 && <span className="shrink-0 text-ink-3">{t("inbox-inboxlist.more_rows", { count: rows.length - 1 })}</span>}
            </div>
          )}
          {draft && <DraftCard it={it} onEdit={onEdit} onSend={() => void sendReply(it, draft, it.agent?.changes ?? [])} />}
          {working && <AgentWorking it={it} />}
          {mark}
        </div>
      )}
      {/* Shown on hover with a mouse; on a touch screen (no hover, #4141) always there as a check beside the time, so an
          item is marked without opening it, with 撤销 in the toast in case the tap was a slip. */}
      <button
        type="button"
        onClick={() => void markFromRow(it, !done)}
        aria-label={done ? t("inbox-inboxlist.move_back_title") : t("inbox-inboxlist.mark_done_title")}
        className="touch-hit absolute top-2.5 right-2.5 flex h-6 items-center gap-1 rounded-[7px] bg-surface px-2 text-[11.5px] font-medium whitespace-nowrap text-ink-2 opacity-0 shadow-btn transition-opacity duration-150 group-focus-within:opacity-100 group-hover:opacity-100 hover:text-ink focus-visible:opacity-100 [@media(hover:none)]:w-6 [@media(hover:none)]:justify-center [@media(hover:none)]:px-0 [@media(hover:none)]:opacity-100"
        title={done ? t("inbox-inboxlist.move_back_title") : t("inbox-inboxlist.mark_done_title")}
      >
        {done ? <Glyph name="undo" size={12} stroke={2.2} /> : <Icon name="check" size={12} stroke={2.6} />}
        <span className="[@media(hover:none)]:hidden">{done ? t("inbox-inboxlist.move_back") : t("inbox-inboxlist.done")}</span>
      </button>
      {menu && <Menu anchor={menu.anchor ?? null} point={menu.point} onClose={() => setMenu(null)} width={224} items={rowMenu(it, done)} />}
    </div>
  );
}

/** A row's menu: 已处理 (or 移回), then 静音这个仓库 / 会话 / 发件人 with the name under it (iOS InboxView.rowMenu). */
export function rowMenu(it: InboxItem, done: boolean): MenuItem[] {
  const items: MenuItem[] = [
    done
      ? { key: "undone", label: t("inbox-inboxlist.move_back"), icon: "inbox", onSelect: () => void setDone(it, false) }
      : { key: "done", label: t("inbox-inboxlist.done"), icon: "check", onSelect: () => void markFromRow(it, true, true) }, // a menu pick: 撤销 on any screen (#4162)
  ];
  (it.mutes ?? []).forEach((m, i) => items.push({ key: `mute:${m.tag}:${m.value}`, label: muteTitle(m), hint: muteShortName(m), icon: "bellOff", divider: i === 0, onSelect: () => void muteItem(it, m) }));
  return items;
}

/** 已静音 (the ⋯ menu; iOS InboxMutesView): every source's muted repositories, chats and senders, newest first, each
 *  with 取消静音. Only what comes in later comes back. */
function MutesDialog({ onClose }: { onClose: () => void }) {
  const [list, setList] = useState<InboxMute[] | null>(null);
  const [failed, setFailed] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const load = () => {
    setFailed("");
    listInboxMutes().then(setList, (e) => setFailed(errorText(e)));
  };
  useEffect(load, []);
  const unmute = async (m: InboxMute) => {
    setBusy(m.id);
    try {
      await unmuteInbox(m.id);
      setList((l) => (l ?? []).filter((x) => x.id !== m.id));
      toast(t("inbox-inboxlist.unmuted", { name: m.name }), "ok");
    } catch (e) {
      toast(t("inbox-inboxlist.unmute_failed", { error: errorText(e) }), "error");
    } finally {
      setBusy(null);
    }
  };
  return (
    <Dialog open onClose={onClose} title={t("inbox-inboxlist.muted_list")} width={420} footer={<Button tone="primary" onClick={onClose}>{t("inbox-inboxlist.close")}</Button>}>
      {failed ? (
        <div className="flex flex-col items-start gap-2">
          <p className="text-[12.5px] break-words text-red">{t("inbox-inboxlist.mutes_load_failed", { error: failed })}</p>
          <Button tone="secondary" icon="retry" onClick={load}>{t("inbox-inboxlist.retry")}</Button>
        </div>
      ) : !list ? (
        <div className="py-2"><Loading /></div>
      ) : !list.length ? (
        <div className="flex flex-col items-center gap-2 py-4 text-center">
          <span className="flex size-10 items-center justify-center rounded-full bg-field text-ink-3">
            <Icon name="bellOff" size={17} />
          </span>
          <p className="text-[13.5px] font-medium text-ink">{t("inbox-inboxlist.muted_empty_title")}</p>
          <p className="max-w-[300px] text-[12.5px] leading-relaxed text-ink-3">{t("inbox-inboxlist.muted_empty_hint")}</p>
        </div>
      ) : (
        <>
          <ul className="-mx-1 flex flex-col">
            {list.map((m) => (
              <li key={m.id} data-mute={m.id} className="flex items-center gap-3 rounded-control px-1 py-2 hover:bg-hover">
                <Icon name="bellOff" size={14} className="shrink-0 text-ink-3" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13px] text-ink" title={m.name}>{m.name}</span>
                  <span className="mt-0.5 block truncate text-[11.5px] text-ink-3">{muteLine(m)}</span>
                </span>
                <Button tone="quiet" busy={busy === m.id} disabled={busy !== null && busy !== m.id} onClick={() => void unmute(m)}>
                  {t("inbox-inboxlist.unmute")}
                </Button>
              </li>
            ))}
          </ul>
          <p className="mt-2 text-[11.5px] leading-relaxed text-ink-3">{t("inbox-inboxlist.muted_note")}</p>
        </>
      )}
    </Dialog>
  );
}

const IX_GLYPH: Record<Interaction["kind"], GlyphName | "list" | "terminal"> = { question: "question", plan: "list", permission: "hand", screen: "terminal", elicitation: "sheet" };

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

/** 「将处理 N 件」: asked before 全部已处理 / 全部忽略 over BULK_CONFIRM_AT items or more. */
function BulkConfirm({ confirm, onClose }: { confirm: { action: "done" | "dismiss"; items: InboxItem[]; scope: string } | null; onClose: () => void }) {
  const n = confirm?.items.length ?? 0;
  const dismiss = confirm?.action === "dismiss";
  return (
    <Dialog
      open={Boolean(confirm)}
      onClose={onClose}
      title={t(dismiss ? "inbox-inboxlist.confirm_dismiss_title" : "inbox-inboxlist.confirm_done_title")}
      width={380}
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>{t("common.cancel")}</Button>
          <Button
            tone="primary"
            icon={dismiss ? "x" : "check"}
            onClick={() => {
              if (confirm) void setAllDone(confirm.items, confirm.action);
              onClose();
            }}
          >
            {t(dismiss ? "inbox-inboxlist.confirm_dismiss" : "inbox-inboxlist.confirm_done", { count: n })}
          </Button>
        </>
      }
    >
      <p className="text-[13px] leading-relaxed text-ink-2">{t("inbox-inboxlist.confirm_body", { count: n, scope: confirm?.scope ?? "" })}</p>
    </Dialog>
  );
}
