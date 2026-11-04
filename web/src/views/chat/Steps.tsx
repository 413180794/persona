// The work of a segment: one row that folds tools, thoughts, notices and intermediate replies (Beautiful UI's
// Thinking / Tool Chips grammar), expanding in place into a trace with a hairline on the left.
import { memo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { imageURL } from "../../lib/api";
import { toolFile } from "../../lib/files";
import { duration } from "../../lib/format";
import { openPanel } from "../../lib/panel";
import { getSession } from "../../lib/store";
import { taskBadge, taskLabel, taskPill, taskSubject, wakeSummary, type Tone } from "../../lib/tasks";
import { editStats, toolChip, toolIcon, toolLabel, workSummary, type Segment, type Step } from "../../lib/turns";
import type { Item } from "../../lib/types";
import { Collapse, copyText, cx, EASE, PixelGrid, Shimmer, Spinner } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";
import { Markdown } from "../../ui/Markdown";
import { useChatContext } from "./chat-context";
import { ChatImageViewer } from "./ImageViewer";

export function WorkRow({
  seg,
  live,
  now,
  mid,
  sid,
  expandFold,
  onOpen,
}: {
  seg: Segment;
  live: boolean;
  now: number;
  mid: string;
  sid: string;
  expandFold: (f: Item) => Promise<void>;
  /** Opening does something else (a merged exchange splits into its messages) instead of unfolding here. */
  onOpen?: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [loadingFolds, setLoadingFolds] = useState(false);
  const header = useRef<HTMLButtonElement>(null);
  const summary = workSummary(seg);
  const start = Date.parse(seg.start ?? "");
  const end = live ? now : Date.parse(seg.end ?? "");
  const took = Number.isFinite(start) && Number.isFinite(end) && end > start ? duration(end - start) : "";
  if (!seg.steps.length && !live) return null;

  const toggle = async () => {
    if (onOpen) {
      onOpen();
      return;
    }
    const next = !open;
    setOpen(next);
    if (next && seg.folds.length) {
      setLoadingFolds(true);
      try {
        for (const f of seg.folds) await expandFold(f);
      } finally {
        setLoadingFolds(false);
      }
    }
  };

  // folding from inside (the 收起 at the end, the hairline): ChatView brings the header back into view (#1486), and
  // focus goes to it rather than to the page as the button goes away
  const foldFromEnd = () => {
    setOpen(false);
    header.current?.focus({ preventScroll: true });
  };

  const last = seg.steps[seg.steps.length - 1];
  return (
    <div className="flex flex-col" data-work-row>
      <button
        ref={header}
        type="button"
        aria-expanded={open}
        onClick={toggle}
        className="group -mx-1.5 flex w-fit max-w-full min-w-0 items-center gap-2 rounded-control px-1.5 py-1 text-left transition-colors duration-100 hover:bg-hover-2"
      >
        {live ? <PixelGrid /> : <Icon name="spark" size={14} className="shrink-0 text-ink-3" />}
        {live ? (
          <Shimmer className="shrink-0 text-[13px] font-medium whitespace-nowrap">正在工作{took ? ` ${took}` : ""}</Shimmer>
        ) : null}
        <span className={cx("min-w-0 truncate text-[13px] whitespace-nowrap", live ? "text-ink-3" : "font-medium text-ink-2")}>
          {[summary, live ? "" : took].filter(Boolean).join(" · ") || (live ? "" : "工作")}
        </span>
        {loadingFolds ? (
          <Spinner />
        ) : (
          <Icon name="chevron" size={14} stroke={2.2} className="shrink-0 text-ink-3 transition-transform duration-300" style={{ transform: open ? "rotate(180deg)" : "none" }} />
        )}
      </button>
      {/* the newest step peeks out while the agent works */}
      {/* the newest step peeks out while the agent works; it folds away as the trace unfolds */}
      <Collapse open={live && !open && !!last && last.kind !== "fold"} lazy>
        {last && (
          <div className="mt-0.5 ml-[5px] border-l border-line pl-3.5" style={{ animation: "fade-in 250ms ease-out both" }} key={last.id}>
            <StepRow step={last} live mid={mid} sid={sid} />
          </div>
        )}
      </Collapse>
      <Collapse open={open} lazy>
        <WorkTrace seg={seg} live={live} mid={mid} sid={sid} onFold={foldFromEnd} />
      </Collapse>
    </div>
  );
}

/** An open work row's trace: its steps on a hairline, which folds it, and 收起 at the end (#1486). */
export function WorkTrace({ seg, live, mid, sid, onFold }: { seg: Segment; live: boolean; mid: string; sid: string; onFold: () => void }) {
  return (
    <div className="relative mt-1 ml-[5px] pl-3.5">
      {/* the hairline folds the trace too: an 11 px strip around it, so it is not a one-pixel target */}
      <button
        type="button"
        data-fold-from-end
        tabIndex={-1}
        aria-label="收起执行过程"
        aria-expanded
        onClick={onFold}
        className="group/rail absolute top-0 bottom-2 -left-[5px] flex w-[11px] cursor-pointer justify-center"
      >
        <span className="h-full w-px bg-line transition-colors duration-100 group-hover/rail:bg-ink-3" />
      </button>
      <div className="flex flex-col gap-0.5 py-0.5">
        {seg.steps.map((st, i) => <StepRow key={st.id} step={st} live={live && i === seg.steps.length - 1} mid={mid} sid={sid} />)}
        {/* folding from the end holds the row's end: a header above the view comes down into it (ChatView) */}
        <button
          type="button"
          data-fold-from-end
          aria-label="收起执行过程"
          aria-expanded
          onClick={onFold}
          className="mt-1 -ml-[3px] flex w-fit items-center gap-1 rounded-[6px] px-[3px] py-0.5 text-[12px] whitespace-nowrap text-ink-3 transition-colors hover:bg-hover hover:text-ink-2"
        >
          收起
          <Icon name="chevron" size={12} stroke={2.2} className="shrink-0" style={{ transform: "rotate(180deg)" }} />
        </button>
      </div>
    </div>
  );
}

const StepRow = memo(function StepRow({ step, live, mid, sid }: { step: Step; live?: boolean; mid: string; sid: string }) {
  switch (step.kind) {
    case "tool":
      return <ToolRow step={step} live={live} mid={mid} sid={sid} />;
    case "thinking":
      return <Disclosure icon="spark" filled label="思考" chip={firstLine(step.item.text)} detail={<p className="text-[12.5px] leading-relaxed whitespace-pre-wrap text-ink-2">{step.item.text}</p>} />;
    case "text":
      return (
        <div className="py-1 pl-0.5 text-ink-2">
          <Markdown text={step.item.text ?? ""} small mid={mid} cwd={getSession(mid, sid)?.cwd} />
        </div>
      );
    case "notice":
      return <NoticeRow item={step.item} mid={mid} sid={sid} />;
    case "image":
      return (
        <div className="py-1">
          <Thumb item={step.item} mid={mid} sid={sid} />
        </div>
      );
    case "fold":
      return (
        <div className="flex h-7 items-center gap-2 px-[3px] text-[12px] text-ink-3">
          <Spinner size={11} /> 正在取 {step.item.fold?.items ?? ""} 条
        </div>
      );
  }
});

function firstLine(s?: string) {
  return (s ?? "").trim().split("\n")[0].slice(0, 160);
}

function Disclosure({
  icon,
  filled,
  label,
  chip,
  mono,
  extra,
  status,
  detail,
  onChip,
  chipTitle,
}: {
  icon: IconName;
  filled?: boolean;
  label: string;
  chip?: string;
  mono?: boolean;
  /** The chip is a link (a tool's file): it opens that instead of folding the row. */
  onChip?: () => void;
  chipTitle?: string;
  extra?: React.ReactNode;
  status?: React.ReactNode;
  detail?: React.ReactNode;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div>
      <button
        type="button"
        aria-expanded={open}
        disabled={!detail}
        onClick={() => setOpen((o) => !o)}
        className="group/row -mx-[3px] flex h-7 w-[calc(100%+6px)] min-w-0 items-center gap-2 rounded-control px-[3px] text-left transition-colors duration-100 enabled:hover:bg-hover-2"
      >
        <span className="relative flex size-4 shrink-0 items-center justify-center text-ink-3">
          <Icon name={icon} size={13} className={cx("transition-opacity duration-100", detail ? "group-hover/row:opacity-0" : "", open && "opacity-0")} style={filled ? { fill: "currentColor", stroke: "none" } : undefined} />
          {detail && (
            <Icon
              name="chevron"
              size={12}
              stroke={2.2}
              className={cx("absolute transition-[opacity,transform] duration-150 group-hover/row:opacity-100", open ? "opacity-100" : "opacity-0")}
              style={{ transform: open ? "none" : "rotate(-90deg)" }}
            />
          )}
        </span>
        <span className="shrink-0 text-[12.5px] font-medium whitespace-nowrap text-ink">{label}</span>
        {chip && (
          <span
            {...(onChip && {
              role: "link",
              tabIndex: 0,
              title: chipTitle,
              onClick: (e: React.MouseEvent) => {
                e.stopPropagation();
                onChip();
              },
              onKeyDown: (e: React.KeyboardEvent) => {
                if (e.key !== "Enter" && e.key !== " ") return;
                e.preventDefault();
                e.stopPropagation();
                onChip();
              },
            })}
            className={cx(
              "inline-flex h-5.5 min-w-0 items-center truncate rounded-chip bg-hover-2 px-1.5 text-[11.5px] text-[#43464c] shadow-hairline dark:bg-field dark:text-ink-2",
              mono && "font-mono",
              onChip && "cursor-pointer underline-offset-2 hover:text-accent-ink hover:underline dark:hover:text-accent-ink",
            )}
          >
            <span className="truncate">{chip}</span>
          </span>
        )}
        {extra}
        <span className="ml-auto flex shrink-0 items-center pl-1">{status}</span>
      </button>
      {detail && (
        <Collapse open={open} lazy>
          <div className="mt-0.5 mb-1.5 ml-2 border-l border-line py-0.5 pl-3.5">{detail}</div>
        </Collapse>
      )}
    </div>
  );
}

function ToolRow({ step, live, mid, sid }: { step: Extract<Step, { kind: "tool" }>; live?: boolean; mid: string; sid: string }) {
  const { use, result } = step;
  const { tasks, subagents, openSubagent } = useChatContext();
  const stats = editStats(use);
  const running = !result && live;
  const error = result?.is_error;
  const open = fileOpener(use, mid, sid);
  // a call that went to the background says how the task went (后台 · 已完成)
  const call = use.tool_use_id;
  const bg = call && (tasks.started.has(call) || tasks.notes.has(call)) ? taskBadge(tasks.notes.get(call) ?? []) : null;
  const sub = call && (use.tool_name === "Agent" || use.tool_name === "Task") && openSubagent ? subagents.find((a) => a.tool_use_id === call) : undefined;
  return (
    <>
      <Disclosure
        icon={toolIcon(use.tool_name)}
        label={toolLabel(use.tool_name)}
        chip={toolChip(use)}
        mono={use.tool_name !== "Task" && use.tool_name !== "Agent" && use.tool_name !== "TodoWrite"}
        extra={
          stats && (stats.add || stats.del) ? (
            <span className="shrink-0 font-mono text-[11px] tabular-nums">
              <span className="text-green">+{stats.add}</span> {stats.del > 0 && <span className="text-red">−{stats.del}</span>}
            </span>
          ) : null
        }
        status={
          running ? (
            <Spinner size={11} />
          ) : error ? (
            <span className="rounded-[4px] bg-red-tint px-1 text-[10.5px] font-medium text-red">出错</span>
          ) : bg ? (
            <span className={cx("rounded-[4px] px-1 text-[10.5px] font-medium whitespace-nowrap", bg.failed ? "bg-red-tint text-red" : "bg-field text-ink-2")}>{bg.text}</span>
          ) : null
        }
        detail={<ToolDetail step={step} mid={mid} sid={sid} />}
        onChip={open}
        chipTitle={open ? `打开 ${toolFile(use.tool_name, use.tool_input)?.path}` : undefined}
      />
      {sub && openSubagent && (
        <button type="button" onClick={() => openSubagent(sub)} className="mb-1 ml-6 flex w-fit items-center gap-1 rounded-[6px] px-1 py-0.5 text-[12px] font-medium whitespace-nowrap text-accent-ink transition-colors hover:bg-hover-2">
          查看子 Agent
          <Icon name="chevronRight" size={12} stroke={2.2} />
        </button>
      )}
    </>
  );
}

/** Opens the file a Read / Edit / Write step works on in the side panel, relative paths in the session's folder. */
function fileOpener(use: Item, mid: string, sid: string): (() => void) | undefined {
  const f = toolFile(use.tool_name, use.tool_input);
  if (!f) return undefined;
  return () => openPanel({ kind: "file", mid, path: f.path, cwd: getSession(mid, sid)?.cwd, line: f.line });
}

function ToolDetail({ step, mid, sid }: { step: Extract<Step, { kind: "tool" }>; mid: string; sid: string }) {
  const { use, result, images } = step;
  const inp = (use.tool_input ?? {}) as Record<string, unknown>;
  const name = use.tool_name ?? "";
  const open = fileOpener(use, mid, sid);
  let input: React.ReactNode = null;
  if ((name === "Bash" || name === "exec_command" || name === "shell") && (typeof inp.command === "string" || Array.isArray(inp.command))) {
    const cmd = Array.isArray(inp.command) ? (inp.command as string[]).join(" ") : (inp.command as string);
    input = <Mono text={`$ ${cmd}`} />;
  } else if (name === "Edit" && typeof inp.old_string === "string") {
    input = <Diff path={String(inp.file_path ?? "")} oldText={inp.old_string as string} newText={String(inp.new_string ?? "")} onOpen={open} />;
  } else if (name === "MultiEdit" && Array.isArray(inp.edits)) {
    input = (
      <div className="flex flex-col gap-1.5">
        {(inp.edits as Record<string, unknown>[]).map((e, i) => (
          <Diff key={i} path={i === 0 ? String(inp.file_path ?? "") : ""} oldText={String(e.old_string ?? "")} newText={String(e.new_string ?? "")} onOpen={open} />
        ))}
      </div>
    );
  } else if (name === "Write" && typeof inp.content === "string") {
    input = <Diff path={String(inp.file_path ?? "")} oldText="" newText={inp.content as string} onOpen={open} />;
  } else if (name === "TodoWrite" && Array.isArray(inp.todos)) {
    input = <Todos todos={inp.todos as Record<string, unknown>[]} />;
  } else if (name === "Task" || name === "Agent") {
    input = <p className="text-[12px] leading-relaxed whitespace-pre-wrap text-ink-2">{String(inp.prompt ?? "")}</p>;
  } else if (Object.keys(inp).length) {
    input = <Mono text={JSON.stringify(inp, null, 2)} />;
  }
  // Read / NotebookEdit / a Codex change (no old_string) have no diff header to click: the path, as a link.
  const link = open && !(name === "Write" || name === "MultiEdit" || (name === "Edit" && typeof inp.old_string === "string")) ? toolFile(name, inp)?.path : undefined;
  return (
    <div className="flex flex-col gap-1.5 py-1">
      {link && open && (
        <button type="button" onClick={open} className="flex w-fit max-w-full min-w-0 items-center gap-1.5 rounded-[6px] px-0.5 text-left font-mono text-[11.5px] text-accent-ink hover:underline">
          <Icon name="read" size={12} className="shrink-0" />
          <span className="truncate">{link}</span>
        </button>
      )}
      {input}
      {result?.text && name !== "TodoWrite" && <Mono text={result.text} tone={result.is_error ? "error" : "out"} />}
      {images.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {images.map((im) => (
            <Thumb key={im.id} item={im} mid={mid} sid={sid} />
          ))}
        </div>
      )}
    </div>
  );
}

function Mono({ text, tone }: { text: string; tone?: "out" | "error" }) {
  return (
    <div className="group/mono relative">
      <pre
        className={cx(
          "max-h-72 overflow-auto rounded-[8px] px-2.5 py-2 font-mono text-[11.5px] leading-[1.6] whitespace-pre-wrap [overflow-wrap:anywhere] shadow-hairline",
          tone === "error" ? "bg-red-tint text-red" : "bg-inset text-ink-2",
        )}
      >
        {text}
      </pre>
      <button
        type="button"
        aria-label="拷贝"
        onClick={() => copyText(text)}
        className="absolute top-1 right-1 flex size-6 items-center justify-center rounded-[6px] bg-surface text-ink-3 opacity-0 shadow-btn transition-opacity group-hover/mono:opacity-100 hover:text-ink focus-visible:opacity-100"
      >
        <Icon name="copy" size={12} />
      </button>
    </div>
  );
}

function Diff({ path, oldText, newText, onOpen }: { path: string; oldText: string; newText: string; onOpen?: () => void }) {
  const [all, setAll] = useState(false);
  const lines = [...(oldText ? oldText.split("\n").map((t) => ({ t, k: "-" as const })) : []), ...(newText ? newText.split("\n").map((t) => ({ t, k: "+" as const })) : [])];
  const shown = all ? lines : lines.slice(0, 40);
  return (
    <div className="overflow-hidden rounded-[8px] bg-surface shadow-hairline">
      {path &&
        (onOpen ? (
          <button type="button" onClick={onOpen} title={`打开 ${path}`} className="block w-full truncate border-b border-line bg-inset px-2.5 py-1 text-left font-mono text-[11px] text-ink-2 hover:text-accent-ink hover:underline">
            {path}
          </button>
        ) : (
          <div className="truncate border-b border-line bg-inset px-2.5 py-1 font-mono text-[11px] text-ink-2">{path}</div>
        ))}
      <div className="max-h-80 overflow-auto py-1 font-mono text-[11.5px] leading-[1.6]">
        {shown.map((l, i) => (
          <div key={i} className={cx("flex min-w-fit pr-3", l.k === "+" ? "bg-green-tint" : "bg-red-tint")}>
            <span className={cx("w-6 shrink-0 text-center select-none", l.k === "+" ? "text-green" : "text-red")}>{l.k === "+" ? "+" : "−"}</span>
            <span className="whitespace-pre text-ink">{l.t || " "}</span>
          </div>
        ))}
      </div>
      {lines.length > 40 && !all && (
        <button type="button" onClick={() => setAll(true)} className="w-full border-t border-line py-1 text-[11.5px] text-ink-2 hover:bg-hover">
          展开全部 {lines.length} 行
        </button>
      )}
    </div>
  );
}

export function Todos({ todos }: { todos: Record<string, unknown>[] }) {
  return (
    <div className="flex flex-col gap-0.5">
      {todos.map((t, i) => {
        const st = String(t.status ?? "");
        return (
          <div key={i} className="flex items-start gap-2 py-0.5 text-[12.5px]">
            <span
              className={cx(
                "mt-[3px] flex size-3.5 shrink-0 items-center justify-center rounded-[4px]",
                st === "completed" ? "bg-ink text-surface" : st === "in_progress" ? "shadow-[inset_0_0_0_1.5px_var(--orange)]" : "shadow-[inset_0_0_0_1.5px_var(--line-strong)]",
              )}
            >
              {st === "completed" && <Icon name="check" size={10} stroke={3} />}
              {st === "in_progress" && <span className="size-1.5 rounded-full bg-orange" />}
            </span>
            <span className={cx("leading-relaxed", st === "completed" ? "text-ink-3 line-through" : st === "in_progress" ? "font-medium text-ink" : "text-ink-2")}>
              {String(t.content ?? t.activeForm ?? "")}
            </span>
          </div>
        );
      })}
    </div>
  );
}

const TONE: Record<Tone, string> = {
  green: "bg-green-tint text-green",
  red: "bg-red-tint text-red",
  neutral: "bg-field text-ink-2",
  orange: "bg-orange-tint text-orange",
};

const TASK_ICON: Record<string, IconName> = { shell: "run", monitor: "monitor", agent: "agent" };

/** A notice in a trace: a background task reporting back (iOS TaskNoteRow), a wake, or a plain line. */
export function NoticeRow({ item, mid, sid }: { item: Item; mid: string; sid: string }) {
  const { tasks } = useChatContext();
  const t = item.task;
  if (t) {
    const call = tasks.calls.get(item.id);
    const pill = taskPill(t, call ? tasks.startedAt.get(call) : undefined, item.ts);
    const out = t.output_file;
    const detail = (
      <div className="flex flex-col gap-1.5 py-1">
        {t.kind === "shell" ? (
          <Mono text={`$ ${t.detail || t.title || ""}`} />
        ) : t.kind === "agent" ? (
          t.detail && <p className="text-[12px] leading-relaxed whitespace-pre-wrap text-ink-2">{t.detail}</p>
        ) : (
          t.detail && <Mono text={t.detail} />
        )}
        {t.kind === "monitor" && (t.status === "event" || !t.detail) && t.title && <p className="text-[12px] text-ink-3">{t.title}</p>}
        {out && (
          <button
            type="button"
            onClick={() => openPanel({ kind: "file", mid, path: out, cwd: getSession(mid, sid)?.cwd })}
            className="flex w-fit items-center gap-1.5 rounded-[6px] px-0.5 text-[12px] font-medium whitespace-nowrap text-accent-ink hover:underline"
          >
            <Icon name="read" size={12} />
            查看输出
          </button>
        )}
      </div>
    );
    return (
      <Disclosure
        icon={TASK_ICON[t.kind] ?? "bell"}
        label={taskLabel(t)}
        chip={taskSubject(t, call ? tasks.descriptions.get(call) : undefined) || firstLine(item.text)}
        mono={t.kind === "shell"}
        status={pill ? <span className={cx("rounded-[4px] px-1 text-[10.5px] font-medium whitespace-nowrap", TONE[pill.tone])}>{pill.text}</span> : null}
        detail={t.detail || t.title || out ? detail : undefined}
      />
    );
  }
  if (item.wake) {
    const w = wakeSummary(item);
    return (
      <div className="flex min-h-7 items-center gap-2 px-[3px] py-1 text-[12.5px] text-ink-2">
        <Icon name="bell" size={13} className={cx("shrink-0", w.failed ? "text-red" : "text-accent")} />
        <span className="min-w-0 truncate">{w.text}</span>
      </div>
    );
  }
  return (
    <div className="flex min-h-7 items-start gap-2 px-[3px] py-1 text-[12.5px] text-ink-2">
      <Icon name="bell" size={13} className="mt-[3px] shrink-0 text-ink-3" />
      <span className="min-w-0 leading-relaxed whitespace-pre-wrap">{item.text}</span>
    </div>
  );
}

export function imageSrc(item: Item, mid: string, sid: string) {
  if (item.image?.data) return `data:${item.image.mime};base64,${item.image.data}`;
  return imageURL(mid, sid, item.id);
}

export function Thumb({ item, mid, sid, size = 132 }: { item: Item; mid: string; sid: string; size?: number }) {
  const [open, setOpen] = useState(false);
  const src = imageSrc(item, mid, sid);
  const w = item.image?.width,
    h = item.image?.height;
  const ratio = w && h ? w / h : 1;
  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="block overflow-hidden rounded-[10px] bg-field shadow-hairline transition-transform duration-150 active:scale-[0.98]"
        // a phone screenshot (tall) gets more height, so it is not a sliver
        style={ratio < 1 ? { width: Math.round(size * 1.5 * ratio), height: Math.round(size * 1.5) } : { width: Math.round(Math.min(size * 1.6, size * ratio)), height: size }}
        aria-label="查看图片"
      >
        <img src={src} alt="" loading="lazy" decoding="async" className="size-full object-cover" />
      </button>
      {open && <ChatImageViewer item={item} mid={mid} sid={sid} onClose={() => setOpen(false)} />}
    </>
  );
}

export function Lightbox({ src, onClose }: { src: string; onClose: () => void }) {
  return createPortal(
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-6 backdrop-blur-sm"
      style={{ animation: "fade-in 150ms ease-out both" }}
      onClick={onClose}
      onKeyDown={(e) => e.key === "Escape" && onClose()}
      role="dialog"
      tabIndex={-1}
      ref={(el) => el?.focus()}
    >
      <img src={src} alt="" className="max-h-full max-w-full rounded-[8px] object-contain shadow-overlay" style={{ animation: `pop-in 220ms ${EASE} both` }} />
      <a href={src} target="_blank" rel="noreferrer" onClick={(e) => e.stopPropagation()} className="absolute top-4 right-14 flex size-8 items-center justify-center rounded-full bg-white/15 text-white hover:bg-white/25" aria-label="在新标签页打开">
        <Icon name="external" size={15} />
      </a>
      <button type="button" onClick={onClose} className="absolute top-4 right-4 flex size-8 items-center justify-center rounded-full bg-white/15 text-white hover:bg-white/25" aria-label="关闭">
        <Icon name="x" size={15} />
      </button>
    </div>,
    document.body,
  );
}
