// UI version 4's code hosting components (docs/extensions.md「UI 版本 4」, #2951), in the web's design system: a detail
// page's header, a breadcrumb, a file tree, a file's code, a file's diff (one column or side by side, inline threads, a
// comment box at any line, hidden lines to show), a discussion and a comment box with its buttons. What they send goes
// through `act` (ViewBody's onAct): the action with the values it carries and the component's own (a path, a line, the
// text typed). Logic in lib/code-view.ts; how a section holds them is ViewBody.tsx.
import { Fragment, useEffect, useMemo, useRef, useState } from "react";
import { ago } from "../../lib/format";
import {
  ancestors, buildTree, filterEntries, gapBefore, highlightLines, hunkRows, hunkSides, languageOf, placeThreads, splitRows, statusMark,
  useDiffMode, useHighlighter, type Comment, type DiffRow, type Hunk, type Thread, type Token, type TreeNode,
} from "../../lib/code-view";
import { imageURL, initials, rowIcon, toneOf, type UIAction, type UIItem, type UITone } from "../../lib/extension-ui";
import { useMedia } from "../../lib/viewport";
import { t } from "../../i18n";
import { Button, cx, Spinner, toast } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";
import { Pill } from "../settings/SettingsParts";
import { ROW_ICON, ViewMarkdown } from "./ViewBody";
import "./code-view.css";

/** What a code component needs from the page: to send an action with values, and which one is under way. */
export interface CodeHandlers {
  act: (a: UIAction, values?: Record<string, string>) => void;
  busy: string;
  /** Scroll to a file's diff on the page (a file tree without an action). */
  reveal?: (path: string) => void;
}

const PILL: Record<UITone, "green" | "orange" | "red" | undefined> = { neutral: undefined, positive: "green", caution: "orange", danger: "red" };
const MARK: Record<UITone, string> = { neutral: "text-ink-3", positive: "text-green", caution: "text-orange", danger: "text-red" };

const card = "overflow-hidden rounded-card bg-surface shadow-card";
const LH = 20; // a code line's height, px

function Avatar({ avatar, name, size = 28 }: { avatar?: { url?: string; text?: string }; name: string; size?: number }) {
  const [failed, setFailed] = useState(false);
  const url = imageURL(avatar?.url);
  return (
    <span
      className="flex shrink-0 items-center justify-center overflow-hidden rounded-full bg-accent-tint font-medium text-accent-ink"
      style={{ width: size, height: size, fontSize: Math.round(size * 0.42) }}
    >
      {url && !failed ? <img src={url} alt="" referrerPolicy="no-referrer" loading="lazy" onError={() => setFailed(true)} className="size-full object-cover" /> : initials(avatar?.text, name)}
    </span>
  );
}

const iconOf = (name?: string): IconName | undefined => {
  const icon = rowIcon(name);
  return icon ? ROW_ICON[icon] : undefined;
};

// ── header ────────────────────────────────────────────

export function Header({ item }: { item: UIItem }) {
  return (
    <div className="flex gap-3 px-4 py-4 sm:px-5">
      {item.avatar && <Avatar avatar={item.avatar} name={item.title ?? ""} size={40} />}
      <div className="min-w-0 flex-1">
        <h2 className="text-[18px] leading-snug font-semibold break-words text-ink select-text sm:text-[20px]">{item.title}</h2>
        {item.subtitle && <p className="mt-0.5 truncate text-[12.5px] text-ink-3 select-text">{item.subtitle}</p>}
        {(item.badges?.length || item.facts?.length) && (
          <div className="mt-2.5 flex flex-wrap items-center gap-x-3.5 gap-y-1.5">
            {item.badges?.map((b, i) => (
              <Pill key={`b${i}`} tone={PILL[toneOf(b.tone)]}>
                {b.text}
              </Pill>
            ))}
            {item.facts?.map((f, i) => {
              const icon = iconOf(f.icon);
              return (
                <span key={`f${i}`} className="flex min-w-0 items-center gap-1 text-[12.5px] whitespace-nowrap text-ink-2" title={f.label}>
                  {icon && <Icon name={icon} size={13} className="shrink-0 text-ink-3" />}
                  {f.label && !icon && <span className="text-ink-3">{f.label}</span>}
                  <span className="truncate font-medium text-ink select-text">{f.value}</span>
                </span>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}

// ── breadcrumb ────────────────────────────────────────

export function Breadcrumb({ item, act, busy }: { item: UIItem } & CodeHandlers) {
  const crumbs = item.crumbs ?? [];
  return (
    <nav aria-label={t("extensions-codeviews.path")} className="flex min-w-0 items-center gap-0.5 overflow-x-auto px-3 py-2 text-[13px] whitespace-nowrap">
      {crumbs.map((c, i) => {
        const last = i === crumbs.length - 1;
        return (
          <Fragment key={i}>
            {i > 0 && <span className="px-0.5 text-ink-3">/</span>}
            {c.action && !last ? (
              <button
                type="button"
                disabled={!!busy}
                onClick={() => act(c.action!)}
                className="rounded-[6px] px-1 py-0.5 text-accent-ink transition-colors enabled:hover:bg-hover"
              >
                {c.title}
              </button>
            ) : (
              <span className={cx("px-1 py-0.5", last ? "font-semibold text-ink" : "text-ink-2")}>{c.title}</span>
            )}
          </Fragment>
        );
      })}
    </nav>
  );
}

// ── file tree ─────────────────────────────────────────

export function FileTree({ item, act, busy, reveal, onPicked }: { item: UIItem; onPicked?: () => void } & CodeHandlers) {
  const entries = item.entries ?? [];
  const [q, setQ] = useState("");
  const shown = useMemo(() => filterEntries(entries, q), [entries, q]);
  const tree = useMemo(() => buildTree(shown), [shown]);
  const [closed, setClosed] = useState<Set<string>>(() => new Set());
  const [asked, setAsked] = useState<string>("");
  const selected = item.selected ?? "";
  // the selected file's directories stay open
  useEffect(() => {
    if (!selected) return;
    setClosed((c) => {
      const open = ancestors(selected).filter((a) => c.has(a));
      if (!open.length) return c;
      const next = new Set(c);
      open.forEach((a) => next.delete(a));
      return next;
    });
  }, [selected]);
  const files = entries.filter((e) => e.kind !== "dir").length;
  const toggle = (n: TreeNode) => {
    if (n.unloaded) {
      if (item.expand && !busy) {
        setAsked(n.path);
        act(item.expand, { path: n.path });
      }
      return;
    }
    setClosed((c) => {
      const next = new Set(c);
      if (next.has(n.path)) next.delete(n.path);
      else next.add(n.path);
      return next;
    });
  };
  const open = (path: string) => {
    if (item.action) act(item.action, { path });
    else reveal?.(path);
    onPicked?.();
  };
  const rows: React.ReactNode[] = [];
  const walk = (nodes: TreeNode[], depth: number) => {
    for (const n of nodes) {
      const pad = { paddingLeft: 10 + depth * 14 };
      if (n.dir) {
        const isOpen = !n.unloaded && (!closed.has(n.path) || !!q);
        rows.push(
          <button
            key={`d:${n.path}`}
            type="button"
            onClick={() => toggle(n)}
            aria-expanded={n.unloaded ? false : isOpen}
            className="flex h-7 w-full items-center gap-1.5 pr-2.5 text-left text-[12.5px] whitespace-nowrap text-ink-2 transition-colors hover:bg-hover"
            style={pad}
          >
            {busy && asked === n.path ? (
              <Spinner size={11} />
            ) : (
              <Icon name="chevronRight" size={11} stroke={2.2} className={cx("shrink-0 text-ink-3 transition-transform duration-150", isOpen && "rotate-90")} />
            )}
            <Icon name="folder" size={13} className="shrink-0 text-ink-3" />
            <span className="min-w-0 flex-1 truncate">{n.name}</span>
          </button>,
        );
        if (isOpen) walk(n.children, depth + 1);
        continue;
      }
      const e = n.entry!;
      const mark = statusMark(e.status);
      const on = n.path === selected;
      rows.push(
        <button
          key={`f:${n.path}`}
          type="button"
          onClick={() => open(n.path)}
          aria-current={on ? "true" : undefined}
          title={n.path}
          className={cx("flex h-7 w-full items-center gap-1.5 pr-2.5 text-left text-[12.5px] whitespace-nowrap transition-colors", on ? "bg-accent-tint text-ink" : "text-ink hover:bg-hover")}
          style={{ paddingLeft: 10 + depth * 14 + 17 }}
        >
          <Icon name="read" size={13} className="shrink-0 text-ink-3" />
          <span className={cx("min-w-0 flex-1 truncate", e.status === "deleted" && "text-ink-3 line-through")}>{n.name}</span>
          {!!e.comments && (
            <span className="flex shrink-0 items-center gap-0.5 text-[11px] text-ink-3 tabular-nums">
              <Icon name="chat" size={11} />
              {e.comments}
            </span>
          )}
          {(!!e.additions || !!e.deletions) && (
            <span className="shrink-0 font-mono text-[10.5px] tabular-nums">
              {!!e.additions && <span className="text-green">+{e.additions}</span>}
              {!!e.deletions && <span className="ml-1 text-red">−{e.deletions}</span>}
            </span>
          )}
          {e.badge?.text && <Pill tone={PILL[toneOf(e.badge.tone as UITone)]}>{e.badge.text}</Pill>}
          {mark && <span className={cx("w-3 shrink-0 text-center font-mono text-[10.5px] font-semibold", MARK[mark.tone])} aria-label={e.status}>{mark.letter}</span>}
        </button>,
      );
    }
  };
  walk(tree, 0);
  return (
    <div className="py-1.5">
      {files > 12 && (
        <div className="px-2 pb-1.5">
          <label className="relative block">
            <Icon name="filter" size={12} className="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-ink-3" />
            <input
              type="search"
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder={t("extensions-codeviews.filter_files")}
              aria-label={t("extensions-codeviews.filter_files")}
              spellCheck={false}
              className="h-7 w-full rounded-control bg-field pr-2 pl-7 text-[12px] text-ink shadow-inset-field outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)]"
            />
          </label>
        </div>
      )}
      <div role="tree" aria-label={t("extensions-codeviews.files")}>
        {rows}
      </div>
      {!rows.length && <p className="px-3 py-3 text-center text-[12px] text-ink-3">{q ? t("extensions-codeviews.no_match") : t("extensions-codeviews.no_files")}</p>}
    </div>
  );
}

// ── code ──────────────────────────────────────────────

function Tokens({ tokens, text }: { tokens?: Token[] | null; text: string }) {
  if (!tokens) return <>{text || " "}</>;
  if (!tokens.length) return <> </>;
  return (
    <>
      {tokens.map((tk, i) =>
        tk.cls ? (
          <span key={i} className={tk.cls}>
            {tk.text}
          </span>
        ) : (
          <Fragment key={i}>{tk.text}</Fragment>
        ),
      )}
    </>
  );
}

function copy(text: string) {
  void navigator.clipboard?.writeText(text).then(
    () => toast(t("extensions-codeviews.copied"), "ok"),
    () => {},
  );
}

/** A file: line numbers, highlighting, the highlighted lines, and the rest to load when it was cut. */
export function CodeView({ item, act, busy }: { item: UIItem } & CodeHandlers) {
  const text = item.text ?? "";
  const lines = useMemo(() => text.replace(/\n$/, "").split("\n"), [text]);
  const lang = languageOf(item.language, item.path);
  const ready = useHighlighter(!!lang);
  const tokens = useMemo(() => (ready ? highlightLines(text, lang) : null), [ready, text, lang]);
  const start = item.start_line ?? 1;
  const digits = String(start + lines.length).length;
  const hl = item.highlight;
  const marked = useRef<HTMLDivElement>(null);
  useEffect(() => {
    marked.current?.scrollIntoView?.({ block: "nearest" });
  }, [hl?.start, item.path]);
  return (
    <div className={card}>
      <div className="flex h-10 items-center gap-2 border-b border-line px-3.5">
        <Icon name="read" size={13} className="shrink-0 text-ink-3" />
        <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-ink select-text">{item.path || t("extensions-codeviews.file")}</span>
        <span className="shrink-0 text-[11.5px] text-ink-3 tabular-nums">{t("extensions-codeviews.lines", { count: lines.length })}</span>
        <button
          type="button"
          onClick={() => copy(text)}
          aria-label={t("extensions-codeviews.copy")}
          title={t("extensions-codeviews.copy")}
          className="flex size-7 shrink-0 items-center justify-center rounded-[8px] text-ink-3 transition-colors hover:bg-hover hover:text-ink"
        >
          <Icon name="copy" size={13} />
        </button>
      </div>
      <div className="code-hl overflow-x-auto bg-inset font-mono text-[12px]" style={{ lineHeight: `${LH}px` }}>
        <div className="min-w-max py-1.5">
          {lines.map((l, i) => {
            const n = start + i;
            const on = !!hl && n >= hl.start && n <= hl.end;
            return (
              <div key={i} ref={on && n === hl!.start ? marked : undefined} className={cx("flex", on && "bg-accent-tint")}>
                <span className="sticky left-0 shrink-0 bg-inset pr-3 pl-3 text-right text-ink-3 tabular-nums select-none" style={{ width: `calc(${digits}ch + 24px)`, background: on ? "transparent" : undefined }}>
                  {n}
                </span>
                <span className="pr-6 whitespace-pre text-ink">
                  <Tokens tokens={tokens?.[i]} text={l} />
                </span>
              </div>
            );
          })}
        </div>
      </div>
      {item.truncated && item.load && (
        <LoadLine busy={busy === item.load.id} disabled={!!busy} onClick={() => act(item.load!, { path: item.path ?? "", from: String(start + lines.length) })} title={t("extensions-codeviews.load_rest")} />
      )}
    </div>
  );
}

function LoadLine({ busy, disabled, onClick, title }: { busy: boolean; disabled: boolean; onClick: () => void; title: string }) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className="flex w-full items-center justify-center gap-2 border-t border-line px-4 py-2.5 text-[12.5px] whitespace-nowrap text-accent-ink transition-colors enabled:hover:bg-hover disabled:text-ink-3"
    >
      {busy && <Spinner />}
      {title}
    </button>
  );
}

// ── comments ──────────────────────────────────────────

function CommentLine({ c }: { c: Comment }) {
  if (c.event)
    return (
      <div className="flex items-center gap-2 px-4 py-2 text-[12px] text-ink-3">
        <span className="flex size-6 shrink-0 items-center justify-center">
          <span className="size-1.5 rounded-full bg-line-strong" />
        </span>
        <span className="min-w-0 flex-1 break-words">
          <span className="font-medium text-ink-2">{c.author}</span> {c.body}
        </span>
        {c.at && <time dateTime={c.at} className="shrink-0 tabular-nums">{ago(c.at)}</time>}
      </div>
    );
  return (
    <div className="flex gap-2.5 px-4 py-2.5">
      <Avatar avatar={c.avatar} name={c.author} size={26} />
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2 whitespace-nowrap">
          <span className="truncate text-[12.5px] font-semibold text-ink">{c.author}</span>
          {c.at && <time dateTime={c.at} title={new Date(c.at).toLocaleString()} className="shrink-0 text-[11.5px] text-ink-3 tabular-nums">{ago(c.at)}</time>}
          {c.badge?.text && <Pill tone={PILL[toneOf(c.badge.tone as UITone)]}>{c.badge.text}</Pill>}
        </div>
        {c.body && (
          <div className="-mx-4 -mb-2">
            <ViewMarkdown text={c.body} compact />
          </div>
        )}
      </div>
    </div>
  );
}

/** A text box that grows with what is typed; ⌘/Ctrl+Enter presses the first button. */
function Box({ value, onChange, placeholder, autoFocus, onSubmit, disabled }: { value: string; onChange: (v: string) => void; placeholder?: string; autoFocus?: boolean; onSubmit?: () => void; disabled?: boolean }) {
  const ref = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(320, Math.max(72, el.scrollHeight))}px`;
  }, [value]);
  return (
    <textarea
      ref={ref}
      value={value}
      autoFocus={autoFocus}
      disabled={disabled}
      onChange={(e) => onChange(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && onSubmit) {
          e.preventDefault();
          onSubmit();
        }
      }}
      placeholder={placeholder}
      aria-label={placeholder}
      className="block w-full resize-none rounded-control bg-field px-3 py-2 text-[13px] leading-relaxed text-ink shadow-inset-field outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)] disabled:opacity-60"
    />
  );
}

/** A small comment box: text and one button (a reply, an inline comment), with cancel when it can close. */
function MiniComposer({ placeholder, send, busy, disabled, onCancel, autoFocus }: { placeholder: string; send: (body: string) => void; busy: boolean; disabled: boolean; onCancel?: () => void; autoFocus?: boolean }) {
  const [body, setBody] = useState("");
  const go = () => {
    if (!body.trim() || disabled) return;
    send(body.trim());
    setBody("");
  };
  return (
    <div className="flex flex-col gap-2">
      <Box value={body} onChange={setBody} placeholder={placeholder} autoFocus={autoFocus} onSubmit={go} disabled={disabled && !busy} />
      <div className="flex items-center justify-end gap-2">
        {onCancel && (
          <Button tone="quiet" onClick={onCancel}>
            {t("extensions-codeviews.cancel")}
          </Button>
        )}
        <Button tone="primary" busy={busy} disabled={!body.trim() || disabled} onClick={go}>
          {t("extensions-codeviews.comment")}
        </Button>
      </div>
    </div>
  );
}

/** A discussion: its comments in order, and a reply box. */
export function CommentThread({ item, act, busy }: { item: UIItem } & CodeHandlers) {
  const comments = item.comments ?? [];
  const [replying, setReplying] = useState(false);
  return (
    <div className={card}>
      {item.title && (
        <div className="flex items-center gap-2 border-b border-line px-4 py-2.5">
          <span className="min-w-0 flex-1 truncate text-[12.5px] font-medium text-ink-2">{item.title}</span>
          {item.resolved && <Pill tone="green">{t("extensions-codeviews.resolved")}</Pill>}
        </div>
      )}
      <div className="divide-y divide-line">
        {comments.map((c, i) => (
          <CommentLine key={c.id ?? i} c={c} />
        ))}
      </div>
      {!comments.length && <p className="px-4 py-4 text-center text-[12.5px] text-ink-3">{t("extensions-codeviews.no_comments")}</p>}
      {item.reply && (
        <div className="border-t border-line px-4 py-3">
          {replying ? (
            <MiniComposer
              autoFocus
              placeholder={t("extensions-codeviews.reply")}
              busy={busy === item.reply.id}
              disabled={!!busy}
              onCancel={() => setReplying(false)}
              send={(body) => {
                act(item.reply!, { body });
                setReplying(false);
              }}
            />
          ) : (
            <button type="button" onClick={() => setReplying(true)} className="h-8 w-full rounded-control bg-field px-3 text-left text-[12.5px] text-ink-3 shadow-inset-field transition-colors hover:text-ink-2">
              {t("extensions-codeviews.reply")}
            </button>
          )}
        </div>
      )}
    </div>
  );
}

const BUTTON_TONE: Record<UITone, "primary" | "secondary" | "danger" | "accent"> = { neutral: "secondary", positive: "accent", caution: "secondary", danger: "danger" };

/** A comment box with its buttons: the one pressed sends the text as the composer's name. */
export function Composer({ item, act, busy }: { item: UIItem } & CodeHandlers) {
  const [body, setBody] = useState(item.value ?? "");
  useEffect(() => setBody(item.value ?? ""), [item.value]);
  const buttons = item.buttons ?? [];
  const press = (a: UIAction) => act(a, { [item.name ?? "body"]: body });
  return (
    <div className="flex flex-col gap-2.5 px-4 py-3">
      <Box value={body} onChange={setBody} placeholder={item.placeholder || t("extensions-codeviews.leave_comment")} onSubmit={() => buttons[0] && press(buttons[0].action)} disabled={!!busy} />
      <div className="flex flex-wrap items-center justify-end gap-2">
        <span className="mr-auto hidden text-[11.5px] text-ink-3 sm:inline">{t("extensions-codeviews.markdown_hint")}</span>
        {buttons.map((b, i) => (
          <Button key={i} tone={i === 0 && toneOf(b.tone) === "neutral" ? "primary" : BUTTON_TONE[toneOf(b.tone)]} busy={busy === b.action.id} disabled={!!busy && busy !== b.action.id} onClick={() => press(b.action)} title={b.title}>
            {b.title}
          </Button>
        ))}
      </div>
    </div>
  );
}

// ── diff ──────────────────────────────────────────────

const ROW_BG: Record<DiffRow["kind"], string> = { add: "bg-diff-add-bg", del: "bg-diff-del-bg", context: "", note: "" };
const GUTTER_BG: Record<DiffRow["kind"], string> = { add: "bg-diff-add-bg", del: "bg-diff-del-bg", context: "bg-surface", note: "bg-surface" };
const SIGN: Record<DiffRow["kind"], string> = { add: "+", del: "−", context: " ", note: "" };

/** Where a comment box is open: a side and a line. */
type Spot = { side: "old" | "new"; line: number };
const spotKey = (s: Spot) => `${s.side}:${s.line}`;

function ThreadCard({ thread, act, busy }: { thread: Thread } & CodeHandlers) {
  const [open, setOpen] = useState(!thread.resolved);
  const [replying, setReplying] = useState(false);
  const first = thread.comments[0];
  return (
    <div className="mx-3 my-2 overflow-hidden rounded-control bg-surface font-sans shadow-card sm:max-w-[760px]">
      <button type="button" onClick={() => setOpen((o) => !o)} aria-expanded={open} className="flex w-full items-center gap-2 border-b border-line px-3 py-1.5 text-left text-[11.5px] whitespace-nowrap text-ink-3 transition-colors hover:bg-hover">
        <Icon name="chevronRight" size={10} stroke={2.2} className={cx("shrink-0 transition-transform", open && "rotate-90")} />
        <span className="min-w-0 flex-1 truncate">
          {open ? t("extensions-codeviews.thread_lines", { count: thread.comments.length }) : `${first?.author ?? ""}: ${first?.body ?? ""}`}
        </span>
        {thread.outdated && <Pill tone="orange">{t("extensions-codeviews.outdated")}</Pill>}
        {thread.resolved && <Pill tone="green">{t("extensions-codeviews.resolved")}</Pill>}
      </button>
      {open && (
        <>
          <div className="divide-y divide-line">
            {thread.comments.map((c, i) => (
              <CommentLine key={c.id ?? i} c={c} />
            ))}
          </div>
          {thread.reply && (
            <div className="border-t border-line px-3 py-2.5">
              {replying ? (
                <MiniComposer
                  autoFocus
                  placeholder={t("extensions-codeviews.reply")}
                  busy={busy === thread.reply.id}
                  disabled={!!busy}
                  onCancel={() => setReplying(false)}
                  send={(body) => {
                    act(thread.reply as UIAction, { ...(thread.id ? { thread: thread.id } : {}), body });
                    setReplying(false);
                  }}
                />
              ) : (
                <button type="button" onClick={() => setReplying(true)} className="h-8 w-full rounded-control bg-field px-3 text-left text-[12.5px] text-ink-3 shadow-inset-field transition-colors hover:text-ink-2">
                  {t("extensions-codeviews.reply")}
                </button>
              )}
            </div>
          )}
        </>
      )}
    </div>
  );
}

/** A number in the gutter: a button that opens a comment box at its line when the diff takes comments. */
function LineNo({ n, kind, canComment, onComment, width }: { n?: number; kind: DiffRow["kind"]; canComment: boolean; onComment: () => void; width: string }) {
  const cls = cx("group/ln shrink-0 pr-2 text-right text-ink-3 tabular-nums select-none", GUTTER_BG[kind]);
  if (n === undefined) return <span className={cls} style={{ width }} />;
  if (!canComment)
    return (
      <span className={cls} style={{ width }}>
        {n}
      </span>
    );
  return (
    <button type="button" onClick={onComment} aria-label={t("extensions-codeviews.comment_on", { n })} className={cx(cls, "relative cursor-pointer hover:text-accent-ink")} style={{ width }}>
      <span className="absolute top-1/2 left-1 flex size-4 -translate-y-1/2 items-center justify-center rounded-[4px] bg-accent text-white opacity-0 transition-opacity group-hover/ln:opacity-100">
        <Icon name="plus" size={10} stroke={2.6} />
      </span>
      {n}
    </button>
  );
}

/** One file's diff. */
export function DiffView({ item, act, busy }: { item: UIItem } & CodeHandlers) {
  const hunks = item.hunks ?? [];
  const path = item.path ?? "";
  const [mode, setMode] = useDiffMode();
  const roomy = useMedia("(min-width: 1024px)");
  const split = mode === "split" && roomy;
  const [open, setOpen] = useState(!item.collapsed);
  useEffect(() => setOpen(!item.collapsed), [item.collapsed]);
  const [spot, setSpot] = useState<Spot | null>(null);
  const lang = languageOf(item.language, path);
  const ready = useHighlighter(open && !!lang && hunks.length > 0);
  const lit = useMemo(
    () =>
      ready && open
        ? hunks.map((h) => {
            const s = hunkSides(h);
            return { old: highlightLines(s.old, lang), new: highlightLines(s.new, lang) };
          })
        : null,
    [ready, open, hunks, lang],
  );
  const rowsOf = useMemo(() => hunks.map(hunkRows), [hunks]);
  const { at, rest } = useMemo(() => placeThreads(item.threads ?? [], hunks), [item.threads, hunks]);
  const digits = useMemo(() => String(hunks.reduce((m, h) => Math.max(m, h.old_start + h.lines.length, h.new_start + h.lines.length), 99)).length, [hunks]);
  const num = `calc(${digits}ch + 14px)`;
  const comment = item.comment;
  const mark = statusMark(item.status);
  const threads = item.threads?.length ?? 0;

  const sendComment = (s: Spot, body: string) => {
    if (!comment) return;
    act(comment, { path, side: s.side, line: String(s.line), body });
    setSpot(null);
  };
  const boxAt = (s: Spot) =>
    spot && spotKey(spot) === spotKey(s) ? (
      <div key={`box:${spotKey(s)}`} className="border-y border-line bg-surface px-3 py-2.5 font-sans">
        <div className="sm:max-w-[760px]">
          <MiniComposer autoFocus placeholder={t("extensions-codeviews.comment_on", { n: s.line })} busy={busy === comment?.id} disabled={!!busy} onCancel={() => setSpot(null)} send={(b) => sendComment(s, b)} />
        </div>
      </div>
    ) : null;
  const threadsAt = (s: Spot) => (at.get(spotKey(s)) ?? []).map((th, i) => <ThreadCard key={th.id ?? i} thread={th} act={act} busy={busy} />);
  // what goes under a row: the threads at its lines, and the open comment box
  const under = (r: { old?: DiffRow; new?: DiffRow }) => {
    const spots: Spot[] = [];
    if (r.old?.old !== undefined && r.old.kind !== "context") spots.push({ side: "old", line: r.old.old });
    if (r.new?.new !== undefined) spots.push({ side: "new", line: r.new.new });
    else if (r.old?.kind === "context" && r.old.new !== undefined) spots.push({ side: "new", line: r.old.new });
    if (r.old?.kind === "context" && r.old.old !== undefined) spots.push({ side: "old", line: r.old.old });
    const out = spots.flatMap((s) => [...threadsAt(s), boxAt(s)]).filter(Boolean);
    return out.length ? <div className="sticky left-0 w-full max-w-[calc(100vw-2rem)]">{out}</div> : null;
  };
  const tokensOf = (h: number, r: DiffRow, side: "old" | "new"): Token[] | null | undefined => {
    const at = side === "old" ? r.oldAt : r.newAt;
    return at === undefined ? undefined : lit?.[h]?.[side]?.[at];
  };
  const gap = (h: number) => {
    const n = gapBefore(hunks, h);
    if (!item.expand || (h > 0 && n === 0)) return null;
    if (h === 0 && n === 0) return null;
    return (
      <div className="flex h-8 items-center gap-1 bg-accent-tint/50 font-sans text-[11.5px] text-accent-ink">
        <span className="sticky left-0 flex items-center gap-1 px-2">
          {(["up", ...(h > 0 && n > 20 ? ["down"] : [])] as const).map((dir) => (
            <button
              key={dir}
              type="button"
              disabled={!!busy}
              onClick={() => act(item.expand!, { path, hunk: String(h), direction: dir })}
              aria-label={dir === "up" ? t("extensions-codeviews.expand_up") : t("extensions-codeviews.expand_down")}
              title={dir === "up" ? t("extensions-codeviews.expand_up") : t("extensions-codeviews.expand_down")}
              className="flex size-6 items-center justify-center rounded-[6px] transition-colors enabled:hover:bg-hover"
            >
              <Icon name={dir === "up" ? "up" : "arrowDown"} size={12} stroke={2.2} />
            </button>
          ))}
          <span className="whitespace-nowrap text-ink-3">{n > 0 ? t("extensions-codeviews.hidden_lines", { count: n }) : hunks[h].header}</span>
        </span>
      </div>
    );
  };
  const hunkHeader = (h: Hunk, i: number) =>
    h.header && (!item.expand || gapBefore(hunks, i) === 0) ? (
      <div className="flex h-7 items-center bg-accent-tint/40 text-[11.5px] text-ink-3">
        <span className="sticky left-0 truncate px-3 whitespace-pre">{h.header}</span>
      </div>
    ) : null;

  const body = () => {
    if (item.binary) return <p className="px-4 py-4 text-center text-[12.5px] text-ink-3">{t("extensions-codeviews.binary")}</p>;
    if (!hunks.length)
      return item.load ? (
        <LoadLine busy={busy === item.load.id} disabled={!!busy} onClick={() => act(item.load!, { path })} title={t("extensions-codeviews.load_diff")} />
      ) : (
        <p className="px-4 py-4 text-center text-[12.5px] text-ink-3">{t("extensions-codeviews.no_changes")}</p>
      );
    return (
      <div className="code-hl overflow-x-auto font-mono text-[12px]" style={{ lineHeight: `${LH}px` }}>
        <div className={cx(split ? "min-w-full" : "min-w-max")}>
          {hunks.map((h, hi) => (
            <Fragment key={hi}>
              {gap(hi)}
              {hunkHeader(h, hi)}
              {split
                ? splitRows(rowsOf[hi]).map((r, k) =>
                    r.note !== undefined ? (
                      <div key={k} className="px-3 text-[11.5px] text-ink-3 italic">{r.note}</div>
                    ) : (
                      <Fragment key={k}>
                        <div className="grid grid-cols-2">
                          {(["old", "new"] as const).map((side) => {
                            const row = r[side];
                            const n = side === "old" ? row?.old : row?.new;
                            const kind = row ? (row.kind === "context" ? "context" : row.kind) : "context";
                            return (
                              <div key={side} className={cx("flex min-w-0", row ? ROW_BG[kind] : "bg-hover/60", side === "new" && "border-l border-line")}>
                                <LineNo n={n} kind={row ? kind : "context"} width={num} canComment={!!comment && n !== undefined} onComment={() => row && setSpot(row.kind === "context" ? { side: "new", line: row.new! } : { side, line: n! })} />
                                <span className={cx("w-[2ch] shrink-0 text-center select-none", kind === "add" ? "text-green" : kind === "del" ? "text-red" : "text-ink-3")}>{row ? SIGN[kind] : ""}</span>
                                <span className="min-w-0 flex-1 overflow-hidden pr-4 whitespace-pre text-ink">{row && <Tokens tokens={tokensOf(hi, row, side)} text={row.text} />}</span>
                              </div>
                            );
                          })}
                        </div>
                        {under(r)}
                      </Fragment>
                    ),
                  )
                : rowsOf[hi].map((r, k) =>
                    r.kind === "note" ? (
                      <div key={k} className="px-3 text-[11.5px] text-ink-3 italic">{r.text}</div>
                    ) : (
                      <Fragment key={k}>
                        <div className={cx("flex", ROW_BG[r.kind])}>
                          <LineNo n={r.old} kind={r.kind} width={num} canComment={!!comment && r.old !== undefined && r.kind === "del"} onComment={() => setSpot({ side: "old", line: r.old! })} />
                          <LineNo n={r.new} kind={r.kind} width={num} canComment={!!comment && r.new !== undefined} onComment={() => setSpot({ side: "new", line: r.new! })} />
                          <span className={cx("w-[2ch] shrink-0 text-center select-none", r.kind === "add" ? "text-green" : r.kind === "del" ? "text-red" : "text-ink-3")}>{SIGN[r.kind]}</span>
                          <span className="pr-6 whitespace-pre text-ink">
                            <Tokens tokens={tokensOf(hi, r, r.kind === "del" ? "old" : "new")} text={r.text} />
                          </span>
                        </div>
                        {under(r.kind === "context" ? { old: r } : r.kind === "del" ? { old: r } : { new: r })}
                      </Fragment>
                    ),
                  )}
            </Fragment>
          ))}
          {item.expand && hunks.length > 0 && (
            <div className="flex h-8 items-center bg-accent-tint/50 font-sans text-[11.5px] text-accent-ink">
              <span className="sticky left-0 flex items-center gap-1 px-2">
                <button
                  type="button"
                  disabled={!!busy}
                  onClick={() => act(item.expand!, { path, hunk: String(hunks.length - 1), direction: "down" })}
                  aria-label={t("extensions-codeviews.expand_down")}
                  title={t("extensions-codeviews.expand_down")}
                  className="flex size-6 items-center justify-center rounded-[6px] transition-colors enabled:hover:bg-hover"
                >
                  <Icon name="arrowDown" size={12} stroke={2.2} />
                </button>
              </span>
            </div>
          )}
        </div>
        {rest.length > 0 && (
          <div className="border-t border-line py-1 font-sans">
            <p className="px-4 pt-1.5 text-[11.5px] text-ink-3">{t("extensions-codeviews.elsewhere")}</p>
            {rest.map((th, i) => (
              <ThreadCard key={th.id ?? i} thread={th} act={act} busy={busy} />
            ))}
          </div>
        )}
      </div>
    );
  };

  return (
    <div className={cx(card, "code-lazy")} data-diff-path={path}>
      <div className={cx("sticky top-0 z-[2] flex h-10 items-center gap-2 bg-surface px-2.5", open && "border-b border-line")}>
        <button type="button" onClick={() => setOpen((o) => !o)} aria-expanded={open} aria-label={open ? t("extensions-codeviews.collapse") : t("extensions-codeviews.expand")} className="flex size-7 shrink-0 items-center justify-center rounded-[8px] text-ink-3 transition-colors hover:bg-hover hover:text-ink">
          <Icon name="chevronRight" size={12} stroke={2.2} className={cx("transition-transform duration-150", open && "rotate-90")} />
        </button>
        {mark && <span className={cx("w-3 shrink-0 text-center font-mono text-[11px] font-semibold", MARK[mark.tone])} aria-label={item.status}>{mark.letter}</span>}
        <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-ink select-text" title={path}>
          {item.old_path && item.old_path !== path && <span className="text-ink-3">{item.old_path} → </span>}
          {path}
        </span>
        {threads > 0 && (
          <span className="flex shrink-0 items-center gap-0.5 text-[11.5px] text-ink-3 tabular-nums">
            <Icon name="chat" size={12} />
            {threads}
          </span>
        )}
        <span className="shrink-0 font-mono text-[11.5px] tabular-nums">
          <span className="text-green">+{item.additions ?? 0}</span> <span className="text-red">−{item.deletions ?? 0}</span>
        </span>
        {roomy && open && hunks.length > 0 && (
          <div role="radiogroup" aria-label={t("extensions-codeviews.view_mode")} className="ml-1 hidden shrink-0 rounded-[8px] bg-field p-0.5 lg:flex">
            {(["unified", "split"] as const).map((m) => (
              <button
                key={m}
                type="button"
                role="radio"
                aria-checked={mode === m}
                onClick={() => setMode(m)}
                className={cx("h-6 rounded-[6px] px-2 text-[11.5px] whitespace-nowrap transition-colors", mode === m ? "bg-surface text-ink shadow-btn" : "text-ink-3 hover:text-ink")}
              >
                {m === "unified" ? t("extensions-codeviews.unified") : t("extensions-codeviews.split")}
              </button>
            ))}
          </div>
        )}
      </div>
      {(open || !hunks.length) && body()}
    </div>
  );
}
