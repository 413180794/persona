// What an extension chart draws around its plot: the header (title, unit, the series legend) and the detail line. It is
// apart from ExtensionChart so ViewBody can draw the same frame, with an empty plot of the same height, while the chart's
// chunk loads (lib/lazy-page.ts): the item is as tall before the chunk arrives as after, and nothing below it moves.
import type { ReactNode } from "react";
import { seriesColors, type UIItem } from "../../lib/extension-ui";
import { cx } from "../../ui/atoms";

/** The labels under the plot, px. */
export const LABEL_H = 18;

/** The plot's height without its labels, px. */
export function plotHeight(compact?: boolean): number {
  return compact ? 60 : 148;
}

/** The chart's mark kind: a line chart, or bars (the default). */
export function chartKind(item: UIItem): "bar" | "line" {
  return item.kind === "line" ? "line" : "bar";
}

export function ChartFrame({ item, compact, colors, children }: { item: UIItem; compact?: boolean; colors?: string[]; children: ReactNode }) {
  const series = item.series ?? [];
  const kind = chartKind(item);
  const title = item.title;
  const swatches = colors ?? seriesColors(series);
  return (
    <div className={cx(compact ? "px-4 py-2" : "px-4 pt-3 pb-2.5")}>
      {(title || series.length > 1 || (item.unit && !compact)) && (
        <div className="mb-1.5 flex flex-wrap items-baseline gap-x-3 gap-y-1">
          {title && <h4 className={cx("min-w-0 truncate font-medium text-ink-2", compact ? "text-[11.5px]" : "text-[12.5px]")}>{title}</h4>}
          {/* the axis' numbers are in the unit: say it once */}
          {item.unit && !compact && <span className="-ml-1.5 shrink-0 truncate text-[11px] text-ink-3">{title ? `· ${item.unit}` : item.unit}</span>}
          <span className="flex-1" />
          {series.length > 1 && (
            <span className="flex flex-wrap items-center gap-x-3 gap-y-0.5">
              {series.map((s, i) => (
                <span key={i} className="flex items-center gap-1.5 whitespace-nowrap text-[11px] text-ink-2">
                  {kind === "bar" ? (
                    <span aria-hidden className="inline-block size-2 rounded-[2px]" style={{ background: swatches[i] }} />
                  ) : (
                    <span aria-hidden className="inline-block h-[2px] w-3 rounded-full" style={{ background: swatches[i] }} />
                  )}
                  {s.name}
                </span>
              ))}
            </span>
          )}
        </div>
      )}
      {children}
      {item.detail && <p className={cx("mt-1 text-ink-3", compact ? "text-[11px]" : "text-[11.5px]")}>{item.detail}</p>}
    </div>
  );
}
