// 互联 (under「高级」): other hubs this one links with (docs/plan-hub-mesh.md 2.7) and which of the group is the center
// (docs/plan-hub-center.md 2). Most people never need it: one hub, the node service on every other computer. A hub
// without the API answers 501 not_implemented and this page says so.
import { useEffect, useState } from "react";
import { ago } from "../../lib/format";
import { Button, cx, Dialog, toast, useNow } from "../../ui/atoms";
import { adminApi, AdminError, errorText, setCenter } from "../api";
import { AdminIcon } from "../icons";
import { centerName, countdown, peerStatus, probeLinkable, sortMeshHubs } from "../logic";
import { devDialog, navigate } from "../router";
import { refreshMesh, share, useShared } from "../store";
import type { DiscoveredHub, MeshHub, MeshView, Peer, PeerCode, PeerProbe, PeersView } from "../types";
import { Card, ConfirmButton, CopyValue, Dot, EmptyState, ErrorBox, Field, Loader, Notice, PageHeader, Tag, useLoad } from "../parts";

export function Peers() {
  const { tls, mesh } = useShared();
  const { data, error, loading, reload } = useLoad(() => adminApi<PeersView>("/peers"));
  const [making, setMaking] = useState(false);
  // adding: true for an address typed in, a discovered hub to go to its first address
  const [adding, setAdding] = useState<DiscoveredHub | true | null>(null);
  const missing = error instanceof AdminError && (error.status === 501 || error.code === "not_implemented");

  // Dev only (screenshots): ?dialog=addpeer / addpeer-notls opens the add flow on the first / second discovered hub.
  const [devOpened, setDevOpened] = useState(false);
  useEffect(() => {
    const d = devDialog();
    if (!data || devOpened || !d.startsWith("addpeer")) return;
    setDevOpened(true);
    setAdding(data.discovered[d === "addpeer-notls" ? 1 : 0] ?? true);
  }, [data, devOpened]);

  useEffect(() => {
    if (!data || missing) return;
    const t = setInterval(() => (reload(), void refreshMesh()), 10_000); // link state, RTT and who is online move on their own
    return () => clearInterval(t);
  }, [data, missing, reload]);

  return (
    <>
      <PageHeader
        title="互联"
        subtitle="和别的 hub 连成一组：组里只有一台中心，App 连哪台都转到它。互联要双方确认，发现到的 hub 不会自动连。"
        right={
          !missing && (
            <>
              <Button onClick={() => setMaking(true)} disabled={tls === null}>
                <AdminIcon name="key" size={13} />
                生成互联码
              </Button>
              <Button tone="primary" onClick={() => setAdding(true)} disabled={tls === null}>
                <AdminIcon name="plus" size={13} stroke={2.2} />
                添加 hub
              </Button>
            </>
          )
        }
      />
      <Notice
        tone="gray"
        icon="info"
        title="一般用不到"
        action={
          <Button icon="plus" onClick={() => navigate("machines")}>
            添加电脑
          </Button>
        }
      >
        只有两处各有一台常开、互相连不通的 hub，或者已经装了好几台 hub 想合并时才需要互联。推荐只装一台 hub，其他电脑都装 node 服务，在「电脑」里添加。
      </Notice>
      {tls === null && !missing && (
        <Notice tone="orange" icon="lock" title="互联只走 TLS" action={<Button onClick={() => navigate("network")}>去开启</Button>}>
          这台 hub 还没开 TLS 端口。hub 之间的链路按公钥指纹钉扎，内网也不例外。
        </Notice>
      )}
      {missing ? (
        <Card>
          <EmptyState icon="mesh" title="这个版本的 hub 还没有互联功能">
            升级 hub 之后，这里能看到局域网里发现的 hub、生成和输入互联码、查看链路状态。
          </EmptyState>
        </Card>
      ) : !data ? (
        loading ? <Loader /> : <ErrorBox error={error} onRetry={reload} />
      ) : (
        <>
          {mesh && mesh.hubs.length > 1 && <Group mesh={mesh} onChanged={reload} />}
          <Card title="已互联" footer="断开会撤销双方的互联凭据，链路立刻断开；以后要再互联得重新交换互联码。">
            {data.peers.length === 0 ? (
              <EmptyState icon="mesh" title="还没有和别的 hub 互联">
                在另一台 hub 的管理界面「生成互联码」，再在这里「添加 hub」：先核对对方，再贴互联码。
              </EmptyState>
            ) : (
              data.peers.map((p) => <PeerRow key={p.hub_id} p={p} onGone={reload} />)
            )}
          </Card>
          <Card title="发现的 hub" footer="局域网里用 mDNS 广播自己的 hub。Docker 桥接网络里收不到广播，只能「添加 hub」手动填地址。">
            {data.discovered.length === 0 ? (
              <div className="flex items-center gap-2.5 px-4 py-3 text-[12.5px] text-ink-3">
                <AdminIcon name="radar" size={14} />
                局域网里没发现别的 hub。
              </div>
            ) : (
              data.discovered.map((d) => (
                <div key={d.hub_id} className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3">
                  <span className="flex size-9 shrink-0 items-center justify-center rounded-[10px] bg-inset text-ink-2 shadow-hairline">
                    <AdminIcon name="radar" size={16} />
                  </span>
                  <div className="min-w-[180px] flex-1">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-[13.5px] font-medium text-ink">{d.name}</span>
                      {d.version && <span className="font-mono text-[11px] text-ink-3">{d.version}</span>}
                    </div>
                    <div className="mt-1 truncate font-mono text-[11.5px] text-ink-3" title={d.addresses.join("\n")}>
                      {d.addresses.join(" · ")}
                    </div>
                    <div className="mt-0.5 truncate font-mono text-[11px] text-ink-3" title={d.fp}>
                      指纹 {d.fp}
                    </div>
                  </div>
                  <Button className="h-7 px-2.5" onClick={() => setAdding(d)}>
                    互联…
                  </Button>
                </div>
              ))
            )}
          </Card>
        </>
      )}
      <MakeCode open={making} onClose={() => setMaking(false)} />
      <AddPeer target={adding} onClose={() => setAdding(null)} onJoined={reload} />
    </>
  );
}

function PeerRow({ p, onGone }: { p: Peer; onGone: () => void }) {
  const st = peerStatus(p);
  const [busy, setBusy] = useState(false);
  const drop = async () => {
    setBusy(true);
    try {
      await adminApi(`/peers/${encodeURIComponent(p.hub_id)}`, "DELETE");
      toast("已断开", "ok");
      onGone();
    } catch (e) {
      toast(errorText(e), "error");
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3">
      <span className={cx("flex size-9 shrink-0 items-center justify-center rounded-[10px] shadow-hairline", p.state === "connected" ? "bg-green-tint text-green" : "bg-inset text-ink-2")}>
        <AdminIcon name="mesh" size={16} />
      </span>
      <div className="min-w-[180px] flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-[13.5px] font-medium text-ink">{p.name}</span>
          <span className="truncate font-mono text-[11px] text-ink-3" title={p.hub_id}>
            {p.hub_id.slice(0, 8)}
          </span>
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11.5px] text-ink-3">
          <span className="flex items-center gap-1.5 whitespace-nowrap">
            <Dot tone={st.tone} pulse={p.state === "connecting"} />
            <span className={st.tone === "green" ? "text-green" : st.tone === "orange" ? "text-orange" : "text-ink-2"}>{st.label}</span>
          </span>
          {p.role && <Tag tone={p.role === "center" ? "accent" : "gray"}>{p.role === "center" ? "中心" : "成员"}</Tag>}
          {p.rtt_ms !== undefined && p.state === "connected" && <Tag>{p.rtt_ms} ms</Tag>}
          {p.address && <span className="truncate font-mono">{p.address}</span>}
          <span className="whitespace-nowrap">{p.machines} 台电脑</span>
          {p.since && <span className="whitespace-nowrap">{p.state === "connected" ? `连上于 ${ago(p.since)}` : `${ago(p.since)}断开`}</span>}
        </div>
        {st.error && <div className="mt-1 text-[11.5px] break-words text-red">{st.error}</div>}
      </div>
      <ConfirmButton label="断开" confirm="断开" icon="unlink" busy={busy} onConfirm={drop} />
    </div>
  );
}

// Group: every hub of the group, which one is the center, and「设为中心」for the others (this hub included).
function Group({ mesh, onChanged }: { mesh: MeshView; onChanged: () => void }) {
  const [target, setTarget] = useState<MeshHub | null>(null);
  return (
    <>
      <Card
        title={`组里的 hub · ${mesh.hubs.length} 台`}
        footer={`中心服务所有 App、保存所有状态、连所有电脑；成员把请求和自己的电脑交给中心。中心连不上时不会自动换，需要时在这里手动设。任期 ${mesh.term}。`}
      >
        {sortMeshHubs(mesh.hubs).map((h) => (
          <div key={h.hub_id} className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3">
            <span className={cx("flex size-9 shrink-0 items-center justify-center rounded-[10px] shadow-hairline", h.role === "center" ? "bg-accent-tint text-accent-ink" : "bg-inset text-ink-2")}>
              <AdminIcon name="mesh" size={16} />
            </span>
            <div className="min-w-[180px] flex-1">
              <div className="flex min-w-0 items-center gap-2">
                <span className="truncate text-[13.5px] font-medium text-ink">{h.name || h.hub_id.slice(0, 8)}</span>
                <Tag tone={h.role === "center" ? "accent" : "gray"}>{h.role === "center" ? "中心" : "成员"}</Tag>
                {h.self && <Tag>这台</Tag>}
              </div>
              <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11.5px] text-ink-3">
                <span className="flex items-center gap-1.5 whitespace-nowrap">
                  <Dot tone={h.online || h.self ? "green" : "gray"} />
                  <span className={h.online || h.self ? "text-green" : "text-ink-2"}>{h.online || h.self ? "在线" : "连不上"}</span>
                </span>
                <span className="truncate font-mono" title={h.hub_id}>
                  {h.hub_id.slice(0, 8)}
                </span>
                {!h.self && !h.paired && <span className="whitespace-nowrap">经组里别的 hub 连着</span>}
              </div>
            </div>
            {h.role !== "center" && (
              <Button className="h-7 px-2.5" disabled={!h.online && !h.self} title={!h.online && !h.self ? "连不上这台 hub，等它在线再设" : undefined} onClick={() => setTarget(h)}>
                设为中心
              </Button>
            )}
          </div>
        ))}
      </Card>
      <SetCenter mesh={mesh} target={target} onClose={() => setTarget(null)} onDone={onChanged} />
    </>
  );
}

// SetCenter asks first, then hands the group over. When the center now in place cannot be reached the hub answers 409
// confirm_required: the dialog says what that means and goes ahead with confirm on the second press.
function SetCenter({ mesh, target, onClose, onDone }: { mesh: MeshView; target: MeshHub | null; onClose: () => void; onDone: () => void }) {
  const [offline, setOffline] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!target) return;
    setOffline(!mesh.center.online);
    setError("");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [target]);
  const old = centerName(mesh);
  // "这台 hub " keeps the space before the next Chinese word; a name in「」needs none.
  const name = target ? (target.self ? "这台 hub " : `「${target.name}」`) : "";
  const go = async () => {
    if (!target) return;
    setBusy(true);
    setError("");
    try {
      const m = await setCenter(target.hub_id, offline);
      share({ mesh: m });
      toast(`${name}现在是中心`, "ok");
      onDone();
      onClose();
    } catch (e) {
      if (e instanceof AdminError && e.code === "confirm_required") setOffline(true);
      else if (e instanceof AdminError && e.code === "unreachable") setError(`连不上${name}，等它在线再设。`);
      else setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={Boolean(target)}
      onClose={() => !busy && onClose()}
      title={target ? `把${name}设为中心？` : ""}
      width={480}
      footer={
        <>
          <Button tone="quiet" onClick={onClose} disabled={busy}>
            取消
          </Button>
          <Button tone={offline ? "danger" : "primary"} busy={busy} onClick={go}>
            {offline ? "仍然设为中心" : "设为中心"}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <p className="text-[12.5px] leading-relaxed text-ink-2">
          设好以后，组里的设备凭据、推送、会话和所有电脑都改由{name}管理；App 连组里哪台 hub 都会转到它。
          {!offline && `现在的中心「${old}」变成成员。`}
        </p>
        {offline && (
          <div className="flex items-start gap-2 rounded-control bg-orange-tint px-3 py-2.5 text-[12.5px] leading-relaxed text-orange" style={{ animation: "fade-in 160ms ease-out both" }}>
            <AdminIcon name="alert" size={14} className="mt-0.5 shrink-0" />
            <span>现在的中心「{old}」连不上；设为中心后，它回来时会变成成员。</span>
          </div>
        )}
        {error && (
          <p className="text-[12.5px] text-red" role="alert">
            {error}
          </p>
        )}
      </div>
    </Dialog>
  );
}

function MakeCode({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [code, setCode] = useState<PeerCode | null>(null);
  const [error, setError] = useState("");
  const now = useNow(1000, Boolean(code));
  useEffect(() => {
    if (!open) return;
    let live = true;
    setCode(null);
    setError("");
    adminApi<PeerCode>("/peers/code", "POST").then(
      (c) => live && setCode(c),
      (e) => live && setError(errorText(e)),
    );
    return () => {
      live = false;
    };
  }, [open]);
  const left = code ? countdown(code.expires_at, now) : "";
  return (
    <Dialog open={open} onClose={onClose} title="本 hub 的互联码" width={520}>
      {!code && !error && <div className="py-6 text-center text-[12.5px] text-ink-3">正在生成…</div>}
      {error && <p className="py-3 text-[12.5px] text-red">{error}</p>}
      {code && (
        <div className="space-y-3">
          <p className="text-[12.5px] leading-relaxed text-ink-2">
            在另一台 hub 的管理界面「互联 › 添加 hub」里核对这台 hub 之后粘贴。只能用一次，
            {left ? (
              <>
                还有 <span className="font-mono text-ink">{left}</span> 过期。
              </>
            ) : (
              <span className="text-red">已经过期，关掉重新生成。</span>
            )}
          </p>
          <div className="rounded-control bg-inset p-2.5 shadow-hairline">
            <CopyValue value={code.code} />
          </div>
          <p className="text-[11.5px] leading-relaxed text-ink-3">拿到互联码的 hub 能看到这台 hub 的所有电脑，只发给你自己的 hub。</p>
        </div>
      )}
    </Dialog>
  );
}

// AddPeer links with another hub in two steps: probe its address and check who answered (name, hub id, key
// fingerprint against that hub's own admin page), then paste the peer code made there. No TLS there: no link.
function AddPeer({ target, onClose, onJoined }: { target: DiscoveredHub | true | null; onClose: () => void; onJoined: () => void }) {
  const [address, setAddress] = useState("");
  const [probe, setProbe] = useState<PeerProbe | null>(null);
  const [probing, setProbing] = useState(false);
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const check = async (addr = address) => {
    const a = addr.trim();
    if (!a) return;
    setProbing(true);
    setProbe(null);
    setError("");
    try {
      setProbe(await adminApi<PeerProbe>("/peers/probe", "POST", { address: a }));
    } catch (e) {
      setError(errorText(e));
    } finally {
      setProbing(false);
    }
  };

  // A discovered hub starts at its first address, checked right away.
  useEffect(() => {
    if (!target) return;
    const first = target === true ? "" : (target.addresses[0] ?? "");
    setAddress(first);
    setProbe(null);
    setCode("");
    setError("");
    if (first) void check(first);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [target]);

  const join = async () => {
    setBusy(true);
    setError("");
    try {
      const p = await adminApi<Peer>("/peers", "POST", { code: code.trim() });
      toast(`已和「${p.name}」互联`, "ok");
      onJoined();
      onClose();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const ok = probe && probeLinkable(probe);
  return (
    <Dialog
      open={Boolean(target)}
      onClose={onClose}
      title={target && target !== true ? `和「${target.name}」互联` : "添加 hub"}
      width={540}
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>
            取消
          </Button>
          <Button tone="primary" busy={busy} disabled={!ok || !/^(?:jarvis|persona):\/\//.test(code.trim())} onClick={join}>
            互联
          </Button>
        </>
      }
    >
      <div className="space-y-3.5">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void check();
          }}
          className="flex items-end gap-2"
        >
          <div className="min-w-0 flex-1">
            <Field
              label="1. 对方的地址"
              value={address}
              onChange={(v) => (setAddress(v), setProbe(null))}
              placeholder="https://192.0.2.20:18662 或 hub.example.com:18662"
              mono
              autoFocus={target === true}
            />
          </div>
          <Button type="submit" className="mb-px h-9" busy={probing} disabled={!address.trim()}>
            核对
          </Button>
        </form>

        {probe && (
          <div className="overflow-hidden rounded-control bg-inset shadow-hairline" style={{ animation: "fade-in 160ms ease-out both" }}>
            <div className="divide-y divide-line">
              <ProbeLine label="名字">
                <span className="text-[13px] font-medium text-ink">{probe.name}</span>
                {probe.version && <span className="font-mono text-[11px] text-ink-3">{probe.version}</span>}
              </ProbeLine>
              <ProbeLine label="hub ID">
                <span className="truncate font-mono text-[12px] text-ink-2" title={probe.hub_id}>
                  {probe.hub_id}
                </span>
              </ProbeLine>
              <ProbeLine label="指纹">
                {probe.fp ? (
                  <span className="font-mono text-[12px] break-all text-ink">{probe.fp}</span>
                ) : (
                  <span className="text-[12px] text-ink-3">没有</span>
                )}
              </ProbeLine>
            </div>
            {ok ? (
              <p className="border-t border-line px-3 py-2 text-[11.5px] leading-relaxed text-ink-3">
                和对方管理界面「概览」里显示的 hub ID、公钥指纹核对一致，再继续。对不上就别互联：可能连错了，或者中间有人冒充。
              </p>
            ) : (
              <div className="flex items-start gap-2 border-t border-line bg-orange-tint px-3 py-2 text-[12px] leading-relaxed text-orange">
                <AdminIcon name="lock" size={13} className="mt-0.5 shrink-0" />
                对方没开 TLS，不能互联。去对方的管理界面「网络与访问」开启 TLS，重启那台 hub 后再来核对。
              </div>
            )}
          </div>
        )}

        <Field
          label="2. 对方的互联码"
          hint={ok ? "在对方的管理界面「互联 › 生成互联码」，10 分钟内有效，只能用一次" : probe ? "对方开启 TLS 后才能互联" : "先核对对方的地址"}
          value={code}
          onChange={setCode}
          placeholder="jarvis://peer?…"
          mono
          multiline
          disabled={!ok}
        />
        {error && (
          <p className="text-[12.5px] text-red" role="alert">
            {error}
          </p>
        )}
      </div>
    </Dialog>
  );
}

function ProbeLine({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-3 px-3 py-2">
      <span className="w-12 shrink-0 text-[11.5px] text-ink-3">{label}</span>
      <div className="flex min-w-0 flex-1 items-center gap-2">{children}</div>
    </div>
  );
}
