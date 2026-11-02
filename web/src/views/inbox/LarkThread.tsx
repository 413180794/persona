// A Feishu chat, read-only, opened at the item's first new message (docs/inbox.md: 「点开：飞书会话停在这件事的第一条新消息」).
// Not a second Feishu (docs/feishu.md): no reactions, profiles or chat settings here; replying goes through
// AI 草拟回复 → 发送 (/inbox/{id}/reply). Rich text and cards are simplified to text.
// What was really seen is reported read to Feishu (docs/inbox.md「看过的飞书消息按可见即读上报」, lark-read.ts): a message
// counts once at least half of it is in the list while the page is in front, placed, and no dialog covers it. With
// 设置 › 翻译 on for this chat, others' messages in another language get their translation under them.
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { api, enc, errorText, getToken } from "../../lib/api";
import { clock } from "../../lib/format";
import {
  cardText, comparePosition, emojiLabel, isDone, mergeMessages, partRuns, withToken,
  type InboxItem, type LarkMessage, type LarkPage, type LarkPart,
} from "../../lib/inbox";
import { initialCursor, LarkReadTracker } from "../../lib/lark-read";
import { clickLink, linkHref, prefetchLink } from "../../lib/links";
import { onHubEvent } from "../../lib/store";
import { isForeignCached, requestTranslation, translatesChat, translationOf, useTranslatePrefs, useTranslations } from "../../lib/translate";
import { Button, cx, Loading, Spinner } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Monogram } from "./InboxParts";

const PAGE = 30;

export function LarkThread({ item }: { item: InboxItem }) {
  const feed = item.lark!.feed_id;
  const focus = !isDone(item) ? item.lark?.start_position : undefined;
  const [page, setPage] = useState<LarkPage | null>(null);
  const [error, setError] = useState("");
  const [more, setMore] = useState<"" | "older" | "newer">("");
  const scroller = useRef<HTMLDivElement>(null);
  const anchor = useRef<{ height: number; top: number } | null>(null);
  const placed = useRef(false);
  const base = `/lark/conversations/${enc(feed)}/messages`;
  const topic = Boolean(item.lark?.is_topic);
  const tracker = useRef<LarkReadTracker | null>(null);
  const prefs = useTranslatePrefs();
  const translateTo = translatesChat(prefs, feed) ? prefs.target : undefined;

  useEffect(() => {
    let live = true;
    placed.current = false;
    tracker.current = null;
    setPage(null);
    setError("");
    api<LarkPage>(`${base}?limit=${PAGE}${focus ? `&around=${enc(focus)}` : ""}`)
      .then((r) => {
        if (!live) return;
        const messages = mergeMessages([], r.messages ?? []);
        tracker.current = new LarkReadTracker(initialCursor(focus, messages, topic));
        setPage({ ...r, messages });
      })
      .catch((e) => live && setError(errorText(e)));
    return () => {
      live = false;
    };
  }, [base, focus, topic]);

  // New messages in this chat (the hub's `lark` push names the chats): read the newest page again when we show it.
  const pageRef = useRef(page);
  pageRef.current = page;
  useEffect(
    () =>
      onHubEvent("lark", (msg) => {
        const ids = Array.isArray(msg.feed_ids) ? (msg.feed_ids as string[]) : [];
        const p = pageRef.current;
        if (!ids.includes(feed) || !p || p.has_newer) return;
        api<LarkPage>(`${base}?limit=${PAGE}`)
          .then((r) => setPage((cur) => cur && { ...cur, messages: mergeMessages(cur.messages, r.messages ?? []) }))
          .catch(() => {});
      }),
    [base, feed],
  );

  useReadReports(feed, scroller, tracker, pageRef, placed, page);

  // First paint: at the first new message (a little of what came before shows above it), else at the bottom.
  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el || !page) return;
    if (anchor.current) {
      el.scrollTop = anchor.current.top + (el.scrollHeight - anchor.current.height);
      anchor.current = null;
      return;
    }
    if (placed.current) return;
    placed.current = true;
    const target = focus ? el.querySelector<HTMLElement>(`[data-pos="${CSS.escape(focus)}"]`) : null;
    if (target) el.scrollTop = Math.max(0, target.offsetTop - 64);
    else el.scrollTop = el.scrollHeight;
  }, [page, focus]);

  const loadMore = useCallback(async (dir: "older" | "newer") => {
    const p = pageRef.current;
    const el = scroller.current;
    if (!p) return;
    const cursor = dir === "older" ? p.older_cursor : p.newer_cursor;
    setMore(dir);
    try {
      const r = await api<LarkPage>(`${base}?limit=${PAGE}&${dir === "older" ? "before" : "after"}=${enc(cursor ?? "")}`);
      if (dir === "older" && el) anchor.current = { height: el.scrollHeight, top: el.scrollTop };
      setPage((cur) => cur && {
        ...cur,
        messages: mergeMessages(cur.messages, r.messages ?? []),
        ...(dir === "older" ? { has_older: r.has_older, older_cursor: r.older_cursor } : { has_newer: r.has_newer, newer_cursor: r.newer_cursor }),
      });
    } catch (e) {
      setError(errorText(e));
    } finally {
      setMore("");
    }
  }, [base]);

  if (!page) {
    return <div className="p-5">{error ? <p className="text-[12.5px] text-red">{error}</p> : <Loading label="正在读飞书消息" />}</div>;
  }
  const firstNew = focus ? page.messages.find((m) => comparePosition(m.position, focus) >= 0)?.id : undefined;
  return (
    <div ref={scroller} className="h-full overflow-y-auto">
      <div className="mx-auto max-w-[760px] px-2 py-3 sm:px-4">
        {page.has_older && (
          <div className="flex justify-center pb-2">
            <Button tone="quiet" busy={more === "older"} onClick={() => void loadMore("older")}>更早的消息</Button>
          </div>
        )}
        {!page.messages.length && <p className="py-10 text-center text-[12.5px] text-ink-3">没有消息</p>}
        {page.messages.map((m, i) => (
          <div key={m.id}>
            {m.id === firstNew && (
              <div className="my-2 flex items-center gap-2 px-2 text-[11px] font-medium text-accent-ink">
                <span className="h-px flex-1 bg-accent/40" />
                以下是新消息
                <span className="h-px flex-1 bg-accent/40" />
              </div>
            )}
            <Message m={m} prev={m.id === firstNew ? undefined : page.messages[i - 1]} translateTo={translateTo} />
          </div>
        ))}
        {page.has_newer && (
          <div className="flex justify-center pt-2">
            <Button tone="quiet" busy={more === "newer"} onClick={() => void loadMore("newer")}>更新的消息</Button>
          </div>
        )}
        {error && <p className="px-2 pt-2 text-[12px] text-red">{error}</p>}
      </div>
    </div>
  );
}

/** Feishu read state from what reached the screen: the list's messages (not the opened threads') are watched;
 * seen ones are reported 450 ms after the last one came into view, and when the page goes to the background or closes. */
function useReadReports(
  feed: string,
  scroller: React.RefObject<HTMLDivElement | null>,
  tracker: React.RefObject<LarkReadTracker | null>,
  pageRef: React.RefObject<LarkPage | null>,
  placed: React.RefObject<boolean>,
  page: LarkPage | null,
) {
  const inView = useRef(new Set<string>());
  const observer = useRef<IntersectionObserver | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const ready = page !== null;

  useEffect(() => {
    const root = scroller.current;
    if (!ready || !root) return;
    const flush = () => {
      clearTimeout(timer.current);
      const t = tracker.current;
      const p = pageRef.current;
      if (!t || !p) return;
      const previous = t.cursor;
      const report = t.report(p.messages);
      if (!report) return;
      api(`/lark/conversations/${enc(feed)}/read`, { method: "POST", json: report, token: getToken() }).catch(() => t.undo(report, previous));
    };
    // The gate (docs/redesign/ux.md 3.5): in front, placed, nothing covering the list.
    const look = () => {
      const t = tracker.current;
      if (!t || !placed.current || document.visibilityState !== "visible" || document.querySelector('[aria-modal="true"]')) return;
      let fresh = false;
      for (const pos of inView.current) if (t.see(pos)) fresh = true;
      if (!fresh) return;
      clearTimeout(timer.current);
      timer.current = setTimeout(flush, 450);
    };
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          const pos = (e.target as HTMLElement).dataset.seenPos;
          if (!pos) continue;
          const enough = e.isIntersecting && (e.intersectionRatio >= 0.5 || (e.rootBounds !== null && e.intersectionRect.height >= e.rootBounds.height * 0.5));
          if (enough) inView.current.add(pos);
          else inView.current.delete(pos);
        }
        look();
      },
      // Every tenth: a message taller than the list still passes "half the list" as it scrolls through.
      { root, threshold: Array.from({ length: 11 }, (_, i) => i / 10) },
    );
    observer.current = io;
    root.querySelectorAll<HTMLElement>("[data-seen-pos]").forEach((el) => io.observe(el));
    const onVisibility = () => (document.visibilityState === "visible" ? look() : flush());
    document.addEventListener("visibilitychange", onVisibility);
    // A dialog over the page (AI 草拟回复, 交给 AI) is portaled into <body>: look again once it is gone.
    const dialogs = new MutationObserver(look);
    dialogs.observe(document.body, { childList: true });
    const seen = inView.current;
    return () => {
      io.disconnect();
      observer.current = null;
      dialogs.disconnect();
      document.removeEventListener("visibilitychange", onVisibility);
      flush();
      seen.clear();
    };
  }, [feed, ready, scroller, tracker, pageRef, placed]);

  // Messages added later (older / newer pages, pushes) are watched too.
  useEffect(() => {
    const io = observer.current;
    scroller.current?.querySelectorAll<HTMLElement>("[data-seen-pos]").forEach((el) => io?.observe(el));
  }, [page, scroller]);
}

function isSystem(m: LarkMessage) {
  return m.type === "SYSTEM" || (m.parts ?? []).some((p) => p.kind === "text" && p.metadata?.system);
}

/** 设置 › 翻译 for this chat: the translation of someone else's message in another language, asked for when it shows. */
function TranslationLine({ m, target }: { m: LarkMessage; target: string }) {
  const text = m.text?.trim() ?? "";
  const applies = !m.is_mine && !m.recalled && text !== "" && isForeignCached(text, target);
  useTranslations();
  useEffect(() => {
    if (applies) requestTranslation(text, target);
  }, [applies, text, target]);
  const t = applies ? translationOf(text, target) : undefined;
  if (!t) return null;
  return (
    <p className="mt-1 flex items-start gap-1.5 text-[12.5px] leading-relaxed text-ink-2" aria-label={`译文：${t}`}>
      <Icon name="translate" size={12} className="mt-[3px] shrink-0 text-ink-3" />
      <span className="min-w-0 break-words whitespace-pre-wrap">{t}</span>
    </p>
  );
}

/** One message: who and when (left out under the same sender's message a moment before), what it answers, the parts. */
function Message({ m, prev, nested, translateTo }: { m: LarkMessage; prev?: LarkMessage; nested?: boolean; translateTo?: string }) {
  const [thread, setThread] = useState<LarkMessage[] | null>(null);
  const [opening, setOpening] = useState(false);
  if (isSystem(m)) {
    return <p data-pos={m.position} data-seen-pos={nested ? undefined : m.position} className="px-2 py-1.5 text-center text-[11.5px] text-ink-3">{m.text || (m.parts ?? []).map((p) => p.text).join("")}</p>;
  }
  const name = m.is_mine ? "我" : m.sender_name || "未知";
  const compact = !nested && prev && !isSystem(prev) && prev.sender_id === m.sender_id && !m.reply_to
    && Date.parse(m.created_at ?? "") - Date.parse(prev.created_at ?? "") < 5 * 60_000;
  const replies = m.thread_reply_count || m.reply_count || 0;
  const openThread = async () => {
    if (thread) return setThread(null);
    setOpening(true);
    try {
      const r = await api<LarkPage>(`/lark/messages/${enc(m.id)}/thread?limit=50`);
      setThread(mergeMessages([], r.messages ?? []));
    } catch {
      setThread([]);
    } finally {
      setOpening(false);
    }
  };
  return (
    <div data-pos={m.position} data-seen-pos={nested ? undefined : m.position} className={cx("flex gap-2.5 rounded-[10px] px-2", compact ? "py-0.5" : "pt-2 pb-0.5", m.at_me && "bg-accent-tint/60")}>
      <span className="w-8 shrink-0">{!compact && <Monogram name={name} size={nested ? 24 : 32} src={m.sender_avatar} />}</span>
      <div className="min-w-0 flex-1">
        {!compact && (
          <p className="flex items-baseline gap-2">
            <span className={cx("truncate text-[12.5px] font-medium", m.is_mine ? "text-accent-ink" : "text-ink")}>{name}</span>
            {m.sender_type === "bot" && <span className="shrink-0 rounded-[4px] bg-field px-1 text-[10px] text-ink-3">机器人</span>}
            <span className="shrink-0 text-[11px] text-ink-3 tabular-nums">{clock(m.created_at)}</span>
            {m.edited && <span className="shrink-0 text-[11px] text-ink-3">已编辑</span>}
          </p>
        )}
        {m.reply_to && (
          <p className="mt-0.5 truncate border-l-2 border-line-strong pl-2 text-[12px] text-ink-3">
            {m.reply_to.sender_name ? `${m.reply_to.sender_name}：` : ""}{m.reply_to.recalled ? "此消息已撤回" : m.reply_to.text}
          </p>
        )}
        {m.recalled ? <p className="text-[13px] text-ink-3 italic">此消息已撤回</p> : <Parts parts={m.parts?.length ? m.parts : [{ kind: "text", text: m.text ?? "" }]} />}
        {translateTo && <TranslationLine m={m} target={translateTo} />}
        {replies > 0 && !nested && (
          <button type="button" onClick={() => void openThread()} className="mt-1 flex items-center gap-1 text-[11.5px] font-medium whitespace-nowrap text-accent-ink hover:underline">
            {opening ? <Spinner size={10} /> : <Icon name="chat" size={11} stroke={2.2} />}
            {thread ? "收起回复" : `${replies} 条回复`}
          </button>
        )}
        {thread && (
          <div className="mt-1 border-l-2 border-line pl-1">
            {thread.length ? thread.map((t) => <Message key={t.id} m={t} nested translateTo={translateTo} />) : <p className="px-2 py-1 text-[11.5px] text-ink-3">读不到回复</p>}
          </div>
        )}
      </div>
    </div>
  );
}

/** A picture of the hub's (a relative /api/ URL with the token) at full size in a tab of its own. */
export function openMedia(src: string) {
  window.open(src, "_blank", "noopener,noreferrer");
}

function safeURL(u?: string) {
  if (!u) return undefined;
  if (u.startsWith("/api/")) return withToken(u);
  return /^(https?:|mailto:)/i.test(u) ? u : undefined;
}

function LinkText({ url, children }: { url?: string; children: React.ReactNode }) {
  const safe = safeURL(url);
  if (!safe) return <>{children}</>;
  return (
    <a
      href={linkHref(safe)}
      target="_blank"
      rel="noopener noreferrer"
      onClick={(e) => clickLink(e, safe, typeof children === "string" ? children : undefined)}
      onMouseEnter={() => prefetchLink(safe)}
      className="text-accent-ink underline decoration-accent-ink/35 underline-offset-[3px] hover:decoration-accent-ink"
    >
      {children}
    </a>
  );
}

function Parts({ parts }: { parts: LarkPart[] }) {
  return (
    <div className="flex flex-col gap-1.5 text-[13.5px] leading-relaxed text-ink">
      {partRuns(parts).map((run, i) =>
        run.inline ? (
          <p key={i} className="break-words whitespace-pre-wrap">
            {run.parts.map((p, j) => <Inline key={j} p={p} />)}
          </p>
        ) : (
          <Block key={i} p={run.parts[0]} />
        ),
      )}
    </div>
  );
}

function Inline({ p }: { p: LarkPart }) {
  switch (p.kind) {
    case "at":
      return <span className="font-medium text-accent-ink">{p.text || "@"}</span>;
    case "link":
      return <LinkText url={p.url}>{p.text || p.url}</LinkText>;
    case "docs":
      return (
        <LinkText url={p.url}>
          <Icon name="read" size={12} className="mr-0.5 inline-block align-[-1px]" />
          {p.text || "文档"}
        </LinkText>
      );
    case "emotion":
      return <>{emojiLabel(p.refId ?? p.text ?? "", p.text)}</>;
    default:
      return <>{p.text}</>;
  }
}

function Block({ p }: { p: LarkPart }) {
  const meta = p.metadata ?? {};
  switch (p.kind) {
    case "image": {
      const src = safeURL(p.url);
      return src ? (
        <button type="button" onClick={() => openMedia(src)} className="w-fit">
          <img src={src} alt="图片" loading="lazy" className="max-h-64 max-w-[min(280px,100%)] rounded-[8px] object-contain shadow-hairline" />
        </button>
      ) : <span className="text-ink-3">[图片]</span>;
    }
    case "emotion": {
      const src = safeURL(p.url);
      return src ? <img src={src} alt={p.text || "表情"} loading="lazy" className="size-20 object-contain" /> : <span>{emojiLabel(p.refId ?? "", p.text)}</span>;
    }
    case "file":
    case "media": {
      const media = p.kind === "media" ? (String(meta.mime ?? "").startsWith("audio/") ? "语音" : "视频") : "";
      const name = p.text || (typeof meta.name === "string" ? meta.name : "") || (media || "文件");
      return (
        <span className="flex w-fit max-w-full items-center gap-2 rounded-[8px] bg-inset px-2.5 py-1.5 text-[12.5px] shadow-hairline">
          <Icon name={p.kind === "media" ? "play" : "clip"} size={13} className="shrink-0 text-ink-3" />
          <span className="truncate">{media && name !== media ? `${media} · ${name}` : name}</span>
        </span>
      );
    }
    case "code":
      return <pre className="overflow-x-auto rounded-[8px] bg-inset px-3 py-2 font-mono text-[12px] leading-relaxed shadow-hairline">{p.text}</pre>;
    case "card": {
      const c = cardText(meta);
      return (
        <div className="max-w-[460px] rounded-[10px] bg-surface px-3 py-2.5 shadow-card">
          {c.title && <p className="text-[13px] font-semibold text-ink">{c.title}</p>}
          {c.lines.map((l, i) => <p key={i} className="text-[12.5px] whitespace-pre-wrap text-ink-2">{l}</p>)}
          {!c.title && !c.lines.length && <p className="text-[12.5px] text-ink-3">{p.text || "[卡片]"}</p>}
        </div>
      );
    }
    case "interactive":
      return <hr className="border-line" />;
    default:
      return p.text ? <p className="break-words whitespace-pre-wrap">{p.text}</p> : null;
  }
}
