// Pieces the two step-by-step sign-ins share (设置 › 连接器's login card in HubConnector.tsx, 设置 › 扩展 › 渠道's
// binding in Channels.tsx; docs/connectors.md「登录」): the QR code drawn from the hub's modules, saving it as a picture,
// asking a step again every interval until it is scanned / entered, and the note for a step this page cannot draw.
import { useEffect } from "react";
import type { ConnectorPrompt } from "../../lib/connectors";
import { Button, IconButton, Spinner, toast } from "../../ui/atoms";

/** A QR code from the hub's modules (rows of "0" / "1"), in the page's own colors, with its quiet zone. */
export function QRCode({ rows, size = 184, label = "二维码" }: { rows: string[]; size?: number; label?: string }) {
  const n = rows.length;
  const quiet = 2;
  const cells: string[] = [];
  rows.forEach((row, y) => {
    for (let x = 0; x < row.length; x++) if (row[x] === "1") cells.push(`M${x + quiet} ${y + quiet}h1v1h-1z`);
  });
  return (
    <svg role="img" aria-label={label} width={size} height={size} viewBox={`0 0 ${n + quiet * 2} ${n + quiet * 2}`} shapeRendering="crispEdges" className="rounded-[10px] bg-white">
      <path d={cells.join("")} fill="#000" />
    </svg>
  );
}

export function saveQR(rows: string[], name: string) {
  const n = rows.length + 4;
  const scale = 12;
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = n * scale;
  const g = canvas.getContext("2d");
  if (!g) return;
  g.fillStyle = "#fff";
  g.fillRect(0, 0, canvas.width, canvas.height);
  g.fillStyle = "#000";
  rows.forEach((row, y) => {
    for (let x = 0; x < row.length; x++) if (row[x] === "1") g.fillRect((x + 2) * scale, (y + 2) * scale, scale, scale);
  });
  const a = document.createElement("a");
  a.href = canvas.toDataURL("image/png");
  a.download = `${name}-二维码.png`;
  a.click();
}

/**
 * Asks `polled` (a step the page sends again by itself, or null) again every `interval` seconds the hub gave
 * (`fallback` when it gave none) until `send` says it is done or the step changes. `send` resolves true when done.
 */
export function useStepPolling(polled: ConnectorPrompt | null, send: (step: string) => Promise<boolean>, fallback: number) {
  useEffect(() => {
    if (!polled) return;
    let stop = false;
    let t: ReturnType<typeof setTimeout>;
    const tick = () => {
      t = setTimeout(async () => {
        if (stop) return;
        if (!(await send(polled.step ?? "")) && !stop) tick();
      }, Math.max(polled.interval ?? fallback, 1) * 1000);
    };
    tick();
    return () => {
      stop = true;
      clearTimeout(t);
    };
  }, [polled?.step, polled?.user_code, polled?.url]); // eslint-disable-line react-hooks/exhaustive-deps
}

/** A QR code to scan in `app`, asked again by the page; 保存二维码 for scanning it from the same phone's photos. */
export function QRStep({ rows, app, label, onCancel }: { rows: string[]; app: string; label?: string; onCancel: () => void }) {
  return (
    <div className="flex flex-col items-center gap-3 px-4 py-4">
      <QRCode rows={rows} label={label} />
      <p className="flex items-center gap-2 text-[12.5px] text-ink-3"><Spinner size={11} />等你在{app}里扫码确认…</p>
      <div className="flex items-center gap-2">
        <Button tone="secondary" icon="download" onClick={() => saveQR(rows, app)}>保存二维码</Button>
        <Button tone="quiet" onClick={onCancel}>取消</Button>
      </div>
    </div>
  );
}

/** A QR step whose modules did not come with it: the link it encodes, to open (or send to yourself) in `app`. */
export function QRLinkStep({ url, app, onCancel }: { url: string; app: string; onCancel: () => void }) {
  return (
    <div className="flex flex-col gap-3 px-4 py-3">
      <p className="text-[12.5px] leading-relaxed text-ink-2">这里画不出二维码。把下面的链接发到{app}里打开，确认后这里会自动继续。</p>
      <div className="flex items-center gap-2">
        <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-ink select-all">{url}</span>
        <IconButton
          icon="copy"
          label="复制链接"
          onClick={() => void navigator.clipboard?.writeText(url).then(() => toast("已复制链接", "ok"), () => toast("没复制上", "error"))}
        />
      </div>
      <p className="flex items-center gap-2 text-[12.5px] text-ink-3"><Spinner size={11} />等你在{app}里确认…</p>
      <div>
        <Button tone="quiet" onClick={onCancel} className="-ml-2">取消</Button>
      </div>
    </div>
  );
}

/**
 * A step this page cannot draw (a kind it does not know, a QR code without its modules or link, a form with no
 * fields): says so and what to do next, with a way to start over — never a 下一步 that can never light up.
 */
export function UnsupportedStep({ elsewhere, onRestart, onCancel }: { elsewhere?: string; onRestart: () => void; onCancel: () => void }) {
  return (
    <div className="flex flex-col gap-2.5 px-4 py-3">
      <p role="alert" className="text-[12.5px] leading-relaxed text-ink-2">
        这一步当前页面画不了。刷新页面用上最新的网页版后点「重新开始」再试{elsewhere ? `；${elsewhere}` : "。"}
      </p>
      <div className="flex items-center gap-2">
        <Button tone="secondary" icon="retry" onClick={onRestart}>重新开始</Button>
        <Button tone="quiet" onClick={onCancel}>取消</Button>
      </div>
    </div>
  );
}
