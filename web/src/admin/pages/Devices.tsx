// 设备: every device credential the hub issued (phones, watches, browsers, jarvis-links), revoking one, and a pairing
// QR code for a new phone. On a member hub the center issues and revokes them all (409 member_hub): this page says so.
import { useEffect, useState } from "react";
import { ago } from "../../lib/format";
import { Button, Dialog, toast, useNow } from "../../ui/atoms";
import { adminApi, AdminError, errorText } from "../api";
import { AdminIcon, type AdminIconName } from "../icons";
import { activeDevices, centerName, countdown, dateTime, deviceKindLabel, roleLabel } from "../logic";
import type { Device, PhonePair } from "../types";
import { devDialog } from "../router";
import { useShared } from "../store";
import { Card, ConfirmButton, CopyValue, EmptyState, ErrorBox, Loader, PageHeader, Qr, Tag, useLoad } from "../parts";

const KIND_ICON: Record<string, AdminIconName> = { phone: "phone", watch: "pulse", link: "link", other: "web" };

export function Devices() {
  const { data, error, loading, reload, set } = useLoad(async () => activeDevices((await adminApi<{ devices: Device[] }>("/devices")).devices));
  const [pairing, setPairing] = useState(() => devDialog() === "pair");
  const { mesh } = useShared();
  const refused = error instanceof AdminError && error.code === "member_hub";
  const member = refused || mesh?.role === "member";
  return (
    <>
      <PageHeader
        title="设备"
        subtitle="hub 发出去的每个凭据：手机、手表、浏览器、电脑上的 jarvis-link。丢了哪个就撤销哪个，别的不受影响。"
        right={
          !member && (
            <Button tone="primary" onClick={() => setPairing(true)}>
              <AdminIcon name="qr" size={13} />
              配对手机
            </Button>
          )
        }
      />
      {member && (refused || !data) ? (
        <Card>
          <EmptyState icon="mesh" title={mesh?.role === "member" ? `设备由中心「${centerName(mesh)}」管理` : "设备由中心 hub 管理"}>
            这台 hub 是组里的成员：手机、手表、浏览器的凭据都由中心签发和撤销，已经配对在这台上的设备不用重新配对。配对新手机、撤销设备，请去中心的管理界面。
          </EmptyState>
        </Card>
      ) : !data ? (
        loading ? <Loader /> : <ErrorBox error={error} onRetry={reload} />
      ) : (
        <Card title={`${data.length} 个凭据`} footer="撤销后那台设备马上断开，要再用得重新配对。主 token（hub.json 的 token）不在这里，换它要改 hub.json。">
          {data.length === 0 ? (
            <EmptyState icon="phone" title="还没有配对过设备" action={<Button onClick={() => setPairing(true)}>配对手机</Button>}>
              用 Jarvis App 扫码配对，这里就会出现它的凭据。
            </EmptyState>
          ) : (
            data.map((d) => <DeviceRow key={d.id} d={d} onRevoked={() => set(data.filter((x) => x.id !== d.id))} />)
          )}
        </Card>
      )}
      <PairPhone open={pairing} onClose={() => (setPairing(false), reload())} />
    </>
  );
}

function DeviceRow({ d, onRevoked }: { d: Device; onRevoked: () => void }) {
  const [busy, setBusy] = useState(false);
  const revoke = async () => {
    setBusy(true);
    try {
      await adminApi(`/devices/${encodeURIComponent(d.id)}`, "DELETE");
      toast(`已撤销「${d.name}」`, "ok");
      onRevoked();
    } catch (e) {
      toast(errorText(e), "error");
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3">
      <span className="flex size-9 shrink-0 items-center justify-center rounded-[10px] bg-inset text-ink-2 shadow-hairline">
        <AdminIcon name={KIND_ICON[d.kind] ?? "key"} size={16} />
      </span>
      <div className="min-w-[180px] flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-[13.5px] font-medium text-ink">{d.name}</span>
          <Tag tone={d.role === "full" ? "accent" : "gray"}>{roleLabel(d.role)}</Tag>
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11.5px] text-ink-3">
          <span className="whitespace-nowrap">{deviceKindLabel(d.kind)}</span>
          {d.machine_id && (
            <>
              <span>·</span>
              <span className="whitespace-nowrap">
                绑定 <span className="font-mono text-ink-2">{d.machine_id}</span>
              </span>
            </>
          )}
          <span>·</span>
          <span className="whitespace-nowrap">{d.last_seen ? `${ago(d.last_seen)}用过` : "还没用过"}</span>
          {d.created_at && (
            <>
              <span className="hidden sm:inline">·</span>
              <span className="hidden whitespace-nowrap sm:inline">{dateTime(d.created_at)} 配对</span>
            </>
          )}
        </div>
      </div>
      <ConfirmButton label="撤销" confirm="撤销" busy={busy} onConfirm={revoke} />
    </div>
  );
}

function PairPhone({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [pair, setPair] = useState<PhonePair | null>(null);
  const [error, setError] = useState("");
  const [round, setRound] = useState(0);
  const now = useNow(1000, Boolean(pair));
  useEffect(() => {
    if (!open) return;
    let live = true;
    setPair(null);
    setError("");
    adminApi<PhonePair>("/devices/pair", "POST").then(
      (p) => live && setPair(p),
      (e) => live && setError(errorText(e)),
    );
    return () => {
      live = false;
    };
  }, [open, round]);
  const left = pair ? countdown(pair.expires_at, now) : "";
  return (
    <Dialog open={open} onClose={onClose} title="配对手机" width={460}>
      {!pair && !error && <div className="py-10 text-center text-[12.5px] text-ink-3">正在生成…</div>}
      {error && <p className="py-3 text-[12.5px] text-red">{error}</p>}
      {pair && (
        <div className="flex flex-col items-center">
          <div className="relative">
            <Qr rows={pair.qr.rows} label="配对二维码" size={208} />
            {!left && (
              <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 rounded-[10px] bg-surface/90 backdrop-blur-[2px]">
                <span className="text-[12.5px] text-ink-2">二维码过期了</span>
                <Button icon="retry" onClick={() => setRound((r) => r + 1)}>
                  重新生成
                </Button>
              </div>
            )}
          </div>
          <p className="mt-3 text-center text-[12.5px] leading-relaxed text-ink-2">
            用 Jarvis App 扫码。只能配对一台，{left ? <>还有 <span className="font-mono text-ink">{left}</span> 过期。</> : "已过期。"}
          </p>
          <div className="mt-4 w-full divide-y divide-line overflow-hidden rounded-control bg-inset shadow-hairline">
            <div className="flex items-center gap-3 px-3 py-2">
              <span className="w-14 shrink-0 text-[11.5px] text-ink-3">地址</span>
              <CopyValue value={pair.url} className="min-w-0 flex-1" />
            </div>
            {pair.fp && (
              <div className="flex items-center gap-3 px-3 py-2">
                <span className="w-14 shrink-0 text-[11.5px] text-ink-3">指纹</span>
                <CopyValue value={pair.fp} className="min-w-0 flex-1" />
              </div>
            )}
            <div className="flex items-center gap-3 px-3 py-2">
              <span className="w-14 shrink-0 text-[11.5px] text-ink-3">链接</span>
              <CopyValue value={pair.pair_url} label="拷贝配对链接（Mac 上粘贴用）" className="min-w-0 flex-1" />
            </div>
          </div>
          <p className="mt-3 text-center text-[11.5px] leading-relaxed text-ink-3">二维码和链接都相当于凭据，别截图发到群里。</p>
        </div>
      )}
    </Dialog>
  );
}
