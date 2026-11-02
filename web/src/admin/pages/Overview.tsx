// 概览: the hub at a glance — its name, build, uptime, how it listens, the addresses it hands out, counts, doctor. With
// no computer but its own yet, adding one comes first (docs/plan-hub-center.md 0).
import { useState } from "react";
import { Button, cx, IconButton, toast, useNow } from "../../ui/atoms";
import { adminApi, errorText } from "../api";
import { AdminIcon } from "../icons";
import { dateTime, endpointKindLabel, groupDoctor, needsMachines, uptime } from "../logic";
import { navigate } from "../router";
import { share } from "../store";
import type { DoctorReport, Endpoint, MachinesView, Overview } from "../types";
import { Card, CopyValue, Dot, ErrorBox, Line, Loader, PageHeader, Stat, Tag, useLoad } from "../parts";

export function OverviewPage() {
  const { data, error, loading, reload, set } = useLoad(async () => {
    const o = await adminApi<Overview>("/overview");
    share({ restart: o.restart, canRestart: o.hub.can_restart, tls: o.tls, hubName: o.hub.name, startedAt: o.hub.started_at, hubId: o.hub.hub_id });
    return o;
  });
  // The counts cannot tell the hub's own machine from a computer: the list can.
  const list = useLoad(() => adminApi<MachinesView>("/machines"));
  const now = useNow(30_000);
  if (!data) return loading ? <Loader /> : <ErrorBox error={error} onRetry={reload} />;
  const o = data;
  const empty = list.data ? needsMachines(list.data.machines) : list.error ? o.machines.total === 0 : false;
  return (
    <>
      <PageHeader title="概览" subtitle="这台 hub 的状态。改动会写回 hub 机器上的 hub.json。" />
      {empty && <AddFirst />}
      <div className="mb-6 grid grid-cols-2 gap-3 sm:grid-cols-4">
        <Stat icon="pulse" label="已运行" value={uptime(o.hub.started_at, now)} hint={`自 ${dateTime(o.hub.started_at)}`} />
        <Stat icon="machine" label="电脑在线" value={`${o.machines.online}/${o.machines.total}`} hint="查看电脑" onClick={() => navigate("machines")} />
        <Stat icon="phone" label="设备" value={o.devices} hint="凭据和配对" onClick={() => navigate("devices")} />
        <Stat icon="lock" label="TLS" value={o.tls ? "已开启" : "未开启"} hint={o.tls ? `端口 ${o.tls.port || o.tls.listen}` : "去网络与访问开启"} onClick={() => navigate("network")} />
      </div>

      <Card title="hub">
        <NameLine o={o} onSaved={(name, nameSet) => set({ ...o, hub: { ...o.hub, name, name_set: nameSet } })} />
        <Line label="版本">
          <CopyValue value={o.hub.version} />
        </Line>
        <Line label="系统">
          <span className="font-mono text-[12px]">
            {o.hub.os}/{o.hub.arch}
          </span>
          {o.hub.container && <Tag>容器</Tag>}
        </Line>
        <Line label="hub ID" hint="互联、配对时用来核对是不是这台 hub">
          <CopyValue value={o.hub.hub_id} />
        </Line>
      </Card>

      <Card title="连接" footer="明文端口只该在局域网或 VPN 里用；公网只放行 TLS 端口。">
        <Line label="明文监听" hint="App 和网页在内网用的 HTTP 端口">
          <span className="font-mono text-[12px]">{o.listen}</span>
        </Line>
        <Line label="对外地址" hint="配对二维码里的地址（public_url）">
          <CopyValue value={o.public_url || "—"} />
        </Line>
        {o.tls ? (
          <>
            <Line label="TLS 监听" hint="HTTPS / WSS，公网和互联只走这个端口">
              <span className="font-mono text-[12px]">{o.tls.listen}</span>
              <Tag tone="green">已开启</Tag>
            </Line>
            <Line label="公钥指纹" hint="客户端钉扎的就是它；换了密钥要重新配对">
              <CopyValue value={o.tls.fp} className="max-w-[340px]" />
            </Line>
          </>
        ) : (
          <Line label="TLS 监听" hint="没开 TLS 时不能从公网安全访问，也不能和别的 hub 互联">
            <Tag>未开启</Tag>
            <Button className="h-7 px-2.5" onClick={() => navigate("network")}>
              去开启
            </Button>
          </Line>
        )}
      </Card>

      <Card title="候选地址" footer="hub 在本机发现或配置的地址，鉴权后下发给 App 选路；不代表公网能连通。">
        <Endpoints list={o.endpoints} />
      </Card>

      <Doctor />
    </>
  );
}

// AddFirst: nothing but the hub's own machine yet. One hub is enough: every other computer gets the node service and is
// added here (docs/plan-hub-center.md 0).
function AddFirst() {
  return (
    <section className="mb-6 flex flex-wrap items-center gap-x-4 gap-y-3 rounded-card bg-surface px-4 py-4 shadow-card" style={{ animation: "fade-in 200ms ease-out both" }}>
      <span className="flex size-10 shrink-0 items-center justify-center rounded-[12px] bg-accent-tint text-accent-ink">
        <AdminIcon name="machine" size={19} />
      </span>
      <div className="min-w-[220px] flex-1">
        <div className="text-[14px] font-semibold text-ink">添加你的电脑</div>
        <p className="mt-0.5 text-[12.5px] leading-relaxed text-ink-2">
          这台 hub 还没有连别的电脑。全家只要这一台 hub；其他电脑都装 node 服务，再在这里添加：笔记本选「电脑来连」，服务器、example-machine 选「hub 来连」。
        </p>
      </div>
      <Button tone="primary" icon="plus" onClick={() => navigate("machines")}>
        添加电脑
      </Button>
    </section>
  );
}

function NameLine({ o, onSaved }: { o: Overview; onSaved: (name: string, nameSet: boolean) => void }) {
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState(o.hub.name_set ? o.hub.name : "");
  const [busy, setBusy] = useState(false);
  const save = async (e?: React.FormEvent) => {
    e?.preventDefault();
    setBusy(true);
    try {
      const r = await adminApi<{ name: string; name_set: boolean }>("/hub", "PUT", { name: value.trim() });
      onSaved(r.name, r.name_set);
      share({ hubName: r.name });
      setEditing(false);
      toast("已保存", "ok");
    } catch (err) {
      toast(errorText(err), "error");
    } finally {
      setBusy(false);
    }
  };
  if (editing)
    return (
      <form onSubmit={save} className="flex flex-wrap items-center gap-2 px-4 py-2.5">
        <span className="min-w-[120px] flex-1 text-[13px] text-ink">名字</span>
        <input
          autoFocus
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder={o.hub.default_name}
          aria-label="hub 名字"
          maxLength={64}
          className="h-8 w-[220px] max-w-full rounded-control bg-inset px-2.5 text-[13px] text-ink shadow-hairline outline-none placeholder:text-ink-3 focus:shadow-[0_0_0_1px_var(--line-strong),0_0_0_3px_var(--accent-tint)]"
        />
        <Button tone="quiet" className="h-8" onClick={() => setEditing(false)}>
          取消
        </Button>
        <Button tone="primary" type="submit" className="h-8" busy={busy}>
          保存
        </Button>
      </form>
    );
  return (
    <Line label="名字" hint={o.hub.name_set ? "App 和别的 hub 看到的名字" : `没起名字，用默认的「${o.hub.default_name}」`}>
      <span className="text-[13px] font-medium text-ink">{o.hub.name}</span>
      <IconButton icon="write" label="改名字" size={26} iconSize={13} onClick={() => (setValue(o.hub.name_set ? o.hub.name : ""), setEditing(true))} />
    </Line>
  );
}

export function Endpoints({ list }: { list: Endpoint[] }) {
  if (!list.length) return <div className="px-4 py-3 text-[12.5px] text-ink-3">没有候选地址。检查 public.advertise 和网卡地址。</div>;
  return (
    <>
      {list.map((e) => (
        <div key={e.url} className="flex items-center gap-3 px-4 py-2">
          <Tag tone={e.scheme === "https" ? "green" : "gray"}>{e.scheme === "https" ? "HTTPS" : "HTTP"}</Tag>
          <CopyValue value={e.url} className="min-w-0 flex-1" />
          <span className="hidden shrink-0 text-[11.5px] text-ink-3 sm:inline">{endpointKindLabel(e.kind)}</span>
        </div>
      ))}
    </>
  );
}

function Doctor() {
  const [report, setReport] = useState<DoctorReport | null>(null);
  const [busy, setBusy] = useState(false);
  const run = async () => {
    setBusy(true);
    try {
      setReport(await adminApi<DoctorReport>("/doctor", "POST"));
    } catch (e) {
      toast(errorText(e), "error");
    } finally {
      setBusy(false);
    }
  };
  const g = report ? groupDoctor(report.items) : null;
  return (
    <Card
      title="检查"
      right={
        <Button className="h-7 px-2.5" busy={busy} onClick={run}>
          {!busy && <AdminIcon name="radar" size={13} />}
          {report ? "再查一次" : "运行检查"}
        </Button>
      }
      footer={report ? `${dateTime(report.ran_at)} 检查。只读，不改配置；不测公网能不能连进来。` : "和在 hub 机器上运行 jarvisd hub doctor 一样：配置、TLS 证书、候选地址、IPv6、防火墙。只读。"}
    >
      {!g ? (
        <div className="px-4 py-3 text-[12.5px] text-ink-3">还没检查。</div>
      ) : (
        <>
          {[...g.problem, ...g.ok, ...g.info].map((it, i) => (
            <div key={i} className="flex items-start gap-3 px-4 py-2.5">
              <span className="mt-1.5">
                <Dot tone={it.level === "problem" ? "red" : it.level === "ok" ? "green" : "gray"} />
              </span>
              <div className="min-w-0 flex-1">
                <div className={cx("text-[13px]", it.level === "problem" ? "font-medium text-ink" : "text-ink")}>{it.item}</div>
                <div className="mt-0.5 text-[12px] leading-relaxed break-words text-ink-2">{it.detail}</div>
              </div>
            </div>
          ))}
          {report!.next.length > 0 && (
            <div className="bg-inset px-4 py-3">
              <div className="mb-1.5 text-[12px] font-medium text-ink-2">下一步</div>
              <ol className="list-decimal space-y-1 pl-4 text-[12px] leading-relaxed text-ink-2 marker:text-ink-3">
                {report!.next.map((s, i) => (
                  <li key={i} className="break-words">
                    {s}
                  </li>
                ))}
              </ol>
            </div>
          )}
        </>
      )}
    </Card>
  );
}
