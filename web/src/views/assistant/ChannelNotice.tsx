import { useState } from "react";
import { api } from "../../lib/api";
import type { FeedItem } from "../../lib/feed";
import { t } from "../../i18n";

function sourceLink(raw: unknown): string | undefined {
  if (typeof raw !== "string" || !raw.startsWith("https://applink.feishu.cn/client/chat/open?") || raw.includes("#")) return;
  const link = new URL(raw);
  const keys = ["chatId", "messageId"];
  if ([...link.searchParams.keys()].length !== keys.length) return;
  if (keys.every(key => link.searchParams.getAll(key).length === 1 && link.searchParams.get(key))) return link.href;
}

export function ChannelNotice({ item }: { item: FeedItem }) {
  const [sending, setSending] = useState(false);
  const [outcome, setOutcome] = useState<"sent" | "unconfirmed">();
  const { extension, channel, source_url, text, forward, kind } = item.body ?? {};
  if (typeof extension !== "string" || typeof channel !== "string" || typeof text !== "string" || typeof forward !== "string") return null;
  const link = sourceLink(source_url);
  async function send() {
    if (sending || !window.confirm(t("assistant-tasks.channel_notice_confirm"))) return;
    setSending(true);
    setOutcome(undefined);
    try {
      await api(`/assistant/feed/${encodeURIComponent(item.id)}/confirm-channel-notice`, { method: "POST", timeout: 50_000 });
      setOutcome("sent");
    } catch {
      setOutcome("unconfirmed");
    } finally {
      setSending(false);
    }
  }
  return (
    <article data-testid="channel-notice" className="w-full rounded-card border border-line bg-field px-4 py-3 text-label text-ink">
      <div className="mb-2 flex flex-wrap items-center gap-x-2 text-ink-2">
        <span title={channel}>{t("assistant-tasks.channel_notice_source", { extension })}</span>
        {link && <a href={link} target="_blank" rel="noopener noreferrer" className="underline">{t("assistant-tasks.channel_notice_original")}</a>}
      </div>
      <p className="select-text whitespace-pre-wrap [overflow-wrap:anywhere]">{text}</p>
      <details className="mt-2 text-ink-2">
        <summary className="cursor-pointer">{t("assistant-tasks.channel_notice_context")}</summary>
        <p className="mt-2 select-text whitespace-pre-wrap [overflow-wrap:anywhere]">{forward}</p>
      </details>
      {kind === "grounded_reply" && (
        <div className="mt-2 flex flex-wrap items-center gap-2">
          <button type="button" className="rounded-control border border-line px-3 py-1.5 disabled:opacity-60"
            onClick={() => void send()} disabled={sending || outcome === "sent"}>
            {t("assistant-tasks.channel_notice_send")}
          </button>
          {outcome && <span role="status">{t(outcome === "sent" ? "assistant-tasks.channel_notice_sent" : "assistant-tasks.channel_notice_unconfirmed")}</span>}
        </div>
      )}
    </article>
  );
}
