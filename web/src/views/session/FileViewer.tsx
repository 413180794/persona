// The file viewer as its own panel (a path from a tool step or a message; docs/ios.md「文件查看器」). Folders and
// Markdown links open further paths in the same panel, with back.
import { useState } from "react";
import { FileView } from "./files/FileView";

export function FileViewerPanel({ mid, path, cwd, line, onClose }: { mid: string; path: string; cwd?: string; line?: number; onClose: () => void }) {
  const [stack, setStack] = useState<{ path: string; line?: number }[]>([{ path, line }]);
  const top = stack[stack.length - 1];
  return (
    <FileView
      key={`${stack.length}:${top.path}`}
      mid={mid}
      path={top.path}
      cwd={cwd}
      line={top.line}
      onClose={onClose}
      onBack={stack.length > 1 ? () => setStack((s) => s.slice(0, -1)) : undefined}
      onOpenPath={(p) => setStack((s) => [...s, { path: p }])}
    />
  );
}
