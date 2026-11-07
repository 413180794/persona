// A subagent's report, folded to one line until the reader opens it (#6381): its first heading, or its first words.
// The reader's choice is kept per card in the browser (`report:<id>`), so a reload shows it as they left it.
import { useState } from "react";
import { t } from "../../i18n";
import { cx } from "../../ui/atoms";
import { Icon } from "../../ui/icons";

/** How much of the report's first line the folded card shows. */
const FOLDED_CHARS = 80;

/** The line Claude Code puts before a subagent's report for the model (2.1.319): the report follows it. */
const HANDBACK_FRAME = "[Subagent hand-back]";

/** Text that starts with Claude Code's hand-back frame: only for how it is drawn (a node tells hand-backs by their origin). */
export function isHandbackText(text: string): boolean {
  return text.trimStart().startsWith(HANDBACK_FRAME);
}

/** The folded line: the report's first Markdown heading, else its first line of text (after a hand-back's frame),
 *  cut to FOLDED_CHARS. */
export function reportLine(text: string): string {
  let lines = text.split("\n").map((l) => l.trim()).filter(Boolean);
  if (lines[0]?.startsWith(HANDBACK_FRAME)) lines = lines.slice(1);
  const heading = lines.find((l) => /^#{1,6}\s+\S/.test(l));
  const line = (heading ?? lines[0] ?? "").replace(/^#{1,6}\s+/, "").replace(/\*\*|__|`/g, "").trim();
  const chars = Array.from(line);
  return chars.length > FOLDED_CHARS ? chars.slice(0, FOLDED_CHARS - 1).join("") + "…" : line;
}

const storageKey = (id: string) => `report:${id}`;

function readOpen(id: string): boolean {
  try {
    return localStorage.getItem(storageKey(id)) === "1";
  } catch {
    return false;
  }
}

function writeOpen(id: string, open: boolean) {
  try {
    if (open) localStorage.setItem(storageKey(id), "1");
    else localStorage.removeItem(storageKey(id));
  } catch {
    // a browser that keeps nothing: the choice lasts until the page reloads
  }
}

/** The reader's choice for report `id`, kept across reloads. */
export function useReportOpen(id: string): [boolean, () => void] {
  const [seen, setSeen] = useState(id);
  const [open, setOpen] = useState(() => readOpen(id));
  if (seen !== id) {
    setSeen(id);
    setOpen(readOpen(id));
  }
  return [
    open,
    () => {
      writeOpen(id, !open);
      setOpen(!open);
    },
  ];
}

/** A report, one line folded with "Expand" after it; open, the whole text (drawn by `children`) and "Collapse". */
export function FoldedReport({ id, text, className, children }: { id: string; text: string; className?: string; children: React.ReactNode }) {
  const [open, toggle] = useReportOpen(id);
  const line = reportLine(text);
  if (!line) return null;
  return (
    <div className={cx("flex min-w-0 flex-col gap-1", className)} data-report={open ? "open" : "folded"}>
      {open ? (
        children
      ) : (
        <button type="button" onClick={toggle} aria-expanded={false} className="flex min-w-0 items-baseline gap-2 text-left" data-report-line>
          <span className="min-w-0 flex-1 truncate text-ink">{line}</span>
          <span className="shrink-0 text-[12px] font-medium whitespace-nowrap text-accent-ink">{t("chat-report.expand")}</span>
        </button>
      )}
      {open && (
        <button type="button" onClick={toggle} aria-expanded className="flex w-fit items-center gap-1 text-[12px] font-medium whitespace-nowrap text-accent-ink" data-report-collapse>
          <Icon name="up" size={12} />
          {t("chat-report.collapse")}
        </button>
      )}
    </div>
  );
}
