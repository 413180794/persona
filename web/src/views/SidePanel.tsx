// The right-hand panel next to the page (docs/web.md「侧边面板」): one at a time, chosen in lib/panel.ts.
// Wide screens (lg+) get a resizable column that pushes the page; narrower ones an overlay over the page.
import { useEffect, useRef, useState } from "react";
import { directoryPanelKey, closePanel, usePanel, type Panel } from "../lib/panel";
import { EASE, IconButton } from "../ui/atoms";
import { Icon, type IconName } from "../ui/icons";
import { FilesPanel } from "./session/FilesPanel";
import { FileViewerPanel } from "./session/FileViewer";
import { GitPanel } from "./session/GitPanel";
import { TerminalPanel } from "./session/TerminalPanel";
import { LinkPanel } from "./link/LinkPanel";
import { AgentPanel, TaskPanel } from "./assistant/TaskPanel";

const WIDTH_KEY = "jarvis.panelWidth";
const MIN = 360;

function clampWidth(w: number) {
  return Math.round(Math.max(MIN, Math.min(w, Math.max(MIN, innerWidth * 0.72))));
}

export function SidePanel() {
  const panel = usePanel();
  const [width, setWidth] = useState(() => clampWidth(Number(localStorage.getItem(WIDTH_KEY)) || 600));
  const drag = useRef<{ x: number; w: number } | null>(null);

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
      aria-label={panelLabel(panel)}
      className="fixed inset-0 z-30 flex flex-col bg-page shadow-overlay lg:relative lg:inset-auto lg:z-auto lg:w-(--panel-w) lg:shrink-0 lg:border-l lg:border-line lg:shadow-none"
      style={{ "--panel-w": `${width}px`, animation: `slide-in-right 260ms ${EASE} both` } as React.CSSProperties}
    >
      <div
        role="separator"
        aria-orientation="vertical"
        aria-label="拖动调整面板宽度"
        title="拖动调整宽度"
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
      <div className="flex min-h-0 flex-1 flex-col">{body(panel)}</div>
    </aside>
  );
}

function panelLabel(p: Panel) {
  return p.kind === "terminal" ? "终端" : p.kind === "files" ? "文件" : p.kind === "git" ? "Git" : p.kind === "file" ? "文件" : p.kind === "task" ? "任务" : p.kind === "agent" ? "Agent" : "链接";
}

function body(p: Panel) {
  switch (p.kind) {
    case "terminal":
      return <TerminalPanel key={`${p.mid}/${p.sid}`} mid={p.mid} sid={p.sid} onClose={closePanel} />;
    case "files":
      return <FilesPanel key={directoryPanelKey(p)} mid={p.mid} sid={p.sid} cwd={p.cwd} path={p.path} onClose={closePanel} />;
    case "git":
      return <GitPanel key={directoryPanelKey(p)} mid={p.mid} sid={p.sid} cwd={p.cwd} onClose={closePanel} />;
    case "file":
      return <FileViewerPanel key={`${p.mid}:${p.path}:${p.line ?? ""}`} mid={p.mid} path={p.path} cwd={p.cwd} line={p.line} onClose={closePanel} />;
    case "link":
      return <LinkPanel key={p.url} url={p.url} title={p.title} onClose={closePanel} />;
    case "task":
      return <TaskPanel key={p.todo} todoId={p.todo} onClose={closePanel} />;
    case "agent":
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
      <IconButton icon="x" label="关闭面板" onClick={onClose} />
    </header>
  );
}
