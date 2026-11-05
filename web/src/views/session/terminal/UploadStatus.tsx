// The line that says files are on their way to the computer (pasted or dropped, terminal/transfer.ts): the same in the
// single-pane surface (TerminalPanel.tsx) and the full herdr surface (FullTerminal.tsx).
import { Spinner } from "../../../ui/atoms";

export function UploadStatus({ text }: { text: string }) {
  return (
    <div
      role="status"
      className="flex items-center gap-2 rounded-control bg-surface px-2.5 py-1.5 text-[12px] text-ink-2 shadow-raised"
      style={{ animation: "fade-in 160ms ease-out both" }}
    >
      <Spinner size={12} />
      <span className="min-w-0 flex-1 truncate">{text}</span>
    </div>
  );
}
