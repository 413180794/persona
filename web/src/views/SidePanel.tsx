// The right-hand panel next to the page (docs/web.md「侧边面板」): one at a time, chosen in lib/panel.ts. (A session's
// terminal is not one: it is the agent page's own view, lib/session-view.ts.)
// Wide screens (lg+) get a resizable column that pushes the page; narrower ones an overlay over the page.
import { lazy, Suspense, useEffect, useRef, useState } from "react";
import { directoryPanelKey, closePanel, panelOpener, usePanel, type Panel } from "../lib/panel";
import { activeComposer } from "../lib/links";
import { returnFocus } from "../ui/overlay";
import { useLayout } from "../lib/layout";
import { cx, EASE, IconButton, Loading } from "../ui/atoms";
import { Icon, type IconName } from "../ui/icons";
import { AgentPanel, TaskPanel } from "./assistant/TaskPanel";
import { SubagentPanel } from "./chat/SubagentPanel";
import { subagentParent } from "../lib/tasks";
import { useCloseOnBack } from "../lib/back-close";
import { mediaMatches, NARROW, PHONE, useMedia } from "../lib/viewport";
import { t } from "../i18n";
import { PageBoundary } from "./PageBoundary";

// Files, Git, the file viewer and a link load when one is first opened (#2673): with their viewers (sheets, Office,
// compare, documents) they are a large part of the main bundle no first screen needs. The assistant's task / agent
// panels stay static: small, and part of the assistant flow.
const FilesPanel = lazy(() => import("./session/FilesPanel").then((m) => ({ default: m.FilesPanel })));
const FileViewerPanel = lazy(() => import("./session/FileViewer").then((m) => ({ default: m.FileViewerPanel })));
const GitPanel = lazy(() => import("./session/GitPanel").then((m) => ({ default: m.GitPanel })));
const LinkPanel = lazy(() => import("./link/LinkPanel").then((m) => ({ default: m.LinkPanel })));

const WIDTH_KEY = "jarvis.panelWidth";
const MIN = 360;

function clampWidth(w: number) {
  return Math.round(Math.max(MIN, Math.min(w, Math.max(MIN, innerWidth * 0.72))));
}

export function SidePanel() {
  const panel = usePanel();
  // ⌘⇧↵ shows only the middle: the panel stays open (its terminal keeps running) but is not drawn, on a wide screen
  const maximized = useLayout().maximized;
  // narrow, it covers the page: Back closes it first (and stays on the conversation)
  useCloseOnBack(Boolean(panel), closePanel, useMedia(NARROW));
  const [width, setWidth] = useState(() => clampWidth(Number(localStorage.getItem(WIDTH_KEY)) || 600));
  const drag = useRef<{ x: number; w: number } | null>(null);
  const aside = useRef<HTMLElement>(null);
  const open = Boolean(panel);
  // Closed (×, Esc, its button again): the focus goes back to the button that opened it, else to the message box, not
  // to the page (#5155). Not the message box on a phone: that would bring the keyboard up.
  useEffect(() => {
    const el = aside.current;
    if (!open || !el) return;
    return () => returnFocus(el, panelOpener(), mediaMatches(PHONE) ? undefined : () => activeComposer()?.focus());
  }, [open]);

  useEffect(() => {
    const move = (e: PointerEvent) => {
      if (!drag.current) return;
      setWidth(clampWidth(drag.current.w + (drag.current.x - e.clientX)));
    };
    const up = () => {
      if (!drag.current) return;
      drag.current = null;
      document.body.style.removeProperty("cursor");
      document.body.style.removeProperty("user-select");
      setWidth((w) => (localStorage.setItem(WIDTH_KEY, String(w)), w));
    };
    addEventListener("pointermove", move);
    addEventListener("pointerup", up);
    return () => {
      removeEventListener("pointermove", move);
      removeEventListener("pointerup", up);
    };
  }, []);

  if (!panel) return null;
  return (
    <aside
      ref={aside}
      aria-label={panelLabel(panel)}
      // narrow: over the visible part of the page (--kb-top / --kb-h, lib/viewport.ts), so on iOS the terminal's key
      // bar sits on the keyboard instead of under it
      className={cx(
        "fixed inset-x-0 top-(--kb-top) z-30 flex h-(--kb-h) flex-col bg-page shadow-overlay lg:relative lg:inset-auto lg:top-auto lg:z-auto lg:h-auto lg:w-(--panel-w) lg:shrink-0 lg:border-l lg:border-line lg:shadow-none",
        maximized && "lg:hidden",
      )}
      style={{ "--panel-w": `${width}px`, animation: `slide-in-right 260ms ${EASE} both` } as React.CSSProperties}
    >
      <div
        role="separator"
        aria-orientation="vertical"
        aria-label={t("sidepanel.resize_label")}
        title={t("sidepanel.resize_title")}
        onPointerDown={(e) => {
          drag.current = { x: e.clientX, w: width };
          document.body.style.cursor = "col-resize";
          document.body.style.userSelect = "none";
        }}
        onDoubleClick={() => {
          setWidth(clampWidth(600));
          localStorage.removeItem(WIDTH_KEY);
        }}
        className="absolute inset-y-0 -left-1 z-10 hidden w-2 cursor-col-resize lg:block"
      />
      <div className="flex min-h-0 flex-1 flex-col">
        {/* a chunk gone after a hub redeploy reloads once, else shows a retry; never a blank panel */}
        <PageBoundary page={JSON.stringify(panel)}>
          <Suspense fallback={<div className="flex flex-1 items-center justify-center"><Loading /></div>}>{body(panel)}</Suspense>
        </PageBoundary>
      </div>
    </aside>
  );
}

function panelLabel(p: Panel) {
  return p.kind === "files" ? t("sidepanel.files") : p.kind === "git" ? "Git" : p.kind === "file" ? t("sidepanel.files") : p.kind === "task" ? t("sidepanel.task") : p.kind === "agent" ? t("sidepanel.agent") : t("sidepanel.link");
}

function body(p: Panel) {
  switch (p.kind) {
    case "files":
      return <FilesPanel key={directoryPanelKey(p)} mid={p.mid} sid={p.sid} cwd={p.cwd} path={p.path} onClose={closePanel} />;
    case "git":
      return <GitPanel key={directoryPanelKey(p)} mid={p.mid} sid={p.sid} cwd={p.cwd} at={p.at} onClose={closePanel} />;
    case "file":
      return <FileViewerPanel key={`${p.mid}:${p.path}:${p.line ?? ""}-${p.to ?? ""}`} mid={p.mid} sid={p.sid} path={p.path} cwd={p.cwd} line={p.line} to={p.to} onClose={closePanel} />;
    case "link":
      return <LinkPanel key={p.url} url={p.url} title={p.title} onClose={closePanel} />;
    case "task":
      return <TaskPanel key={p.todo} todoId={p.todo} onClose={closePanel} />;
    case "agent":
      // a subagent's conversation ("<parent>@<agent>", its card's 查看完整过程), read-only
      if (subagentParent(p.sid) !== p.sid) return <SubagentPanel key={`${p.mid}/${p.sid}`} mid={p.mid} sid={p.sid} onClose={closePanel} />;
      return <AgentPanel key={`${p.mid}/${p.sid}`} mid={p.mid} sid={p.sid} onClose={closePanel} />;
  }
}

// The panel's header: an icon, a title (and a quieter line under it), actions, and ×.
export function PanelHeader({ icon, title, subtitle, actions, onClose }: { icon: IconName; title: React.ReactNode; subtitle?: React.ReactNode; actions?: React.ReactNode; onClose: () => void }) {
  return (
    <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line px-3">
      <span className="flex size-7 shrink-0 items-center justify-center rounded-[8px] bg-field text-ink-2">
        <Icon name={icon} size={14} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="truncate text-[13.5px] font-semibold text-ink">{title}</div>
        {subtitle && <div className="truncate text-[11.5px] text-ink-3">{subtitle}</div>}
      </div>
      {actions}
      <IconButton icon="x" label={t("sidepanel.close")} onClick={onClose} />
    </header>
  );
}
