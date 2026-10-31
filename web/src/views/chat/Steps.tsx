// The work of a segment: one row that folds tools, thoughts, notices and intermediate replies (Beautiful UI's
// Thinking / Tool Chips grammar), expanding in place into a trace with a hairline on the left.
import { memo, useState } from "react";
import { createPortal } from "react-dom";
import { imageURL } from "../../lib/api";
import { duration } from "../../lib/format";
import { editStats, toolChip, toolIcon, toolLabel, workSummary, type Segment, type Step } from "../../lib/turns";
import type { Item } from "../../lib/types";
import { Collapse, copyText, cx, EASE, PixelGrid, Shimmer, Spinner } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";
import { Markdown } from "../../ui/Markdown";

export function WorkRow({
  seg,
  live,
  now,
  mid,
  sid,
  expandFold,
}: {
  seg: Segment;
  live: boolean;
  now: number;
  mid: string;
  sid: string;
  expandFold: (f: Item) => Promise<void>;
}) {
  const [open, setOpen] = useState(false);
  const [loadingFolds, setLoadingFolds] = useState(false);
  const summary = workSummary(seg);
  const start = Date.parse(seg.start ?? "");
  const end = live ? now : Date.parse(seg.end ?? "");
  const took = Number.isFinite(start) && Number.isFinite(end) && end > start ? duration(end - start) : "";
  if (!seg.steps.length && !live) return null;

  const toggle = async () => {
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

  const last = seg.steps[seg.steps.length - 1];
  return (
    <div className="flex flex-col">
      <button
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
      {live && !open && last && last.kind !== "fold" && (
        <div className="mt-0.5 ml-[5px] border-l border-line pl-3.5" style={{ animation: "fade-in 250ms ease-out both" }} key={last.id}>
          <StepRow step={last} live mid={mid} sid={sid} />
        </div>
      )}
      <Collapse open={open}>
        <div className="relative mt-1 ml-[5px] pl-3.5">
          <span aria-hidden className="absolute top-0 bottom-2 left-0 w-px bg-line" />
          <div className="flex flex-col gap-0.5 py-0.5">
            {open && seg.steps.map((st, i) => <StepRow key={st.id} step={st} live={live && i === seg.steps.length - 1} mid={mid} sid={sid} />)}
            {open && seg.steps.length > 8 && (
              <button type="button" onClick={() => setOpen(false)} className="mt-1 w-fit rounded-[6px] px-1.5 py-0.5 text-[12px] text-ink-3 transition-colors hover:bg-hover hover:text-ink-2">
                收起 {seg.steps.length} 步
              </button>
            )}
          </div>
        </div>
      </Collapse>
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
          <Markdown text={step.item.text ?? ""} small />
        </div>
      );
    case "notice":
      return <NoticeRow item={step.item} />;
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
}: {
  icon: IconName;
  filled?: boolean;
  label: string;
  chip?: string;
  mono?: boolean;
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
            className={cx(
              "inline-flex h-5.5 min-w-0 items-center truncate rounded-chip bg-hover-2 px-1.5 text-[11.5px] text-[#43464c] shadow-hairline dark:bg-field dark:text-ink-2",
              mono && "font-mono",
            )}
          >
            <span className="truncate">{chip}</span>
          </span>
        )}
        {extra}
        <span className="ml-auto flex shrink-0 items-center pl-1">{status}</span>
      </button>
      {detail && (
        <Collapse open={open}>
          <div className="mt-0.5 mb-1.5 ml-2 border-l border-line py-0.5 pl-3.5">{open && detail}</div>
        </Collapse>
      )}
    </div>
  );
}

function ToolRow({ step, live, mid, sid }: { step: Extract<Step, { kind: "tool" }>; live?: boolean; mid: string; sid: string }) {
  const { use, result } = step;
  const stats = editStats(use);
  const running = !result && live;
  const error = result?.is_error;
  return (
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
      status={running ? <Spinner size={11} /> : error ? <span className="rounded-[4px] bg-red-tint px-1 text-[10.5px] font-medium text-red">出错</span> : null}
      detail={<ToolDetail step={step} mid={mid} sid={sid} />}
    />
  );
}

function ToolDetail({ step, mid, sid }: { step: Extract<Step, { kind: "tool" }>; mid: string; sid: string }) {
  const { use, result, images } = step;
  const inp = (use.tool_input ?? {}) as Record<string, unknown>;
  const name = use.tool_name ?? "";
  let input: React.ReactNode = null;
  if ((name === "Bash" || name === "exec_command" || name === "shell") && (typeof inp.command === "string" || Array.isArray(inp.command))) {
    const cmd = Array.isArray(inp.command) ? (inp.command as string[]).join(" ") : (inp.command as string);
    input = <Mono text={`$ ${cmd}`} />;
  } else if (name === "Edit" && typeof inp.old_string === "string") {
    input = <Diff path={String(inp.file_path ?? "")} oldText={inp.old_string as string} newText={String(inp.new_string ?? "")} />;
  } else if (name === "MultiEdit" && Array.isArray(inp.edits)) {
    input = (
      <div className="flex flex-col gap-1.5">
        {(inp.edits as Record<string, unknown>[]).map((e, i) => (
          <Diff key={i} path={i === 0 ? String(inp.file_path ?? "") : ""} oldText={String(e.old_string ?? "")} newText={String(e.new_string ?? "")} />
        ))}
      </div>
    );
  } else if (name === "Write" && typeof inp.content === "string") {
    input = <Diff path={String(inp.file_path ?? "")} oldText="" newText={inp.content as string} />;
  } else if (name === "TodoWrite" && Array.isArray(inp.todos)) {
    input = <Todos todos={inp.todos as Record<string, unknown>[]} />;
  } else if (name === "Task" || name === "Agent") {
    input = <p className="text-[12px] leading-relaxed whitespace-pre-wrap text-ink-2">{String(inp.prompt ?? "")}</p>;
  } else if (Object.keys(inp).length) {
    input = <Mono text={JSON.stringify(inp, null, 2)} />;
  }
  return (
    <div className="flex flex-col gap-1.5 py-1">
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

function Diff({ path, oldText, newText }: { path: string; oldText: string; newText: string }) {
  const [all, setAll] = useState(false);
  const lines = [...(oldText ? oldText.split("\n").map((t) => ({ t, k: "-" as const })) : []), ...(newText ? newText.split("\n").map((t) => ({ t, k: "+" as const })) : [])];
  const shown = all ? lines : lines.slice(0, 40);
  return (
    <div className="overflow-hidden rounded-[8px] bg-surface shadow-hairline">
      {path && <div className="truncate border-b border-line bg-inset px-2.5 py-1 font-mono text-[11px] text-ink-2">{path}</div>}
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

function NoticeRow({ item }: { item: Item }) {
  const t = item.task;
  if (t) {
    const ok = t.status === "completed" || t.status === "ended";
    const bad = t.status === "failed" || t.status === "killed";
    const label = { shell: "后台命令", monitor: "监控", agent: "子 Agent", task: "后台任务" }[t.kind] ?? "后台任务";
    return (
      <Disclosure
        icon="bell"
        label={label}
        chip={t.title || firstLine(item.text)}
        mono={t.kind === "shell"}
        status={
          <span className={cx("rounded-[4px] px-1 text-[10.5px] font-medium", ok ? "bg-green-tint text-green" : bad ? "bg-red-tint text-red" : "bg-field text-ink-2")}>
            {{ completed: "完成", failed: "失败", killed: "已停止", stopped: "已停止", event: "事件", ended: "结束", waiting: "等待" }[t.status] ?? t.status}
          </span>
        }
        detail={t.detail ? <Mono text={t.detail} /> : undefined}
      />
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
        style={{ width: Math.round(Math.min(size * 1.6, size * ratio)), height: size }}
        aria-label="查看图片"
      >
        <img src={src} alt="" loading="lazy" decoding="async" className="size-full object-cover" />
      </button>
      {open && <Lightbox src={src} onClose={() => setOpen(false)} />}
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
