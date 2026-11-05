// A unified diff with old / new line numbers, green / red rows, no wrapping (scroll sideways); only the rows on
// screen are rendered (a diff can be 512 KB).
import { useMemo, useRef } from "react";
import { columns, parseDiff, type DiffLine } from "../../../lib/files";
import { cx } from "../../../ui/atoms";
import { Empty, useWindow } from "./parts";
import { t } from "../../../i18n";

const LH = 20;
const PAD = 6;

const ROW: Record<DiffLine["kind"], string> = {
  add: "bg-green-tint",
  del: "bg-red-tint",
  context: "",
  hunk: "bg-field text-ink-3",
  info: "text-ink-3 italic",
};

export function DiffLines({ diff, truncated }: { diff: string; truncated?: boolean }) {
  const lines = useMemo(() => parseDiff(diff), [diff]);
  const width = useMemo(() => lines.reduce((m, l) => Math.max(m, columns(l.text)), 0), [lines]);
  const digits = useMemo(() => String(lines.reduce((m, l) => Math.max(m, l.old ?? 0, l.new ?? 0), 99)).length, [lines]);
  const scroller = useRef<HTMLDivElement>(null);
  const [start, end] = useWindow(scroller, lines.length, LH, PAD);

  if (!lines.length) return <Empty icon="git" title={t("session-files-difflines.empty_title")} detail={t("session-files-difflines.empty_detail")} />;
  const num = `calc(${digits}ch + 12px)`;
  return (
    <div ref={scroller} className="min-h-0 flex-1 overflow-auto bg-inset font-mono text-[12px]" style={{ lineHeight: `${LH}px` }} data-testid="diff">
      <div style={{ minWidth: `calc(${digits * 2}ch + ${width}ch + 24px + 2ch + 40px)`, paddingTop: PAD, paddingBottom: PAD }}>
        <div style={{ height: start * LH }} />
        {lines.slice(start, end).map((l, k) => (
          <div key={start + k} className={cx("flex", ROW[l.kind])} style={{ height: LH }}>
            {l.kind === "hunk" || l.kind === "info" ? (
              <span className="pl-3 whitespace-pre">{l.text}</span>
            ) : (
              <>
                <span className="shrink-0 pr-1.5 text-right text-ink-3 tabular-nums select-none" style={{ width: num }}>
                  {l.old ?? ""}
                </span>
                <span className="shrink-0 pr-1.5 text-right text-ink-3 tabular-nums select-none" style={{ width: num }}>
                  {l.new ?? ""}
                </span>
                <span className={cx("w-[2ch] shrink-0 text-center select-none", l.kind === "add" ? "text-green" : l.kind === "del" ? "text-red" : "text-ink-3")}>
                  {l.kind === "add" ? "+" : l.kind === "del" ? "−" : ""}
                </span>
                <span className="pr-6 pl-1 whitespace-pre text-ink">{l.text || " "}</span>
              </>
            )}
          </div>
        ))}
        <div style={{ height: (lines.length - end) * LH }} />
        {truncated && <p className="sticky left-0 px-3 pt-2 font-sans text-[11.5px] text-ink-3">{t("session-files-difflines.truncated")}</p>}
      </div>
    </div>
  );
}
