// 操控电脑 on a Mac's page while the hub's extension cua is on (docs/computer-use.md, iOS ComputerSection): the switch that
// lets the assistant operate it through Cua Driver, and whether it can now. What is missing comes with what to do:
// 安装 Cua Driver (after a confirmation) when it is not there, 去授权 when a permission is missing (macOS asks on the
// Mac's own screen), the node's progress while it works, and VNC (the web's live screen, /screen/<mid>) when
// Screen Sharing is on. The hub is asked again when the page opens, and every 2 s while a job runs.
import { useEffect, useState } from "react";
import { api, enc, errorText } from "../../lib/api";
import { computerView, computerWatching, GRANT_HINT, NO_SCREEN_SHARING, screenPath, type ComputerAction, type MachineComputer } from "../../lib/computer";
import { Button, cx, Dialog, EASE, Spinner, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Switch } from "../settings/SettingsParts";

export function ComputerSection({ mid, computer, online }: { mid: string; computer: MachineComputer; online: boolean }) {
  // What the hub said since the page opened (newer than the machine list's).
  const [fresh, setFresh] = useState<MachineComputer | null>(null);
  const [saving, setSaving] = useState(false);
  const [asking, setAsking] = useState(false);
  const [confirmInstall, setConfirmInstall] = useState(false);
  const shown = fresh ?? computer;
  const enabled = shown.enabled;
  const watching = computerWatching(shown, online);

  useEffect(() => setFresh(null), [mid]);

  const refresh = async () => {
    try {
      setFresh(await api<MachineComputer>(`/machines/${enc(mid)}/computer`));
    } catch {
      // the machine list's copy stays
    }
  };

  useEffect(() => {
    if (online && enabled) void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mid, online, enabled]);

  useEffect(() => {
    if (!watching) return;
    const t = setInterval(() => void refresh(), 2000);
    return () => clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mid, watching]);

  const set = async (on: boolean) => {
    setSaving(true);
    try {
      setFresh(await api<MachineComputer>(`/machines/${enc(mid)}/computer`, { method: "PUT", json: { enabled: on } }));
    } catch (e) {
      toast(`${on ? "打开" : "关掉"}失败：${errorText(e)}`, "error");
    } finally {
      setSaving(false);
    }
  };

  const run = async (what: ComputerAction) => {
    setAsking(true);
    try {
      setFresh(await api<MachineComputer>(`/machines/${enc(mid)}/computer/${what}`, { method: "POST" }));
    } catch (e) {
      toast(errorText(e), "error");
    } finally {
      setAsking(false);
    }
  };

  const v = computerView(shown, online);
  const look = (prominent: boolean) => (
    <Button tone={prominent ? "primary" : "secondary"} icon="monitor" onClick={() => window.open(screenPath(mid), "_blank", "noopener")}>
      VNC
    </Button>
  );
  const action = (title: string, what: ComputerAction, prominent = false) => (
    <Button tone={prominent ? "primary" : "secondary"} busy={asking} onClick={() => (what === "install" ? setConfirmInstall(true) : void run(what))}>
      {title}
    </Button>
  );

  return (
    <section data-testid="computer-section">
      <div className="mb-2.5 flex min-h-7 items-center gap-2 px-1">
        <h2 className="text-[13px] font-medium text-ink">操控电脑</h2>
      </div>
      <div className="overflow-hidden rounded-card bg-surface shadow-card" style={{ animation: `fade-up 420ms ${EASE} both` }}>
        <div className="flex items-center gap-3 px-4 py-3">
          <div className="min-w-0 flex-1">
            <div className="truncate text-[13px] text-ink">允许助手操控这台电脑</div>
            <div className="mt-0.5 truncate text-[11.5px] text-ink-3">经 Cua Driver 看屏幕、点按、输入、截图</div>
          </div>
          {saving && <Spinner />}
          <Switch on={enabled} onChange={(on) => void set(on)} disabled={saving || !online} label="允许助手操控这台电脑" />
        </div>
        {v.kind !== "off" && (
          <div className="flex flex-col items-start gap-2.5 border-t border-line px-4 py-3">
            {v.kind === "offline" && <Line text="机器离线，连上后才能操控" />}
            {v.kind === "running" && (
              <>
                <p className="flex items-center gap-2 text-[12.5px] text-ink-2" role="status">
                  <Spinner />
                  {v.text}
                </p>
                {(v.cancel || v.screen) && (
                  <div className="flex flex-wrap gap-2">
                    {v.cancel && action("取消", "cancel")}
                    {v.screen && look(true)}
                  </div>
                )}
              </>
            )}
            {v.kind === "install" && (
              <>
                <Line text={v.text} />
                <p className="text-[11.5px] leading-relaxed text-ink-3">从 Cua 官网下载安装器，校验后装进「应用程序」，并关掉它的使用统计。</p>
                {action(v.retry ? "重新安装" : "安装 Cua Driver", "install", true)}
              </>
            )}
            {v.kind === "granted" && (
              <>
                <Line text={GRANT_HINT} />
                <div className="flex flex-wrap gap-2">
                  {v.screen && look(true)}
                  {action("再请求一次", "grant")}
                </div>
                {!v.screen && <p className="text-[11.5px] leading-relaxed text-ink-3">{NO_SCREEN_SHARING}</p>}
              </>
            )}
            {v.kind === "grant" && (
              <>
                <Line text={v.text} />
                {action("去授权", "grant", true)}
              </>
            )}
            {v.kind === "ready" && (
              <>
                <Line text={v.text} ok />
                {v.screen ? look(false) : <p className="text-[11.5px] leading-relaxed text-ink-3">{NO_SCREEN_SHARING}</p>}
              </>
            )}
            {v.kind === "unknown" && <Line text={v.text} />}
          </div>
        )}
      </div>
      <Dialog
        open={confirmInstall}
        onClose={() => setConfirmInstall(false)}
        title="安装 Cua Driver"
        footer={
          <>
            <Button tone="quiet" onClick={() => setConfirmInstall(false)}>
              取消
            </Button>
            <Button
              tone="primary"
              onClick={() => {
                setConfirmInstall(false);
                void run("install");
              }}
            >
              安装
            </Button>
          </>
        }
      >
        <p className="text-[12.5px] leading-relaxed text-ink-2">在这台 Mac 上从 Cua 官网下载安装器，校验后装进「应用程序」，并关掉它的使用统计。装好后会请求辅助功能和屏幕录制权限，要在那台 Mac 的屏幕上点「允许」。</p>
      </Dialog>
    </section>
  );
}

function Line({ text, ok }: { text: string; ok?: boolean }) {
  return (
    <p className="flex items-start gap-2 text-[12.5px]" role="status">
      <span className={cx("mt-[3px] flex size-3.5 shrink-0 items-center justify-center rounded-full", ok ? "bg-green-tint text-green" : "bg-orange-tint text-orange")}>
        <Icon name={ok ? "check" : "info"} size={9} stroke={2.8} />
      </span>
      <span className={cx("min-w-0 break-words", ok ? "text-ink" : "text-ink-2")}>{text}</span>
    </p>
  );
}
