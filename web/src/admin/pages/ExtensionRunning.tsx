// How an extension runs (docs/extensions.md「运行情况」, #1857): the error rate and p95 on its row, and in its details the
// calls and errors, the latency, why it exited, and its log (search, follow it live, copy). A hub from before #1857 has
// none of it (404): the rows and details show nothing of it then.
import { useEffect, useMemo, useRef, useState } from "react";
import { ExtensionChart } from "../../views/extensions/ExtensionChart";
import { Button, cx, toast } from "../../ui/atoms";
import { AdminError, errorText, followExtensionLogs, getExtensionLogs, getExtensionMetrics } from "../api";
import { AdminIcon } from "../icons";
import { dateTime, logText, mergeLines, metricsCharts, msText, rateText, rateTone } from "../logic";
import type { ExtensionMetrics, ExtensionOverview, LogLine, MetricsWindow } from "../types";
import { Loader, Segmented, Tag } from "../parts";

const WINDOWS: { value: MetricsWindow; label: string }[] = [
  { value: "1h", label: "1 小时" },
  { value: "24h", label: "24 小时" },
  { value: "7d", label: "7 天" },
];

/** The row's two columns: the error rate and p95 over the last 24 hours. */
export function RunColumns({ o }: { o?: ExtensionOverview }) {
  if (!o) return null;
  const tone = rateTone(o.error_rate, o.calls);
  return (
    <div className="hidden shrink-0 items-center gap-4 sm:flex" title={`近 24 小时：${o.calls} 次调用，${o.errors} 次出错`}>
      <Column label="错误率" className={tone === "red" ? "text-red" : tone === "orange" ? "text-orange" : "text-ink-2"}>
        {rateText(o.error_rate, o.calls)}
      </Column>
      <Column label="p95" className="text-ink-2">
        {msText(o.p95_ms)}
      </Column>
    </div>
  );
}

function Column({ label, className, children }: { label: string; className?: string; children: React.ReactNode }) {
  return (
    <span className="flex w-[64px] flex-col items-end leading-tight">
      <span className="whitespace-nowrap text-[10.5px] text-ink-3">{label}</span>
      <span className={cx("whitespace-nowrap font-mono text-[12px] tabular-nums", className)}>{children}</span>
    </span>
  );
}

/** 运行情况 and 日志, in an extension's details. */
export function ExtensionRunning({ id }: { id: string }) {
  const [win, setWin] = useState<MetricsWindow>("24h");
  const [m, setM] = useState<ExtensionMetrics | null>(null);
  const [failed, setFailed] = useState<unknown>(null);
  const [absent, setAbsent] = useState(false);
  useEffect(() => {
    let live = true;
    const load = () =>
      getExtensionMetrics(id, win).then(
        (r) => live && (setM(r), setFailed(null)),
        (e) => {
          if (!live) return;
          if (e instanceof AdminError && e.status === 404) setAbsent(true);
          else setFailed(e);
        },
      );
    void load();
    const t = setInterval(load, 30_000);
    return () => {
      live = false;
      clearInterval(t);
    };
  }, [id, win]);
  if (absent) return null; // a hub from before #1857
  return (
    <>
      <section className="grid min-w-0 gap-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h4 className="text-[12px] font-medium text-ink-2">运行情况</h4>
          <Segmented value={win} options={WINDOWS} onChange={setWin} label="时间范围" />
        </div>
        {!m ? failed ? <p className="text-[12px] text-red">{errorText(failed)}</p> : <Loader /> : <Running m={m} />}
      </section>
      <ExtensionLogs id={id} />
    </>
  );
}

function Running({ m }: { m: ExtensionMetrics }) {
  const charts = useMemo(() => metricsCharts(m), [m]);
  const t = m.total;
  if (t.calls === 0 && m.starts === 0 && m.exits === 0) {
    return <p className="text-[12px] text-ink-3">这段时间没有调用，也没有启动过。按需启动的扩展用到时才跑。</p>;
  }
  const tone = rateTone(t.error_rate, t.calls);
  const reasons = Object.entries(m.exit_reasons ?? {}).sort((a, b) => b[1] - a[1]);
  return (
    <div className="grid min-w-0 gap-3">
      <dl className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        <Figure label="调用">{t.calls.toLocaleString()}</Figure>
        <Figure label="错误率" className={tone === "red" ? "text-red" : tone === "orange" ? "text-orange" : undefined}>
          {rateText(t.error_rate, t.calls)}
          {t.errors > 0 && <span className="ml-1 text-[11px] text-ink-3">（{t.errors} 次）</span>}
        </Figure>
        <Figure label="延迟 p95">
          {msText(t.p95_ms)}
          <span className="mt-0.5 block truncate text-[11px] text-ink-3">
            p50 {msText(t.p50_ms)} · p99 {msText(t.p99_ms)}
          </span>
        </Figure>
        <Figure label="启动 / 出错退出" className={m.exits > 0 ? "text-orange" : undefined}>
          {m.starts} / {m.exits}
        </Figure>
      </dl>
      <div className="grid min-w-0 gap-3 lg:grid-cols-2">
        <div className="min-w-0 rounded-control bg-inset p-3 shadow-hairline">
          <p className="mb-1 text-[11.5px] text-ink-3">调用和出错</p>
          <ExtensionChart item={charts.calls} />
        </div>
        {charts.latency && (
          <div className="min-w-0 rounded-control bg-inset p-3 shadow-hairline">
            <p className="mb-1 text-[11.5px] text-ink-3">延迟（p95）</p>
            <ExtensionChart item={charts.latency} />
          </div>
        )}
      </div>
      {m.methods.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[460px] text-[12px]">
            <thead>
              <tr className="text-left text-[11px] text-ink-3">
                <th className="py-1 pr-3 font-normal">方法</th>
                <th className="py-1 pr-3 text-right font-normal">调用</th>
                <th className="py-1 pr-3 text-right font-normal">错误率</th>
                <th className="py-1 pr-3 text-right font-normal">p95</th>
                <th className="py-1 font-normal">出错的码</th>
              </tr>
            </thead>
            <tbody>
              {m.methods.slice(0, 12).map((x) => (
                <tr key={x.method} className="border-t border-line">
                  <td className="max-w-[240px] truncate py-1 pr-3 font-mono text-ink-2">{x.method}</td>
                  <td className="py-1 pr-3 text-right font-mono tabular-nums text-ink-2">{x.calls}</td>
                  <td className={cx("py-1 pr-3 text-right font-mono tabular-nums", rateTone(x.error_rate, x.calls) === "gray" ? "text-ink-2" : "text-orange")}>
                    {rateText(x.error_rate, x.calls)}
                  </td>
                  <td className="py-1 pr-3 text-right font-mono tabular-nums text-ink-2">{msText(x.p95_ms)}</td>
                  <td className="py-1 font-mono text-[11px] text-ink-3">
                    {Object.entries(x.codes ?? {})
                      .map(([c, n]) => `${c} ×${n}`)
                      .join("  ")}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {reasons.length > 0 && (
        <div className="grid gap-1">
          <p className="text-[11.5px] text-ink-3">出错退出的原因</p>
          {reasons.slice(0, 5).map(([r, n]) => (
            <p key={r} className="flex items-start gap-1.5 text-[12px] leading-relaxed text-orange">
              <AdminIcon name="alert" size={13} className="mt-0.5 shrink-0" />
              <span className="min-w-0 break-words">{r}</span>
              <span className="ml-auto shrink-0 font-mono text-ink-3">×{n}</span>
            </p>
          ))}
          {m.recent_exits.length > 0 && <p className="text-[11px] text-ink-3">最近一次：{dateTime(m.recent_exits[m.recent_exits.length - 1].at)}</p>}
        </div>
      )}
    </div>
  );
}

function Figure({ label, className, children }: { label: string; className?: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0 rounded-control bg-inset px-3 py-2 shadow-hairline">
      <dt className="whitespace-nowrap text-[11px] text-ink-3">{label}</dt>
      <dd className={cx("mt-0.5 font-mono text-[15px] tabular-nums text-ink", className)}>{children}</dd>
    </div>
  );
}

/** The extension's log: the kept lines, searched on the hub; following adds each new line as it comes. */
function ExtensionLogs({ id }: { id: string }) {
  const [q, setQ] = useState("");
  const [lines, setLines] = useState<LogLine[] | null>(null);
  const [next, setNext] = useState(0);
  const [failed, setFailed] = useState<unknown>(null);
  const [following, setFollowing] = useState(false);
  const box = useRef<HTMLDivElement>(null);
  const atEnd = useRef(true);

  // The kept lines, again whenever the search changes (0.3 s after the last key).
  useEffect(() => {
    let live = true;
    const t = setTimeout(() => {
      getExtensionLogs(id, q.trim()).then(
        (r) => live && (setLines(r.lines), setNext(r.next), setFailed(null)),
        (e) => live && setFailed(e),
      );
    }, 300);
    return () => {
      live = false;
      clearTimeout(t);
    };
  }, [id, q]);

  // Following: the new lines from the stream, filtered here as the hub filters the kept ones.
  useEffect(() => {
    if (!following || lines === null) return;
    const needle = q.trim().toLowerCase();
    return followExtensionLogs(
      id,
      next - 1,
      (l) => {
        if (needle && !l.text.toLowerCase().includes(needle) && !l.source.includes(needle)) return;
        setLines((have) => mergeLines(have ?? [], [l]));
      },
      () => {
        setFollowing(false);
        toast("实时日志断开了", "error");
      },
    );
    // next and lines only matter when following starts
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [following, id, q]);

  // Stay at the end while the reader is there.
  useEffect(() => {
    const el = box.current;
    if (el && atEnd.current) el.scrollTop = el.scrollHeight;
  }, [lines]);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(logText(lines ?? []));
      toast(`已拷贝 ${lines?.length ?? 0} 行`, "ok");
    } catch {
      toast("拷贝失败", "error");
    }
  };

  return (
    <section className="grid min-w-0 gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <h4 className="mr-auto text-[12px] font-medium text-ink-2">日志</h4>
        <label className="flex h-7 min-w-[160px] items-center gap-1.5 rounded-control bg-inset px-2 shadow-hairline">
          <AdminIcon name="search" size={12} className="shrink-0 text-ink-3" />
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="搜索日志"
            aria-label="搜索日志"
            className="min-w-0 flex-1 bg-transparent text-[12px] text-ink outline-none placeholder:text-ink-3"
          />
        </label>
        <Button tone={following ? "primary" : "quiet"} className="h-7 px-2" onClick={() => setFollowing((f) => !f)} aria-pressed={following}>
          <AdminIcon name={following ? "x" : "pulse"} size={12} />
          {following ? "停止跟随" : "实时跟随"}
        </Button>
        <Button tone="quiet" className="h-7 px-2" onClick={() => void copy()} disabled={!lines?.length}>
          <AdminIcon name="copy" size={12} />
          拷贝
        </Button>
      </div>
      {failed && !lines ? (
        <p className="text-[12px] text-red">{errorText(failed)}</p>
      ) : !lines ? (
        <Loader />
      ) : (
        <div
          ref={box}
          onScroll={(e) => {
            const el = e.currentTarget;
            atEnd.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
          }}
          className="max-h-[320px] overflow-auto rounded-control bg-inset px-3 py-2 font-mono text-[11.5px] leading-[1.6] shadow-hairline"
        >
          {lines.length === 0 ? (
            <p className="py-4 text-center font-sans text-ink-3">{q.trim() ? "没有匹配的行" : "还没有日志：扩展在 stderr 上写的和 hub 关于它的话会出现在这里"}</p>
          ) : (
            lines.map((l) => (
              <div key={l.seq} className="flex gap-2 whitespace-pre-wrap break-all">
                <span className="shrink-0 select-none text-ink-3">{timeOf(l.at)}</span>
                {l.source === "hub" && (
                  <span className="shrink-0 select-none">
                    <Tag tone="accent">hub</Tag>
                  </span>
                )}
                <span className={cx("min-w-0", l.source === "hub" ? "text-ink-2" : "text-ink")}>{l.text}</span>
              </div>
            ))
          )}
        </div>
      )}
      <p className="text-[11px] text-ink-3">每个扩展留最近 2000 行，hub 重启后清空；像密钥的内容在进来前已换成 ***。</p>
    </section>
  );
}

function timeOf(iso: string): string {
  const d = new Date(iso);
  const two = (n: number) => String(n).padStart(2, "0");
  return `${two(d.getHours())}:${two(d.getMinutes())}:${two(d.getSeconds())}`;
}
