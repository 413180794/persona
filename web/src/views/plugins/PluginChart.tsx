// A plugin's chart (docs/plugins.md「UI 插槽」「新条目」 chart): bars (side by side or stacked) or lines, drawn with the
// web's own colors (a tone is a role color, no tone the series palette in order). Thin marks with a rounded free end,
// a 2px surface gap between touching bars, hairline grid, short numbers on the right; labels thinned so they never
// overlap. Hovering (or the arrow keys) shows the label and every series' value. A compact chart (a card) draws no
// axis text: only the marks and the first / last label. The geometry is in lib/plugin-ui.ts.
import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { barSpans, chartExtent, compactNumber, edgeLabels, labelWidth, linear, niceTicks, pointText, seriesColors, thinLabels, type UIItem } from "../../lib/plugin-ui";
import { cx } from "../../ui/atoms";

const FONT = 10.5; // axis text, px
const LABEL_H = 18; // the labels under the plot
const TOP = 6;

function useWidth<T extends HTMLElement>(): [React.RefObject<T | null>, number] {
  const ref = useRef<T>(null);
  const [w, setW] = useState(0);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    setW(el.clientWidth);
    const ro = new ResizeObserver(([e]) => setW(Math.round(e.contentRect.width)));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, w];
}

/** A bar from its base to its end, the free end rounded (square at the base). */
function barPath(x: number, w: number, base: number, end: number, r: number, round: boolean): string {
  const h = Math.abs(end - base);
  const rr = round ? Math.min(r, w / 2, h) : 0;
  if (end <= base) {
    // grows up
    return `M${x},${base}V${end + rr}Q${x},${end} ${x + rr},${end}H${x + w - rr}Q${x + w},${end} ${x + w},${end + rr}V${base}Z`;
  }
  return `M${x},${base}V${end - rr}Q${x},${end} ${x + rr},${end}H${x + w - rr}Q${x + w},${end} ${x + w},${end - rr}V${base}Z`;
}

export function PluginChart({ item, compact }: { item: UIItem; compact?: boolean }) {
  const series = item.series ?? [];
  const kind = item.kind === "line" ? "line" : "bar";
  const stacked = kind === "bar" && !!item.stacked && series.length > 1;
  const labels = useMemo(() => (series[0]?.points ?? []).map((p) => p.label), [series]);
  const n = labels.length;
  const colors = useMemo(() => seriesColors(series), [series]);
  const [box, width] = useWidth<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);

  const H = compact ? 60 : 148;
  const [mn, mx] = useMemo(() => chartExtent(series, kind, stacked), [series, kind, stacked]);
  const axis = niceTicks(mn, mx, compact ? 2 : 4);
  // the numbers on the right take what the widest needs
  const axisW = compact ? 0 : Math.max(28, ...axis.ticks.map((v) => labelWidth(compactNumber(v), FONT) + 8));
  const plotW = Math.max(0, width - axisW);
  const y = linear(axis.lo, axis.hi, H, TOP);
  const zero = y(0);

  // where each label sits: a band's middle (bars) or a point (lines, inset so the end dots stay inside)
  const band = n ? plotW / n : 0;
  const pad = compact ? 4 : 6;
  const xs = useMemo(
    () => labels.map((_, i) => (kind === "bar" ? band * (i + 0.5) : n === 1 ? plotW / 2 : pad + (i * (plotW - 2 * pad)) / (n - 1))),
    [labels, kind, band, n, plotW, pad],
  );
  const shown = useMemo(() => (plotW > 0 ? (compact ? edgeLabels(labels, plotW, FONT) : thinLabels(labels, xs, plotW, FONT)) : []), [compact, labels, xs, plotW]);

  // bars: a group's width, each bar at most 24px (compact 16px), 2px apart
  const m = stacked ? 1 : series.length;
  const barW = kind === "bar" ? Math.max(1, Math.min(compact ? 16 : 24, (band * (compact ? 0.66 : 0.62) - 2 * (m - 1)) / m)) : 0;
  const spans = useMemo(() => (kind === "bar" ? barSpans(series, stacked) : []), [kind, series, stacked]);

  const nearest = (px: number) => {
    let best = 0;
    xs.forEach((x, i) => {
      if (Math.abs(x - px) < Math.abs(xs[best] - px)) best = i;
    });
    return best;
  };
  const move = (e: React.PointerEvent<SVGRectElement>) => {
    const r = e.currentTarget.getBoundingClientRect();
    setHover(nearest(e.clientX - r.left));
  };
  const key = (e: React.KeyboardEvent) => {
    if (!n) return;
    if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
      e.preventDefault();
      const at = hover ?? (e.key === "ArrowLeft" ? n : -1);
      setHover(Math.min(n - 1, Math.max(0, at + (e.key === "ArrowLeft" ? -1 : 1))));
    } else if (e.key === "Escape") setHover(null);
  };

  const title = item.title;
  const summary = `${title ? title + "：" : ""}${kind === "bar" ? "柱状图" : "折线图"}，${n} 个点${series.length > 1 ? `，${series.map((s) => s.name).filter(Boolean).join("、")}` : ""}`;

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
                    <span aria-hidden className="inline-block size-2 rounded-[2px]" style={{ background: colors[i] }} />
                  ) : (
                    <span aria-hidden className="inline-block h-[2px] w-3 rounded-full" style={{ background: colors[i] }} />
                  )}
                  {s.name}
                </span>
              ))}
            </span>
          )}
        </div>
      )}
      <div ref={box} className="relative">
        {width > 0 && (
          <svg
            width={width}
            height={H + LABEL_H}
            role="img"
            aria-label={summary}
            tabIndex={0}
            onKeyDown={key}
            onBlur={() => setHover(null)}
            className="block rounded-[4px] outline-none focus-visible:shadow-[0_0_0_2px_var(--accent)]"
          >
            {/* grid: hairlines at round values, short numbers on the right; a compact chart keeps only the zero line */}
            {!compact &&
              axis.ticks.map((v) => (
                <g key={v}>
                  <line x1={0} x2={plotW} y1={y(v)} y2={y(v)} stroke={v === 0 ? "var(--line-strong)" : "var(--line)"} strokeWidth={1} shapeRendering="crispEdges" />
                  <text x={plotW + 6} y={y(v)} dy="0.35em" fontSize={FONT} fill="var(--ink-3)" className="tabular-nums">
                    {compactNumber(v)}
                  </text>
                </g>
              ))}
            {compact && <line x1={0} x2={plotW} y1={zero} y2={zero} stroke="var(--line-strong)" strokeWidth={1} shapeRendering="crispEdges" />}

            {/* the hovered column, behind the marks */}
            {hover !== null && kind === "bar" && <rect x={band * hover} y={TOP - 2} width={band} height={H - TOP + 2} rx={4} fill="var(--ink)" fillOpacity={0.05} pointerEvents="none" />}

            {kind === "bar" &&
              spans.map((col, i) => (
                <g key={i}>
                  {col.map((b) => {
                    const k = stacked ? 0 : b.series;
                    const x = xs[i] - (m * barW + 2 * (m - 1)) / 2 + k * (barW + 2);
                    // 2px of surface between stacked segments: each gives 1px at the side it shares
                    let base = y(b.from);
                    let end = y(b.to);
                    if (Math.abs(end - base) < 0.5) return null;
                    const dir = end < base ? -1 : 1;
                    if (stacked && b.from !== 0) base += dir;
                    if (stacked && !b.end) end -= dir;
                    if ((end - base) * dir <= 0) return null;
                    return <path key={b.series} d={barPath(x, barW, base, end, compact ? 3 : 4, b.end)} fill={colors[b.series]} />;
                  })}
                </g>
              ))}

            {kind === "line" &&
              series.map((s, k) => {
                const pts = s.points.map((p, i) => `${xs[i].toFixed(1)},${y(p.value).toFixed(1)}`).join(" ");
                if (n === 1) return <circle key={k} cx={xs[0]} cy={y(s.points[0].value)} r={3} fill={colors[k]} />;
                return (
                  <g key={k}>
                    {series.length === 1 && <polygon points={`${xs[0].toFixed(1)},${zero} ${pts} ${xs[n - 1].toFixed(1)},${zero}`} fill={colors[k]} fillOpacity={0.1} />}
                    <polyline points={pts} fill="none" stroke={colors[k]} strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
                  </g>
                );
              })}

            {/* the crosshair and the points at the hovered label (lines) */}
            {hover !== null && kind === "line" && (
              <g pointerEvents="none">
                <line x1={xs[hover]} x2={xs[hover]} y1={TOP} y2={H} stroke="var(--ink-3)" strokeWidth={1} shapeRendering="crispEdges" />
                {series.map((s, k) => (
                  <circle key={k} cx={xs[hover]} cy={y(s.points[hover]?.value ?? 0)} r={4} fill={colors[k]} stroke="var(--surface)" strokeWidth={2} />
                ))}
              </g>
            )}

            {/* labels under the plot: thinned so none overlap; the first and last flush with the edges */}
            {shown.map((i) => {
              const w = labelWidth(labels[i], FONT);
              const anchor = xs[i] - w / 2 < 0 ? "start" : xs[i] + w / 2 > plotW ? "end" : "middle";
              const tx = anchor === "start" ? 0 : anchor === "end" ? plotW : xs[i];
              const dim = hover !== null && hover !== i;
              return (
                <text key={i} x={tx} y={H + 13} textAnchor={anchor} fontSize={FONT} fill="var(--ink-3)" opacity={dim ? 0.55 : 1} className="tabular-nums">
                  {labels[i]}
                </text>
              );
            })}

            {/* the hit area: the whole plot, the nearest label wins */}
            <rect x={0} y={0} width={plotW} height={H + LABEL_H} fill="transparent" onPointerMove={move} onPointerDown={move} onPointerLeave={() => setHover(null)} style={{ touchAction: "pan-y" }} />
          </svg>
        )}
        {width === 0 && <div style={{ height: H + LABEL_H }} />}
        {hover !== null && width > 0 && <Tip item={item} at={hover} x={xs[hover]} plotW={plotW} colors={colors} kind={kind} />}
        {/* the same numbers as a table, for screen readers (in a div: a table grows past sr-only's 1px) */}
        <div className="sr-only">
          <table>
            <thead>
              <tr>
                <th scope="col">{title || "标签"}</th>
                {series.map((s, k) => (
                  <th key={k} scope="col">
                    {s.name || item.unit || "值"}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {labels.map((l, i) => (
                <tr key={i}>
                  <th scope="row">{l}</th>
                  {series.map((s, k) => (
                    <td key={k}>{s.points[i] ? pointText(s.points[i], item.unit) : ""}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
      {item.detail && <p className={cx("mt-1 text-ink-3", compact ? "text-[11px]" : "text-[11.5px]")}>{item.detail}</p>}
    </div>
  );
}

/** The read-out at a label: the label, then each series' value (strong) and name (muted), keyed by its mark. */
function Tip({ item, at, x, plotW, colors, kind }: { item: UIItem; at: number; x: number; plotW: number; colors: string[]; kind: "bar" | "line" }) {
  const series = item.series ?? [];
  const right = x > plotW / 2;
  return (
    <div
      role="status"
      className="pointer-events-none absolute top-0 z-10 min-w-[96px] rounded-[8px] border border-tooltip-border bg-tooltip-bg px-2.5 py-1.5 text-tooltip-fg shadow-overlay"
      style={{ left: x, transform: right ? "translateX(calc(-100% - 12px))" : "translateX(12px)" }}
    >
      <div className="text-[11px] whitespace-nowrap text-tooltip-muted">{series[0]?.points[at]?.label}</div>
      {series.map((s, k) => {
        const p = s.points[at];
        if (!p) return null;
        return (
          <div key={k} className="mt-0.5 flex items-center gap-1.5 whitespace-nowrap">
            {kind === "bar" ? <span aria-hidden className="inline-block size-2 rounded-[2px]" style={{ background: colors[k] }} /> : <span aria-hidden className="inline-block h-[2px] w-2.5 rounded-full" style={{ background: colors[k] }} />}
            <span className="text-[12px] font-semibold tabular-nums">{pointText(p, item.unit)}</span>
            {s.name && <span className="text-[11px] text-tooltip-muted">{s.name}</span>}
          </div>
        );
      })}
    </div>
  );
}
