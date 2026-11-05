// What an opened step shows besides its raw input (iOS ToolViews.swift ToolDetail, Android ToolDetails.kt /
// ContentPresentation.kt): a Codex patch as a diff, the questions an AskUserQuestion or the assistant's ask_user asked
// (with what the user picked), and the hub's cut texts (full_size) with the mark it adds where it cut them.
import type { Op } from "./compare";
import { parseDiff } from "./files";

/** A Codex file change (the hub maps it to an Edit with `diff`): one block per hunk; a new or deleted file the hub
 *  sends as its whole content (no hunks) is all added, or all removed when `change` is `delete` (Android patchDiff). */
export function patchDiffs(patch: string, change?: string): Op[][] {
  if (!patch.split("\n").some((l) => l.startsWith("@@"))) {
    if (!patch) return [];
    return [patch.replace(/\n$/, "").split("\n").map((text) => ({ kind: change === "delete" ? ("del" as const) : ("add" as const), text }))];
  }
  const out: Op[][] = [];
  for (const l of parseDiff(patch)) {
    if (l.kind === "hunk") out.push([]);
    else if (l.kind === "add" || l.kind === "del" || l.kind === "context") out[out.length - 1].push({ kind: l.kind === "context" ? "same" : l.kind, text: l.text });
  }
  return out.filter((d) => d.length);
}

/** The base name of a tool: an MCP tool's own name (mcp__jarvis__ask_user → ask_user). */
export function baseToolName(name: string): string {
  return name.startsWith("mcp__") ? (name.split("__").pop() ?? name) : name;
}

export interface ToolQuestion {
  question: string;
  header?: string;
  multiSelect: boolean;
  options: { label: string; description?: string }[];
}

const s = (v: unknown) => (typeof v === "string" ? v.trim() : "");

/** The questions an AskUserQuestion / ask_user call asked (empty when its input has none). */
export function toolQuestions(input: Record<string, unknown> | undefined): ToolQuestion[] {
  const qs = Array.isArray(input?.questions) ? (input.questions as unknown[]) : [];
  return qs.flatMap((raw) => {
    const q = (raw ?? {}) as Record<string, unknown>;
    // kept untrimmed: the hub writes the answer with the raw strings (`- <question> → <answer>`), so matching needs them
    const question = typeof q.question === "string" ? q.question : "";
    if (!question.trim()) return [];
    const options = (Array.isArray(q.options) ? (q.options as unknown[]) : []).flatMap((o) => {
      const opt = (typeof o === "string" ? { label: o } : (o ?? {})) as Record<string, unknown>;
      const label = typeof opt.label === "string" ? opt.label : "";
      return label.trim() ? [{ label, description: s(opt.description) || undefined }] : [];
    });
    return [{ question, header: s(q.header) || undefined, multiSelect: q.multiSelect === true || q.multi_select === true, options }];
  });
}

/** Each question's answer in the hub's ask_user result (`The user answered:\n- <question> → <answer>`); empty when
 *  nobody answered or the text is not the hub's (iOS AskUserAnswers.parse). */
export function askUserAnswers(result: string, questions: ToolQuestion[]): Record<string, string> {
  if (!result.startsWith("The user answered:")) return {};
  const marks = questions
    .map((q) => {
      const mark = `\n- ${q.question} → `;
      const at = result.indexOf(mark);
      return at >= 0 ? { q: q.question, at, end: at + mark.length } : null;
    })
    .filter((m): m is { q: string; at: number; end: number } => m !== null)
    .sort((a, b) => a.at - b.at);
  const out: Record<string, string> = {};
  marks.forEach((m, n) => {
    const answer = result.slice(m.end, n + 1 < marks.length ? marks[n + 1].at : undefined).trim();
    if (answer) out[m.q] = answer;
  });
  return out;
}

// i18n-ignore-start: the suffixes Claude writes after an option, matched as written
const RECOMMENDED_SUFFIXES = [" (Recommended)", "(Recommended)", "（推荐）", "(推荐)"];
// i18n-ignore-end

/** An option's words without Claude's "(Recommended)" suffix, and whether it had one (iOS QuestionOption.display).
 *  Only for showing it: the answer sent back keeps the whole label. */
export function optionDisplay(label: string): { text: string; recommended: boolean } {
  const trimmed = label.trimEnd();
  for (const suffix of RECOMMENDED_SUFFIXES) {
    if (trimmed.endsWith(suffix) && trimmed.length > suffix.length) return { text: trimmed.slice(0, -suffix.length).trim(), recommended: true };
  }
  return { text: label, recommended: false };
}

/** Whether an option is among what was picked (a multi-select answer joins its picks with ", "). */
export function pickedOption(label: string, answer: string | undefined, multiSelect: boolean): boolean {
  if (answer === undefined) return false;
  // the label is kept raw (see toolQuestions) and the answer comes trimmed: compare without the edge whitespace
  const want = label.trim();
  return answer.trim() === want || (multiSelect && answer.split(", ").some((a) => a.trim() === want));
}

/** An answer typed instead of picking one of the options. */
export function typedAnswer(q: ToolQuestion, answer: string | undefined): string | undefined {
  if (answer === undefined || q.options.some((o) => pickedOption(o.label, answer, q.multiSelect))) return undefined;
  return answer;
}

/** The note the hub appends where it cut a long text for the page (`\n…（已截断，…共 N 字节）`). */
const CUT_MARK = "\n…（已截断"; // i18n-ignore: matches the hub's text

/** A cut text without the hub's note (the 看全文 button says it instead). */
export function withoutCutMark(text: string): string {
  const at = text.lastIndexOf(CUT_MARK);
  return at >= 0 ? text.slice(0, at) : text;
}

