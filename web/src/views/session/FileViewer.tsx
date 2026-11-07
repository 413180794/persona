// The file viewer as its own panel (a path from a tool step or a message; docs/apple/ios.md "File viewer"). Folders and
// Markdown links open further paths in the same panel, with back.
import { useState } from "react";
import { FileView } from "./files/FileView";

export function FileViewerPanel({ mid, sid, path, cwd, line, to, onClose }: { mid: string; sid?: string; path: string; cwd?: string; line?: number; to?: number; onClose: () => void }) {
  const [stack, setStack] = useState<{ path: string; line?: number; to?: number }[]>([{ path, line, to }]);
  const top = stack[stack.length - 1];
  return (
    <FileView
      key={`${stack.length}:${top.path}`}
      mid={mid}
      sid={sid}
      path={top.path}
      cwd={cwd}
      line={top.line}
      lineTo={top.to}
      onClose={onClose}
      onBack={stack.length > 1 ? () => setStack((s) => s.slice(0, -1)) : undefined}
      onOpenPath={(p) => setStack((s) => [...s, { path: p }])}
    />
  );
}
