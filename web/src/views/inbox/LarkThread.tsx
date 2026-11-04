// A Feishu chat, opened at the item's first new message (docs/inbox.md: 「点开：飞书会话停在这件事的第一条新消息」).
// Supports native Feishu messaging interactions: direct replies, quoting messages, copying, asking Persona for summary,
// and reporting seen messages to Feishu (docs/inbox.md「看过的飞书消息按可见即读上报」, lark-read.ts). The chat's Pins
// (lark-pins.ts) open over it; a Pin found in the chat replaces the window around it, a reply opens its thread.
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { api, enc, errorText, getToken } from "../../lib/api";
import { clock } from "../../lib/format";
import {
  cardText, comparePosition, emojiLabel, isDone, mergeMessages, nextLarkPosition, partRuns, replyLarkMessage,
  sendLarkMessage, withToken, type InboxItem, type LarkMessage, type LarkPage, type LarkPart,
} from "../../lib/inbox";
import { larkAccount } from "../../lib/lark-chats";
import { canAddPin, changeChatPin, needsRecheck, pinProblemText, type ChatPinTarget } from "../../lib/lark-pins";
import { initialCursor, LarkReadTracker } from "../../lib/lark-read";
import { clickLink, linkHref, prefetchLink } from "../../lib/links";
import { navigate } from "../../lib/router";
import { onHubEvent } from "../../lib/store";
import { isForeignCached, requestTranslation, translatesChat, translationOf, useTranslatePrefs, useTranslations } from "../../lib/translate";
import type { Session } from "../../lib/types";
import { Button, cx, Dialog, Loading, Spinner, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Monogram } from "./InboxParts";
import { LarkPinsDialog } from "./LarkPins";

const PAGE = 30;

export interface LarkReplyTarget {
  id: string;
  sender_name?: string;
  text?: string;
}

/** The chat a LarkThread shows: from an inbox item, or a row of 消息. */
export interface LarkChatRef {
  feed: string;
  title: string;
  topic?: boolean;
  /** Open at this position (the first new message) instead of the newest. */
  focus?: string;
}

export function chatOfItem(item: InboxItem): LarkChatRef {
  return { feed: item.lark!.feed_id, title: item.title, topic: item.lark?.is_topic, focus: !isDone(item) ? item.lark?.start_position : undefined };
}

export function LarkThread({ chat, pinsOpen = false, onPinsOpen }: { chat: LarkChatRef; pinsOpen?: boolean; onPinsOpen?: (open: boolean) => void }) {
  const feed = chat.feed;
  const focus = chat.focus;
  const [page, setPage] = useState<LarkPage | null>(null);
  const [error, setError] = useState("");
  const [more, setMore] = useState<"" | "older" | "newer">("");
  const [replyTo, setReplyTo] = useState<LarkReplyTarget | null>(null);
  const scroller = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const anchor = useRef<{ height: number; top: number } | null>(null);
  const placed = useRef(false);
  const base = `/lark/conversations/${enc(feed)}/messages`;
  const topic = Boolean(chat.topic);
  const tracker = useRef<LarkReadTracker | null>(null);
  const [flash, setFlash] = useState<{ id: string; n: number } | null>(null);
  const jumpTo = useRef<string | null>(null);
  const [pinThread, setPinThread] = useState<ChatPinTarget | null>(null);
  const [adding, setAdding] = useState<LarkMessage | null>(null);
  const [changingPin, setChangingPin] = useState(false);
  const [pinRecheck, setPinRecheck] = useState(false);
  const prefs = useTranslatePrefs();
  const translateTo = translatesChat(prefs, feed) ? prefs.target : undefined;

  useEffect(() => {
    let live = true;
    placed.current = false;
    tracker.current = null;
    setPage(null);
    setError("");
    setReplyTo(null);
    setFlash(null);
    setPinThread(null);
    api<LarkPage>(`${base}?limit=${PAGE}${focus ? `&around=${enc(focus)}` : ""}`)
      .then((r) => {
        if (!live) return;
        const messages = mergeMessages([], r.messages ?? []);
        tracker.current = new LarkReadTracker(initialCursor(focus, messages, topic || Boolean(r.topic)));
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
    if (jumpTo.current) {
      const target = el.querySelector<HTMLElement>(`[data-mid="${CSS.escape(jumpTo.current)}"]`);
      jumpTo.current = null;
      if (target) el.scrollTop = Math.max(0, target.offsetTop - el.clientHeight / 3);
      return;
    }
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

  const handleReply = useCallback((m: LarkMessage) => {
    setReplyTo({
      id: m.id,
      sender_name: m.is_mine ? "我" : (m.sender_name || "未知"),
      text: m.text || (m.parts ?? []).map((p) => p.text).join(""),
    });
    inputRef.current?.focus();
  }, []);

  // A Pin found by the hub (lark-pins.ts locate): a reply opens its thread; a message replaces the window around it.
  const openPin = useCallback((target: ChatPinTarget) => {
    onPinsOpen?.(false);
    if (target.root) {
      setPinThread(target);
      return;
    }
    placed.current = true;
    anchor.current = null;
    jumpTo.current = target.message.id;
    setPage((cur) => ({ ...(cur ?? {}), ...target.page, messages: mergeMessages([], target.page.messages ?? []) }));
    setFlash((f) => ({ id: target.message.id, n: (f?.n ?? 0) + 1 }));
  }, [onPinsOpen]);

  const addPin = useCallback(async (m: LarkMessage) => {
    setAdding(null);
    setChangingPin(true);
    try {
      await changeChatPin(feed, m.id, undefined, await larkAccount());
      toast("已添加 Pin", "ok");
    } catch (e) {
      const recheck = needsRecheck(e);
      setPinRecheck(recheck);
      toast(pinProblemText(e), "error");
      if (recheck) onPinsOpen?.(true);
    } finally {
      setChangingPin(false);
    }
  }, [feed, onPinsOpen]);

  // Closing the list after it read cleanly settles an unknown add.
  const closePins = useCallback((checked: boolean) => {
    if (checked) setPinRecheck(false);
    onPinsOpen?.(false);
  }, [onPinsOpen]);

  const handleAskPersona = useCallback(async () => {
    const chatTitle = chat.title || "飞书会话";
    const prompt = `读取我飞书会话「${chatTitle}」（lark-connector feedId ${feed}）最近的消息，总结要点，列出需要我回复或处理的事，并给出建议的回复。`;
    toast("正在交给 Persona 总结…", "ok");
    try {
      const r = await api<{ session: Session }>("/assistant/conversations", {
        method: "POST",
        json: { prompt },
      });
      navigate({ name: "jarvis", sid: r.session.session_id });
    } catch (e) {
      toast(`总结失败：${errorText(e)}`, "error");
    }
  }, [chat.title, feed]);

  const handleCopy = useCallback((m: LarkMessage) => {
    const text = m.text || (m.parts ?? []).map((p) => p.text ?? "").join("");
    if (text) {
      navigator.clipboard?.writeText(text).then(() => {
        toast("已拷贝", "ok");
      }).catch(() => {
        toast("拷贝失败", "error");
      });
    }
  }, []);

  const handleSend = useCallback(async (content: string, replyTarget?: LarkReplyTarget) => {
    const clientID = "wmsg-" + Date.now().toString(36) + "-" + Math.random().toString(36).slice(2, 6);
    const tempId = "temp-" + clientID;
    const currentMessages = pageRef.current?.messages ?? [];
    const optMsg: LarkMessage = {
      id: tempId,
      client_id: clientID,
      position: nextLarkPosition(currentMessages),
      is_mine: true,
      sender_name: "我",
      created_at: new Date().toISOString(),
      text: content,
      parts: [{ kind: "text", text: content }],
      sending: true,
      reply_to: replyTarget ? { id: replyTarget.id, sender_name: replyTarget.sender_name, text: replyTarget.text } : undefined,
    };

    setPage((cur) => cur && { ...cur, messages: mergeMessages(cur.messages, [optMsg]) });
    setReplyTo(null);
    setTimeout(() => {
      const el = scroller.current;
      if (el) el.scrollTop = el.scrollHeight;
    }, 10);

    try {
      let res: { message_id?: string };
      if (replyTarget?.id) {
        res = await replyLarkMessage(replyTarget.id, content, false, clientID);
      } else {
        res = await sendLarkMessage(feed, content, clientID);
      }
      setPage((cur) => {
        if (!cur) return cur;
        const updated = cur.messages.map((m) =>
          m.id === tempId ? { ...m, id: res.message_id || tempId, sending: false, failed: false } : m,
        );
        return { ...cur, messages: updated };
      });
    } catch (e) {
      setPage((cur) => {
        if (!cur) return cur;
        const updated = cur.messages.map((m) =>
          m.id === tempId ? { ...m, sending: false, failed: true } : m,
        );
        return { ...cur, messages: updated };
      });
      toast(`发送失败：${errorText(e)}`, "error");
    }
  }, [feed]);

  const handleRetry = useCallback(async (m: LarkMessage) => {
    const clientID = m.client_id || ("wmsg-" + Date.now().toString(36));
    setPage((cur) => {
      if (!cur) return cur;
      const updated = cur.messages.map((msg) =>
        msg.id === m.id ? { ...msg, sending: true, failed: false } : msg,
      );
      return { ...cur, messages: updated };
    });
    try {
      let res: { message_id?: string };
      if (m.reply_to?.id) {
        res = await replyLarkMessage(m.reply_to.id, m.text ?? "", false, clientID);
      } else {
        res = await sendLarkMessage(feed, m.text ?? "", clientID);
      }
      setPage((cur) => {
        if (!cur) return cur;
        const updated = cur.messages.map((msg) =>
          msg.id === m.id ? { ...msg, id: res.message_id || msg.id, sending: false, failed: false } : msg,
        );
        return { ...cur, messages: updated };
      });
    } catch (e) {
      setPage((cur) => {
        if (!cur) return cur;
        const updated = cur.messages.map((msg) =>
          msg.id === m.id ? { ...msg, sending: false, failed: true } : msg,
        );
        return { ...cur, messages: updated };
      });
      toast(`重试失败：${errorText(e)}`, "error");
    }
  }, [feed]);

  if (!page) {
    return <div className="p-5">{error ? <p className="text-[12.5px] text-red">{error}</p> : <Loading label="正在读飞书消息" />}</div>;
  }
  const firstNew = focus ? page.messages.find((m) => comparePosition(m.position, focus) >= 0)?.id : undefined;
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div ref={scroller} className="min-h-0 flex-1 overflow-y-auto">
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
              <Message
                m={m}
                prev={m.id === firstNew ? undefined : page.messages[i - 1]}
                translateTo={translateTo}
                onReply={handleReply}
                onAskPersona={handleAskPersona}
                onCopy={handleCopy}
                onRetry={handleRetry}
                onPin={onPinsOpen && !changingPin && !pinRecheck ? setAdding : undefined}
                flash={flash?.id === m.id ? flash.n : undefined}
              />
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
      <LarkComposer
        replyTo={replyTo}
        onClearReply={() => setReplyTo(null)}
        onSend={handleSend}
        inputRef={inputRef}
      />
      {pinsOpen && <LarkPinsDialog feed={feed} topic={topic} onClose={closePins} onOpen={openPin} />}
      {pinThread && <PinThreadDialog target={pinThread} translateTo={translateTo} onClose={() => setPinThread(null)} />}
      <Dialog
        open={adding !== null}
        onClose={() => setAdding(null)}
        title="添加到 Pin 消息？"
        footer={(
          <>
            <Button tone="quiet" onClick={() => setAdding(null)}>取消</Button>
            <Button tone="accent" icon="pin" onClick={() => adding && void addPin(adding)}>添加 Pin</Button>
          </>
        )}
      >
        <p className="text-[12.5px] leading-relaxed text-ink-2">会话成员可以在 Pin 列表中找到这条消息。</p>
        {adding && <p className="mt-2 line-clamp-3 rounded-[8px] bg-inset px-3 py-2 text-[12.5px] text-ink shadow-hairline">{adding.text || (adding.parts ?? []).map((p) => p.text ?? "").join("") || "消息"}</p>}
      </Dialog>
    </div>
  );
}

/** A pinned reply in its thread (iOS opens LarkThreadView): the thread's first message, then the replies the hub read
 * up to the pinned one, which is brought into view and lit for a moment. */
function PinThreadDialog({ target, translateTo, onClose }: { target: ChatPinTarget; translateTo?: string; onClose: () => void }) {
  const box = useRef<HTMLDivElement>(null);
  const root = target.root!;
  const replies = mergeMessages([], (target.page.messages ?? []).filter((m) => m.id !== root.id));
  useLayoutEffect(() => {
    box.current?.querySelector<HTMLElement>(`[data-mid="${CSS.escape(target.message.id)}"]`)?.scrollIntoView({ block: "center" });
  }, [target]);
  return (
    <Dialog open onClose={onClose} title="话题" width={620}>
      <div ref={box} className="-mx-2 max-h-[60vh] overflow-y-auto">
        <Message m={root} nested translateTo={translateTo} flash={root.id === target.message.id ? 1 : undefined} />
        <p className="mx-2 my-2 border-t border-line pt-2 text-[11.5px] font-medium text-ink-3">{replies.length} 条回复</p>
        {replies.map((m) => <Message key={m.id} m={m} nested translateTo={translateTo} flash={m.id === target.message.id ? 1 : undefined} />)}
      </div>
    </Dialog>
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

/** One message: who and when, quoted reply, the content, hover action bar, and retry for failed sends. */
function Message({
  m,
  prev,
  nested,
  translateTo,
  onReply,
  onAskPersona,
  onCopy,
  onRetry,
  onPin,
  flash,
}: {
  m: LarkMessage;
  prev?: LarkMessage;
  nested?: boolean;
  translateTo?: string;
  onReply?: (m: LarkMessage) => void;
  onAskPersona?: () => void;
  onCopy?: (m: LarkMessage) => void;
  onRetry?: (m: LarkMessage) => void;
  /** 添加 Pin, offered when the hub says the message may be pinned. */
  onPin?: (m: LarkMessage) => void;
  /** Lit for a moment (a Pin jumped here); a new number lights it again. */
  flash?: number;
}) {
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
    <div
      data-pos={m.position}
      data-mid={m.id}
      data-seen-pos={nested ? undefined : m.position}
      className={cx(
        "group/msg relative flex gap-2.5 rounded-[10px] px-2",
        compact ? "py-0.5" : "pt-2 pb-0.5",
        m.at_me && "bg-accent-tint/60",
        flash !== undefined && "isolate",
      )}
    >
      {flash !== undefined && <span key={flash} aria-hidden className="pointer-events-none absolute inset-0 -z-10 rounded-[10px] bg-accent-tint" style={{ animation: "pin-flash 1.8s ease-out both" }} />}
      <span className="w-8 shrink-0">{!compact && <Monogram name={name} size={nested ? 24 : 32} src={m.sender_avatar} />}</span>
      <div className="min-w-0 flex-1">
        {!compact && (
          <p className="flex items-baseline gap-2">
            <span className={cx("truncate text-[12.5px] font-medium", m.is_mine ? "text-accent-ink" : "text-ink")}>{name}</span>
            {m.sender_type === "bot" && <span className="shrink-0 rounded-[4px] bg-field px-1 text-[10px] text-ink-3">机器人</span>}
            <span className="shrink-0 text-[11px] text-ink-3 tabular-nums">{clock(m.created_at)}</span>
            {m.edited && <span className="shrink-0 text-[11px] text-ink-3">已编辑</span>}
            {m.sending && (
              <span className="flex items-center gap-1 text-[11px] text-ink-3">
                <Spinner size={10} />
                <span>发送中…</span>
              </span>
            )}
            {m.failed && (
              <button
                type="button"
                onClick={() => onRetry?.(m)}
                className="flex items-center gap-1 text-[11px] font-medium text-red hover:underline"
                title="点击重试"
              >
                <Icon name="retry" size={11} />
                <span>发送失败，点击重试</span>
              </button>
            )}
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

      {/* Hover action bar */}
      {!isSystem(m) && !m.recalled && !m.sending && (
        <div className="absolute right-2 top-1.5 z-10 hidden items-center gap-0.5 rounded-[6px] border border-line bg-surface/95 px-1 py-0.5 shadow-hairline backdrop-blur-sm group-hover/msg:flex">
          {onReply && (
            <button
              type="button"
              onClick={() => onReply(m)}
              className="flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] text-ink-2 hover:bg-surface-hover hover:text-ink"
              title="回复"
            >
              <Icon name="quote" size={11} />
              <span>回复</span>
            </button>
          )}
          {onAskPersona && !nested && (
            <button
              type="button"
              onClick={onAskPersona}
              className="flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] text-accent-ink hover:bg-accent-tint/40"
              title="让 Persona 总结"
            >
              <Icon name="spark" size={11} />
              <span>总结</span>
            </button>
          )}
          {onPin && !nested && canAddPin(m) && (
            <button
              type="button"
              onClick={() => onPin(m)}
              className="flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] whitespace-nowrap text-ink-2 hover:bg-surface-hover hover:text-ink"
              title="添加 Pin"
            >
              <Icon name="pin" size={11} />
              <span>Pin</span>
            </button>
          )}
          {onCopy && (
            <button
              type="button"
              onClick={() => onCopy(m)}
              className="flex items-center rounded p-1 text-ink-3 hover:bg-surface-hover hover:text-ink"
              title="拷贝"
            >
              <Icon name="copy" size={11} />
            </button>
          )}
        </div>
      )}
    </div>
  );
}

/** Bottom composer for native Feishu chat replies. */
function LarkComposer({
  replyTo,
  onClearReply,
  onSend,
  inputRef,
}: {
  replyTo: LarkReplyTarget | null;
  onClearReply: () => void;
  onSend: (text: string, replyToMessage?: LarkReplyTarget) => Promise<void>;
  inputRef: React.RefObject<HTMLTextAreaElement | null>;
}) {
  const [text, setText] = useState("");
  const [sending, setSending] = useState(false);

  const submit = async () => {
    const trimmed = text.trim();
    if (!trimmed || sending) return;
    setSending(true);
    const toReply = replyTo ?? undefined;
    setText("");
    if (inputRef.current) inputRef.current.style.height = "auto";
    try {
      await onSend(trimmed, toReply);
    } finally {
      setSending(false);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey) {
      if (e.nativeEvent.isComposing) return;
      e.preventDefault();
      void submit();
    }
  };

  const handleChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setText(e.target.value);
    const el = e.target;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 120)}px`;
  };

  return (
    <div className="shrink-0 border-t border-line bg-surface px-3 py-2 sm:px-4">
      <div className="mx-auto max-w-[760px]">
        {replyTo && (
          <div className="mb-2 flex items-center justify-between rounded-[8px] bg-field/60 px-3 py-1.5 text-[12px] text-ink-2">
            <div className="flex min-w-0 items-center gap-1.5">
              <Icon name="quote" size={12} className="shrink-0 text-accent-ink" />
              <span className="shrink-0 font-medium text-ink">{replyTo.sender_name}：</span>
              <span className="truncate text-ink-3">{replyTo.text || "引用消息"}</span>
            </div>
            <button
              type="button"
              onClick={onClearReply}
              className="ml-2 shrink-0 rounded p-0.5 text-ink-3 hover:bg-surface hover:text-ink"
              title="取消引用"
            >
              <Icon name="x" size={12} />
            </button>
          </div>
        )}
        <div className="flex items-end gap-2">
          <textarea
            ref={inputRef}
            value={text}
            onChange={handleChange}
            onKeyDown={handleKeyDown}
            placeholder={replyTo ? `回复 ${replyTo.sender_name}…` : "回复飞书消息…"}
            rows={1}
            className="min-h-[36px] max-h-[120px] flex-1 resize-none rounded-[10px] border border-line bg-field px-3 py-1.5 text-[13px] leading-relaxed text-ink placeholder:text-ink-3 focus:border-accent focus:outline-none"
          />
          <Button
            tone="accent"
            busy={sending}
            disabled={!text.trim() || sending}
            onClick={() => void submit()}
            className="h-[36px] shrink-0"
          >
            发送
          </Button>
        </div>
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
