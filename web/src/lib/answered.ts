// What a card that was just answered shows in place of its choices: each question and the pick, or how a plan,
// permission or terminal dialog was answered. The resolution is what was sent, or — when the hub says the card was
// answered already (POST …/resolve → {already, resolution}) — the answer that took effect, as its history keeps it.
import { t } from "../i18n";
import type { Interaction } from "./types";

export type Resolution = Record<string, unknown>;

/** The hub's answer to POST /interactions/{id}/resolve: {ok}, or {ok, already, status, resolution} for a card that
 *  was over already (answered elsewhere, or by this page when its event was missed). */
export interface ResolveResult {
  ok?: boolean;
  already?: boolean;
  status?: string;
  resolution?: Resolution;
}

export interface AnswerLine {
  /** What was asked (a question's wording); empty for a card with one answer and nothing to label it. */
  label: string;
  answer: string;
}

const MODE_LABEL: Record<string, string> = { acceptEdits: t("lib-answered.mode_accept_edits"), bypassPermissions: t("lib-answered.mode_bypass"), default: t("lib-answered.mode_default") };

const str = (v: unknown) => (typeof v === "string" ? v : "");

/** The lines an answered card shows. */
export function answerLines(ix: Interaction, r: Resolution): AnswerLine[] {
  if (r.dismiss === true) return [{ label: "", answer: t("lib-answered.dismissed") }];
  if (r.answered_by === "assistant" && ix.kind !== "question") return [{ label: "", answer: t("lib-answered.by_assistant") }];
  switch (ix.kind) {
    case "question": {
      const answers = (r.answers ?? {}) as Record<string, unknown>;
      return (ix.questions ?? []).map((q) => ({ label: q.question, answer: str(answers[q.question]) || t("lib-answered.unanswered") }));
    }
    case "plan":
      if (r.approve === true) return [{ label: "", answer: MODE_LABEL[str(r.mode)] ? t("lib-answered.approved_mode", { mode: MODE_LABEL[str(r.mode)] }) : t("lib-answered.approved") }];
      return [{ label: "", answer: str(r.feedback) ? t("lib-answered.rejected_feedback", { feedback: str(r.feedback) }) : t("lib-answered.rejected") }];
    case "permission":
      if (r.allow === true) return [{ label: "", answer: r.always === true ? t("lib-answered.always_allowed") : t("lib-answered.allowed") }];
      if (r.allow === false) return [{ label: "", answer: t("lib-answered.denied") }];
      return [{ label: "", answer: t("lib-answered.handled") }];
    default: {
      const options = ix.dialog?.options ?? [];
      const labelOf = (key: string) => options.find((o) => o.key === key)?.label ?? key;
      const typed = str(r.text) && (ix.secret || r.text === "<redacted>" ? t("lib-answered.entered") : str(r.text));
      const picked = Array.isArray(r.options) ? (r.options as unknown[]).map((k) => labelOf(str(k))) : str(r.option) ? [labelOf(str(r.option))] : [];
      const answer = [...picked, typed].filter(Boolean).join(t("lib-answered.separator"));
      return [{ label: "", answer: answer || t("lib-answered.handled") }];
    }
  }
}

/** Why a card could not be answered any more (410, its `status`). */
export function endedText(status: unknown): string {
  switch (status) {
    case "expired":
      return t("lib-answered.ended_expired");
    case "cancelled":
      return t("lib-answered.ended_cancelled");
    case "dismissed":
      return t("lib-answered.ended_dismissed");
    default:
      return t("lib-answered.ended_generic");
  }
}
