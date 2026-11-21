// A morning or evening briefing snapshot, with its own waiting, history and owner verification rows.
import { useState } from "react";
import { leaveChat, openAgent, openTask } from "../../lib/handoff";
import { conversationRoute } from "../../lib/conversation-page";
import { getState } from "../../lib/store";
import { briefBody, failedKey, handledLater, type BriefBody, type BriefRow, type FeedItem } from "../../lib/feed";
import { formatDate, t } from "../../i18n";
import { Button, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { TodoStageBar } from "../TodoStageBar";
import { AcceptRow, meta, Section } from "./Brief";
import { BriefSetupRows } from "./BriefSetup";

function More({ n, text = "assistant-brief.more" }: { n: number; text?: string }) {
  return n > 0 ? <div className="py-1 text-label text-ink-2">{t(text, { n })}</div> : null;
}

/** A link the hub gave, if it is a web address: anything else is not drawn as one. */
function webLink(link?: string): string | undefined {
  return link && /^https?:\/\//i.test(link) ? link : undefined;
}

/** Opens the chat a waiting or later row came from in the app (#12055), as iOS and Android do (#2320): an Agent's as the
 *  task rows open theirs (How Agents open: the side panel, or its page with the way back to this conversation), a
 *  Persona conversation as its page. */
function openChat(mid: string, sid: string) {
  const s = getState();
  const to = conversationRoute(mid, sid, s.sessions, s.assistant);
  if (to.name === "session") openAgent(mid, sid);
  else leaveChat(to, false);
}

/** Open on a waiting or later row: the chat it came from, in the app; from a hub that does not name it, the row's link
 *  in a new tab, as before. */
function RowOpen({ row }: { row: BriefRow }) {
  const title = t("assistant-brief.open_row", { title: row.title });
  const { machine_id: mid, session_id: sid } = row;
  if (mid && sid) return <Button title={title} onClick={() => openChat(mid, sid)}>{t("assistant-brief.open")}</Button>;
  const link = webLink(row.link);
  return link ? <Button href={link} title={title}>{t("assistant-brief.open")}</Button> : null;
}

function WaitingRow({ row }: { row: BriefRow }) {
  return (
    <div className="@container" data-brief-row="waiting">
      {/* Narrow: the words take the whole width and the link drops under them, as in the Accept row (#5569) */}
      <div className="flex items-center gap-3 py-1.5 @max-md:flex-wrap @max-md:gap-y-2">
        <div className="min-w-0 flex-1 @max-md:w-full @max-md:flex-none">
          <div className="text-[13.5px] font-medium [overflow-wrap:anywhere]">{row.title}</div>
          <div className="text-label text-ink-2">{meta(row)}</div>
        </div>
        <RowOpen row={row} />
      </div>
    </div>
  );
}

/** What the Handled button of a question that can wait says: idle, the hub took it, or it was not in the queue any more. */
type Marking = "idle" | "busy" | "marked" | "handled";

function LaterRow({ row, conversation }: { row: BriefRow; conversation: string }) {
  const [state, setState] = useState<Marking>("idle");
  const mark = () => {
    if (state !== "idle") return;
    setState("busy");
    handledLater(row.id, conversation).then(
      (res) => setState(res),
      (e) => {
        setState("idle");
        toast(t("assistant-brief.later_failed", { error: String(e instanceof Error ? e.message : e) }), "error");
      },
    );
  };
  return (
    <div className="flex items-center gap-3 py-1.5" data-brief-row="later">
      <div className="min-w-0 flex-1">
        <div className="text-[13.5px] font-medium [overflow-wrap:anywhere]">{row.title}</div>
        <div className="text-label text-ink-2">{meta(row)}</div>
      </div>
      <RowOpen row={row} />
      {state === "marked" || state === "handled" ? (
        <span className="shrink-0 text-[12.5px] text-ink-2" data-brief-marked={state}>
          {t(state === "marked" ? "assistant-brief.later_marked" : "assistant-brief.handled")}
        </span>
      ) : (
        <Button busy={state === "busy"} title={t("assistant-brief.later_done_row", { title: row.title })} onClick={mark}>
          {t("assistant-brief.later_done")}
        </Button>
      )}
    </div>
  );
}

function clock(iso: string): string {
  return formatDate(iso, { hour: "2-digit", minute: "2-digit" });
}

export function BriefCard({ item }: { item: FeedItem }) {
  const body: BriefBody | undefined = briefBody(item);
  if (!body) return null;
  const past = body.failed.length + body.done.length;
  return (
    <div className="flex flex-col rounded-card bg-surface p-4 shadow-card" data-testid="brief-card">
      <div className="mb-2 flex items-center gap-1.5 text-label font-medium text-ink-2">
        <Icon name="sun" size={13} />
        {body.title || t("assistant-brief.title")}
      </div>

      {body.waiting_unknown ? (
        <Section icon="hand" tint="bg-field text-attention" title={t("assistant-brief.waiting_head", { n: "?" })}>
          <div className="py-1 text-meta text-ink-2">{t("assistant-brief.waiting_unknown")}</div>
        </Section>
      ) : (
        <Section icon="hand" tint="bg-field text-attention" title={t("assistant-brief.waiting_head", { n: body.waiting.length })}>
          {body.waiting.length === 0 && <div className="py-1 text-meta text-ink-2">{t("assistant-brief.none")}</div>}
          {body.waiting.map((r) => (
            <WaitingRow key={r.id} row={r} />
          ))}
        </Section>
      )}

      <Section icon="clock" tint="bg-accent-tint text-accent" title={t("assistant-brief.past_head", { hours: body.hours, done: body.done_total, failed: body.failed_total })}>
        {past === 0 && <div className="py-1 text-meta text-ink-2">{t("assistant-brief.none")}</div>}
        {body.failed.map((x) => (
          <div key={`${x.kind}-${x.id}-${x.at}`} className="flex items-start gap-2 py-1 text-meta" data-brief-row="failed">
            <Icon name="stop" size={11} className="mt-1.5 shrink-0 text-attention" />
            <div className="min-w-0 flex-1 [overflow-wrap:anywhere]">
              <span className="font-medium">{x.title}</span> · {t(failedKey(x), { time: clock(x.at) })}
              {x.why && <span className="text-ink-2"> — {x.why}</span>}
            </div>
          </div>
        ))}
        {body.done.map((x) => (
          <div key={x.id} className="flex items-start gap-2 py-1 text-meta" data-brief-row="done">
            <Icon name="check" size={12} className="mt-1 shrink-0 text-green" />
            <div className="min-w-0 flex-1 [overflow-wrap:anywhere]">
              <span className="font-medium">{x.title}</span> · {clock(x.at)}
            </div>
          </div>
        ))}
        <More n={body.done_total + body.failed_total - past} />
      </Section>

      <Section icon="check" tint="bg-green-tint text-green" title={t("assistant-brief.accept_head", { n: body.accept.length })}>
        {body.accept.length === 0 && <div className="py-1 text-meta text-ink-2">{t("assistant-brief.none")}</div>}
        {body.accept.map((r) => (
          <AcceptRow key={r.id + (r.asked_at ?? "")} row={r} />
        ))}
      </Section>
      {body.later.length > 0 && (
        <Section icon="chat" tint="bg-field text-ink-2" title={t("assistant-brief.later_head", { n: body.later_total })}>
          {body.later.map((r) => (
            <LaterRow key={r.id} row={r} conversation={item.conversation} />
          ))}
          <More n={body.later_total - body.later.length} text="assistant-brief.later_more" />
        </Section>
      )}
      {body.progress_total > 0 && (
        <Section icon="gear" tint="bg-orange-tint text-orange" title={t("assistant-brief.progress_head", { n: body.progress_total })}>
          {body.progress.map((p) => (
            <div key={p.id} className="flex flex-col gap-1 py-1.5" data-brief-row="progress">
              <div className="flex items-center gap-3">
                <div className="min-w-0 flex-1 text-[13.5px] font-medium [overflow-wrap:anywhere]">{p.title}</div>
                <Button onClick={() => openTask(p.id)}>{t("assistant-brief.open")}</Button>
              </div>
              <TodoStageBar stage={p.stage} stageKind={p.stage_kind} />
            </div>
          ))}
          <More n={body.progress_total - body.progress.length} />
        </Section>
      )}
      {/* The week's recommended setup (#5085): the hub's own counts and words, drawn as they came. Opening the settings
          page is all a row does: nothing is turned on from here. At most once a week, so its own chunk (#11794). */}
      {body.setup.length > 0 && <BriefSetupRows setup={body.setup} />}
    </div>
  );
}
