// One tool call in a work group (cindy's AgentActionRow): [status][verb][what it worked on][+N −N][›]. A command and
// the like open in place (what went in, what came out, in full); an edit opens its diff over the page; a read opens
// the file in the side panel.
import { memo, useMemo, useState } from "react";
import { diffLines, type Op } from "../../lib/compare";
import { fileName, toolFile } from "../../lib/files";
import { openPanel } from "../../lib/panel";
import { getSession } from "../../lib/store";
import { taskBadge } from "../../lib/tasks";
import { toolChip, toolLabel } from "../../lib/turns";
import type { Item } from "../../lib/types";
import type { ToolStep } from "../../lib/work-groups";
import { cx, Dialog, Spinner } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { useChatContext } from "./chat-context";
import { Thumb } from "./Steps";

/** The row's chrome, shared with the thinking rows so a mixed list reads as one (cindy's activityRowChrome). */
const ROW = "group flex w-full min-w-0 items-center gap-1.5 rounded-[8px] px-2 py-[3px] text-left select-none transition-colors duration-150 hover:bg-tool-hover motion-reduce:transition-none";
export const CHEVRON_SLOT = "ml-auto flex size-[18px] shrink-0 items-center justify-center rounded-[8px] text-tool-ink transition-colors duration-150 group-hover:bg-hover-2 motion-reduce:transition-none";
export const STATUS_SLOT = "inline-flex h-[18px] w-4 shrink-0 items-center justify-center text-tool-ink";

const COMMAND_TOOLS = new Set(["Bash", "exec_command", "shell"]);
const FILE_TOOLS = new Set(["Read", "Edit", "Write", "MultiEdit", "NotebookEdit"]);
const DIFF_TOOLS = new Set(["Edit", "Write", "MultiEdit"]);

/** cindy's verbAggregator, in its Chinese wording. */
const VERB: Record<string, string> = {
  Edit: "编辑",
  MultiEdit: "编辑",
  NotebookEdit: "编辑",
  Write: "创建",
  Bash: "运行",
  exec_command: "运行",
  shell: "运行",
  Read: "读取",
  TodoWrite: "更新",
  Glob: "搜索",
  Grep: "搜索",
  LS: "列出",
  WebFetch: "访问",
  WebSearch: "访问",
};

const PARAM_MAX = 60;

export function truncateParam(text: string, max = PARAM_MAX): string {
  const t = text.trim();
  return t.length <= max ? t : `${t.slice(0, Math.max(0, max - 3)).trimEnd()}...`;
}

const str = (v: unknown) => (typeof v === "string" ? v.trim() : "");
/** A command call's command (Codex sends it as an argv array). */
const commandOf = (inp: Record<string, unknown>) => (Array.isArray(inp.command) ? (inp.command as unknown[]).join(" ") : str(inp.command) || str(inp.cmd));

export interface ActionPresentation {
  /** Hidden when the agent described the command itself: its sentence is the row. */
  verb?: string;
  param?: { text: string; title?: string };
  /** A file the call worked on: drawn as a chip with its name. */
  file?: string;
  /** A command without a description: on a quieter line under the row. */
  raw?: string;
}

/** How a call reads in its row (cindy's describeToolUse + extractDisplayParam). */
export function presentAction(use: Item): ActionPresentation {
  const name = use.tool_name ?? "";
  const inp = (use.tool_input ?? {}) as Record<string, unknown>;
  if (COMMAND_TOOLS.has(name)) {
    const cmd = commandOf(inp);
    const desc = str(inp.description);
    if (desc) return { param: { text: desc, title: cmd || undefined } };
    return { verb: "运行命令", raw: cmd || undefined };
  }
  if (FILE_TOOLS.has(name)) {
    const path = toolFile(name, inp)?.path ?? "";
    return { verb: VERB[name], ...(path ? { file: path, param: { text: truncateParam(fileName(path)), title: path } } : {}) };
  }
  if (name === "Grep" || name === "Glob") return { verb: VERB[name], param: { text: truncateParam(str(inp.pattern)) } };
  if (name === "WebFetch") return { verb: VERB[name], param: { text: truncateParam(str(inp.url)), title: str(inp.url) } };
  if (name === "WebSearch") return { verb: VERB[name], param: { text: truncateParam(str(inp.query)) } };
  if (name === "LS") return { verb: VERB[name], param: { text: truncateParam(str(inp.path)) } };
  if (name.startsWith("mcp__")) {
    const server = name.slice(5, name.lastIndexOf("__")).replace(/^plugin_/, "");
    return { verb: "调用", param: { text: truncateParam(`${server} · ${toolLabel(name)}`), title: name } };
  }
  const chip = toolChip(use);
  return { verb: VERB[name] ?? "调用", param: { text: truncateParam(chip ? `${toolLabel(name)} · ${chip}` : toolLabel(name)), title: chip || undefined } };
}

// ── diffs ──

/** A line diff of an edit's old and new text (lib/compare.ts's Myers diff); too big to diff: all out, all in. */
export function lineDiff(oldText: string, newText: string): Op[] {
  const a = oldText ? oldText.split("\n") : [];
  const b = newText ? newText.split("\n") : [];
  return diffLines(a, b) ?? [...a.map((text) => ({ kind: "del" as const, text })), ...b.map((text) => ({ kind: "add" as const, text }))];
}

/** An edit's changes, one diff per change (MultiEdit has several; Write is all new). */
export function editDiffs(use: Item): Op[][] | null {
  const inp = (use.tool_input ?? {}) as Record<string, unknown>;
  switch (use.tool_name) {
    case "Edit":
      return typeof inp.old_string === "string" || typeof inp.new_string === "string" ? [lineDiff(String(inp.old_string ?? ""), String(inp.new_string ?? ""))] : null;
    case "Write":
      return typeof inp.content === "string" ? [lineDiff("", inp.content)] : null;
    case "MultiEdit":
      return Array.isArray(inp.edits) ? (inp.edits as Record<string, unknown>[]).map((e) => lineDiff(String(e.old_string ?? ""), String(e.new_string ?? ""))) : null;
  }
  return null;
}

export function diffStats(diffs: Op[][]): { add: number; del: number } {
  let add = 0;
  let del = 0;
  for (const d of diffs) for (const l of d) l.kind === "add" ? add++ : l.kind === "del" && del++;
  return { add, del };
}

/** What a call opens to in place: its input as text. */
export function inlineInput(use: Item): string {
  const name = use.tool_name ?? "";
  const inp = (use.tool_input ?? {}) as Record<string, unknown>;
  if (COMMAND_TOOLS.has(name)) {
    return [commandOf(inp), str(inp.cwd) && `cwd: ${str(inp.cwd)}`].filter(Boolean).join("\n");
  }
  if (name === "Grep") return [`pattern: ${str(inp.pattern)}`, ...["path", "glob", "type", "output_mode"].filter((k) => str(inp[k])).map((k) => `${k}: ${str(inp[k])}`)].join("\n");
  if (name === "Glob") return str(inp.path) ? `${str(inp.pattern)}\nin: ${str(inp.path)}` : str(inp.pattern);
  if (name === "WebFetch") return [str(inp.url), str(inp.prompt)].filter(Boolean).join("\n");
  if (name === "WebSearch") return str(inp.query);
  return Object.keys(inp).length ? JSON.stringify(inp, null, 2) : "";
}

export const ActionRow = memo(function ActionRow({ step, running, mid, sid }: { step: ToolStep; running?: boolean; mid: string; sid: string }) {
  const { use, result, images } = step;
  const name = use.tool_name ?? "";
  const { tasks } = useChatContext();
  const p = useMemo(() => presentAction(use), [use]);
  const diffs = useMemo(() => (DIFF_TOOLS.has(name) ? editDiffs(use) : null), [name, use]);
  const stats = diffs ? diffStats(diffs) : null;
  const [open, setOpen] = useState(false);
  const [diffOpen, setDiffOpen] = useState(false);
  const inPlace = !FILE_TOOLS.has(name);
  const call = use.tool_use_id;
  const bg = call && (tasks.started.has(call) || tasks.notes.has(call)) ? taskBadge(tasks.notes.get(call) ?? []) : null;
  const error = Boolean(result?.is_error);

  const activate = () => {
    if (diffs) return setDiffOpen(true);
    if (inPlace) return setOpen((o) => !o);
    const f = toolFile(name, use.tool_input);
    if (f) openPanel({ kind: "file", mid, path: f.path, cwd: getSession(mid, sid)?.cwd, line: f.line });
  };
  const label = [p.verb, p.param?.text].filter(Boolean).join(" ");
  const input = open ? inlineInput(use) : "";
  return (
    <div className="flex min-w-0 flex-col" data-action-row={name}>
      <button type="button" onClick={activate} aria-expanded={inPlace ? open : undefined} aria-label={label || name} title={p.param?.title} className={ROW}>
        <span role="img" aria-label={running ? "进行中" : error ? "出错" : "完成"} className={cx(STATUS_SLOT, error && "text-red")}>
          {running ? <Spinner size={12} /> : <Icon name={error ? "x" : "check"} size={13} />}
        </span>
        {p.verb && <span className="shrink-0 text-[14px] whitespace-nowrap text-tool-ink">{p.verb}</span>}
        {p.param &&
          (p.file ? (
            <span className="inline-flex max-w-full min-w-0 items-center gap-[3px] rounded-[4px] border border-line bg-field px-[5px] py-[0.5px] font-mono text-[13px] leading-[1.385] whitespace-nowrap text-ink-2 transition-colors group-hover:bg-hover-2" data-file-chip>
              <Icon name="read" size={12} className="shrink-0 text-ink-3" />
              <span className="truncate">{p.param.text}</span>
            </span>
          ) : (
            <span className="min-w-0 truncate text-[14px] font-medium text-tool-ink">{p.param.text}</span>
          ))}
        <span className="flex-1" />
        {stats && (stats.add > 0 || stats.del > 0) && (
          <span className="flex shrink-0 gap-1 font-mono text-[13px] font-medium tabular-nums">
            <span className="text-diff-add-fg">+{stats.add}</span>
            <span className="text-diff-del-fg">−{stats.del}</span>
          </span>
        )}
        {bg && <span className={cx("shrink-0 rounded-[4px] px-1 text-[11px] font-medium whitespace-nowrap", bg.failed ? "bg-red-tint text-red" : "bg-field text-ink-2")}>{bg.text}</span>}
        <span aria-hidden className={CHEVRON_SLOT}>
          <Icon name={inPlace && open ? "chevron" : "chevronRight"} size={13} stroke={2.2} />
        </span>
      </button>
      {p.raw && !open && (
        <div title={p.raw} className="min-w-0 truncate px-2 pb-[3px] pl-[30px] font-mono text-[12px] leading-[1.5] text-tool-ink">
          {p.raw}
        </div>
      )}
      {images.length > 0 && (
        <div className="flex min-w-0 flex-wrap gap-1.5 py-2 pl-8">
          {images.map((im) => (
            <Thumb key={im.id} item={im} mid={mid} sid={sid} />
          ))}
        </div>
      )}
      {open && (
        <div className="mx-2 mt-1 mb-1 cursor-text rounded-[6px] border border-line bg-user-bubble px-[10px] py-2 font-mono text-[12px] leading-[17px] text-ink select-text" data-inline-detail>
          {input && <pre className="m-0 whitespace-pre-wrap [overflow-wrap:anywhere]">{input}</pre>}
          {result?.text && (
            <>
              {input && <div className="my-2 h-px bg-line" />}
              <pre className={cx("m-0 whitespace-pre-wrap [overflow-wrap:anywhere]", error ? "text-red" : "text-tool-ink")}>{result.text}</pre>
            </>
          )}
          {!input && !result?.text && <span className="text-tool-ink">没有内容</span>}
        </div>
      )}
      {diffs && <DiffDialog open={diffOpen} onClose={() => setDiffOpen(false)} path={p.file ?? ""} diffs={diffs} mid={mid} sid={sid} />}
    </div>
  );
});

/** An edit's changes over the page: whole lines on red and green, the file one click away. */
function DiffDialog({ open, onClose, path, diffs, mid, sid }: { open: boolean; onClose: () => void; path: string; diffs: Op[][]; mid: string; sid: string }) {
  const s = diffStats(diffs);
  return (
    <Dialog
      open={open}
      onClose={onClose}
      width={860}
      title={
        <span className="flex min-w-0 items-center gap-2">
          <span className="truncate">{path ? fileName(path) : "改动"}</span>
          <span className="shrink-0 font-mono text-[12px] font-medium tabular-nums">
            <span className="text-diff-add-fg">+{s.add}</span> <span className="text-diff-del-fg">−{s.del}</span>
          </span>
        </span>
      }
      footer={
        path ? (
          <button
            type="button"
            onClick={() => {
              onClose();
              openPanel({ kind: "file", mid, path, cwd: getSession(mid, sid)?.cwd });
            }}
            className="flex h-8 items-center gap-1.5 rounded-control px-3 text-[12.5px] font-medium whitespace-nowrap text-ink-2 shadow-btn transition-colors hover:bg-hover"
          >
            <Icon name="read" size={13} />
            打开文件
          </button>
        ) : undefined
      }
    >
      {path && <div className="mb-2 truncate font-mono text-[11.5px] text-ink-3" title={path}>{path}</div>}
      <div className="flex max-h-[60vh] flex-col gap-2 overflow-auto" data-diff>
        {diffs.map((d, n) => (
          <div key={n} className="min-w-fit overflow-hidden rounded-[8px] py-1 font-mono text-[12px] leading-[1.6] shadow-hairline">
            {d.map((l, i) => (
              <div key={i} className={cx("flex min-w-fit pr-3", l.kind === "add" && "bg-diff-add-bg", l.kind === "del" && "bg-diff-del-bg")} data-diff-line={l.kind}>
                <span className={cx("w-6 shrink-0 text-center select-none", l.kind === "add" ? "text-diff-add-fg" : l.kind === "del" ? "text-diff-del-fg" : "text-ink-3")}>{l.kind === "add" ? "+" : l.kind === "del" ? "−" : " "}</span>
                <span className="whitespace-pre text-ink">{l.text || " "}</span>
              </div>
            ))}
          </div>
        ))}
      </div>
    </Dialog>
  );
}
