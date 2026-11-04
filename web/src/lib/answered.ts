// What a card that was just answered shows in place of its choices: each question and the pick, or how a plan,
// permission or terminal dialog was answered. The resolution is what was sent, or — when the hub says the card was
// answered already (POST …/resolve → {already, resolution}) — the answer that took effect, as its history keeps it.
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

const MODE_LABEL: Record<string, string> = { acceptEdits: "自动接受编辑", bypassPermissions: "全部放行", default: "每步确认" };

const str = (v: unknown) => (typeof v === "string" ? v : "");

/** The lines an answered card shows. */
export function answerLines(ix: Interaction, r: Resolution): AnswerLine[] {
  if (r.dismiss === true) return [{ label: "", answer: "交回电脑处理" }];
  if (r.answered_by === "assistant" && ix.kind !== "question") return [{ label: "", answer: "助手已代答" }];
  switch (ix.kind) {
    case "question": {
      const answers = (r.answers ?? {}) as Record<string, unknown>;
      return (ix.questions ?? []).map((q) => ({ label: q.question, answer: str(answers[q.question]) || "（未回答）" }));
    }
    case "plan":
      if (r.approve === true) return [{ label: "", answer: MODE_LABEL[str(r.mode)] ? `批准，${MODE_LABEL[str(r.mode)]}` : "批准" }];
      return [{ label: "", answer: str(r.feedback) ? `打回：${str(r.feedback)}` : "打回" }];
    case "permission":
      if (r.allow === true) return [{ label: "", answer: r.always === true ? "总是允许" : "允许" }];
      if (r.allow === false) return [{ label: "", answer: "拒绝" }];
      return [{ label: "", answer: "已处理" }];
    default: {
      const options = ix.dialog?.options ?? [];
      const labelOf = (key: string) => options.find((o) => o.key === key)?.label ?? key;
      const typed = str(r.text) && (ix.secret || r.text === "<redacted>" ? "（已输入）" : str(r.text));
      const picked = Array.isArray(r.options) ? (r.options as unknown[]).map((k) => labelOf(str(k))) : str(r.option) ? [labelOf(str(r.option))] : [];
      const answer = [...picked, typed].filter(Boolean).join("，");
      return [{ label: "", answer: answer || "已处理" }];
    }
  }
}

/** Why a card could not be answered any more (410, its `status`). */
export function endedText(status: unknown): string {
  switch (status) {
    case "expired":
      return "这个请求已经过期，回答没有送到";
    case "cancelled":
      return "电脑上已经撤回了这个请求，回答没有送到";
    case "dismissed":
      return "这个请求已经交回电脑处理，回答没有送到";
    default:
      return "这个请求已经结束，回答没有送到";
  }
}
