// 电脑: every computer the hub drives and every way it reaches each one (paths), adding computers, jarvis-link pairing,
// re-pairing a connector, reconnect, remove. New computers come through the node service (docs/plan-hub-center.md 0,
// 4): the computer dials the hub (kind link, a jarvis-link pairing link) or the hub dials the computer's connector
// (kind service, the jarvis://connector link it prints). Both connect at once; removing waits for a restart.
import { useEffect, useState } from "react";
import { ago } from "../../lib/format";
import { Button, Collapse, cx, Dialog, IconButton, toast, useNow } from "../../ui/atoms";
import { adminApi, AdminError, addLinkMachine, addServiceMachine, errorText, linkPair, repairConnector } from "../api";
import { AdminIcon, type AdminIconName } from "../icons";
import { canRemove, countdown, isLegacy, linuxInstallCommand, machineStatus, needsRepair, parseConnectorLink, pathRows, validHostPort, validMachineId, validProxy, viaLabel } from "../logic";
import { devDialog, navigate } from "../router";
import { share, useShared } from "../store";
import type { AdminMachine, LinkPair, MachinesView, MeshView, Restart } from "../types";
import { Card, CodeBlock, ConfirmButton, CopyValue, Dot, EmptyState, ErrorBox, Field, Loader, Notice, PageHeader, Segmented, Tag, useLoad } from "../parts";

const enc = encodeURIComponent;

type AddWay = "link" | "service";

const LEGACY_TIP = "ssh 已不推荐：在那台电脑上装 node 服务，用「电脑来连」或「hub 来连」重新添加，确认在线后删掉这一条。";

export function Machines() {
  const { mesh, hubId, tls } = useShared();
  const { data, error, loading, reload } = useLoad(async () => {
    const v = await adminApi<MachinesView>("/machines");
    share({ restart: v.restart });
    return v;
  });
  const [adding, setAdding] = useState<AddWay | null>(() => {
    const d = devDialog();
    return d === "add-link" || d === "add" ? "link" : d === "add-service" ? "service" : null;
  });
  const [pairing, setPairing] = useState<AdminMachine | null>(null);
  const [repairing, setRepairing] = useState<AdminMachine | null>(null);

  // Dev only (screenshots): ?dialog=pair / repair opens it on the first computer it fits.
  const [devOpened, setDevOpened] = useState(false);
  useEffect(() => {
    const d = devDialog();
    if (!data || devOpened || (d !== "pair" && d !== "repair")) return;
    setDevOpened(true);
    if (d === "pair") setPairing(data.machines.find((m) => m.kind === "link") ?? null);
    else setRepairing(data.machines.find(needsRepair) ?? null);
  }, [data, devOpened]);

  // Online state and the paths' round trips move on their own.
  useEffect(() => {
    if (!data) return;
    const t = setInterval(reload, 15_000);
    return () => clearInterval(t);
  }, [data, reload]);

  const changed = (r?: Restart) => {
    if (r) share({ restart: r });
    reload();
  };

  return (
    <>
      <PageHeader title="电脑" subtitle="hub 连着的电脑。全家只要这一台 hub，其他电脑都装 node 服务，再在这里添加。" />
      <AddCard onPick={setAdding} noTLS={tls === null} />
      {!data ? (
        loading ? <Loader /> : <ErrorBox error={error} onRetry={reload} />
      ) : (
        <Card
          title={`${data.machines.length} 台`}
          footer="添加立刻生效；删除写进 hub.json，重启 hub 后生效。一台电脑有几条路时同一时刻只用一条：本机优先，其次直接连，最后经成员 hub；断了马上换下一条。"
        >
          {data.machines.length === 0 ? (
            <EmptyState icon="machine" title="还没有电脑">
              在上面选一种方式添加：笔记本选「电脑来连」，服务器选「hub 来连」。
            </EmptyState>
          ) : (
            data.machines.map((m) => (
              <MachineRow key={m.id} m={m} mesh={mesh} selfId={hubId} tls={data.tls} onChanged={changed} onPair={() => setPairing(m)} onRepair={() => setRepairing(m)} />
            ))
          )}
        </Card>
      )}
      <AddLink open={adding === "link"} onClose={() => setAdding(null)} existing={data?.machines.map((m) => m.id) ?? []} onAdded={() => changed()} />
      <AddService open={adding === "service"} onClose={() => setAdding(null)} existing={data?.machines.map((m) => m.id) ?? []} onAdded={() => changed()} />
      <PairLinkDialog machine={pairing} onClose={() => setPairing(null)} />
      <RepairDialog machine={repairing} onClose={() => setRepairing(null)} onDone={() => changed()} />
    </>
  );
}

// AddCard is the page's lead: the two ways a computer joins, side by side.
function AddCard({ onPick, noTLS }: { onPick: (w: AddWay) => void; noTLS: boolean }) {
  const ways: { w: AddWay; icon: AdminIconName; title: string; fit: string; desc: string; note?: string }[] = [
    {
      w: "link",
      icon: "link",
      title: "电脑来连",
      fit: "笔记本、会换网络的电脑",
      desc: "那台电脑上的 Persona Link 主动连 hub：在 NAT 后面、换了网络也能连上。",
      note: noTLS ? "要先开 TLS" : undefined,
    },
    {
      w: "service",
      icon: "machine",
      title: "hub 来连",
      fit: "服务器、开发工作站这类地址固定的电脑",
      desc: "hub 按地址连那台电脑上的连接器，也能经 SOCKS 代理连过去。",
    },
  ];
  return (
    <section className="mb-6">
      <div className="mb-1.5 flex min-h-6 items-end px-1">
        <h2 className="text-[12px] font-medium text-ink-3">添加电脑</h2>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        {ways.map((o) => (
          <button
            key={o.w}
            type="button"
            onClick={() => onPick(o.w)}
            className="group flex items-start gap-3 rounded-card bg-surface px-3.5 py-3.5 text-left shadow-card transition-[background-color,transform] duration-150 hover:bg-hover active:scale-[0.99]"
          >
            <span className="flex size-9 shrink-0 items-center justify-center rounded-[10px] bg-accent-tint text-accent-ink">
              <AdminIcon name={o.icon} size={17} />
            </span>
            <span className="min-w-0 flex-1">
              <span className="flex items-center gap-2">
                <span className="text-[13.5px] font-semibold text-ink">{o.title}</span>
                {o.note && <Tag tone="orange">{o.note}</Tag>}
              </span>
              <span className="mt-0.5 block text-[12px] font-medium text-ink-2">{o.fit}</span>
              <span className="mt-1 block text-[11.5px] leading-relaxed text-ink-3">{o.desc}</span>
            </span>
            <AdminIcon name="chevronRight" size={15} className="mt-2.5 shrink-0 text-ink-3 transition-transform duration-150 group-hover:translate-x-0.5" />
          </button>
        ))}
      </div>
    </section>
  );
}

function MachineRow({
  m,
  mesh,
  selfId,
  tls,
  onChanged,
  onPair,
  onRepair,
}: {
  m: AdminMachine;
  mesh: MeshView | null | undefined;
  selfId: string;
  tls: boolean;
  onChanged: (r?: Restart) => void;
  onPair: () => void;
  onRepair: () => void;
}) {
  const st = machineStatus(m);
  const [busy, setBusy] = useState("");
  const active = m.state === "active";
  const act = async (what: string, fn: () => Promise<{ restart?: Restart }>, ok: string) => {
    setBusy(what);
    try {
      const r = await fn();
      toast(ok, "ok");
      onChanged(r.restart);
    } catch (e) {
      toast(errorText(e), "error");
    } finally {
      setBusy("");
    }
  };
  const paths = pathRows(m, mesh, selfId);
  const now = paths.find((p) => p.active);
  // How it is reached now: the path in use (through a member: which one), else how it is configured.
  const how = now ? (now.via === "hub" ? `经成员「${now.hub}」` : now.label) : viaLabel(m.transport) || viaLabel(m.kind);
  const member = m.kind === "hub" ? paths[0]?.hub || mesh?.hubs.find((h) => h.hub_id === m.hub_id)?.name || "" : "";
  const repair = needsRepair(m);
  const icon: AdminIconName = m.kind === "hub" ? "mesh" : m.kind === "link" ? "link" : "machine";
  return (
    <div className={cx("px-4 py-3", m.state === "pending_remove" && "opacity-60")}>
      <div className="flex flex-wrap items-start gap-x-3 gap-y-2">
        <span className="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-[10px] bg-inset text-ink-2 shadow-hairline">
          <AdminIcon name={icon} size={16} />
        </span>
        <div className="min-w-[200px] flex-1">
          <div className="flex min-w-0 items-center gap-2">
            <span className="truncate text-[13.5px] font-medium text-ink">{m.name || m.id}</span>
            <span className="shrink-0 font-mono text-[11.5px] text-ink-3">{m.id}</span>
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11.5px] text-ink-3">
            <span className="flex items-center gap-1.5 whitespace-nowrap">
              <Dot tone={st.tone} />
              <span className={st.tone === "red" ? "text-red" : st.tone === "green" ? "text-green" : st.tone === "orange" ? "text-orange" : "text-ink-2"}>{st.label}</span>
            </span>
            {[
              <span key="how" className="whitespace-nowrap">
                {how}
                {now?.rtt !== undefined && m.online && <span className="ml-1.5 font-mono tabular-nums">{now.rtt} ms</span>}
              </span>,
              m.address && (
                <span key="addr" className="font-mono">
                  {m.address}
                </span>
              ),
              m.proxy && (
                <span key="proxy" className="whitespace-nowrap">
                  经代理 <span className="font-mono">{m.proxy}</span>
                </span>
              ),
              m.ssh_host && (
                <span key="ssh" className="font-mono">
                  {m.ssh_host}
                </span>
              ),
              m.node_version && (
                <span key="ver" className="font-mono">
                  {m.node_version}
                </span>
              ),
              !m.online && active && m.last_seen && (
                <span key="seen" className="whitespace-nowrap">
                  最后在线 {ago(m.last_seen)}
                </span>
              ),
            ]
              .filter(Boolean)
              .flatMap((el, i) => [<span key={`sep${i}`}>·</span>, el])}
            {m.node_mismatch && <Tag tone="orange" title="这台电脑的 node 不是 hub 的版本：到那台电脑上更新 node 服务">版本不一致</Tag>}
            {isLegacy(m) && <Tag title={LEGACY_TIP}>ssh 已不推荐，迁移见文档</Tag>}
          </div>
          {member && <div className="mt-1 text-[11.5px] leading-relaxed text-ink-3">只在成员「{member}」上配置，中心经它连这台；要删到那台 hub 的管理界面。</div>}
          {m.error && active && <div className="mt-1 text-[11.5px] leading-relaxed break-words text-red">{m.error}</div>}
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {repair && active && (
            <Button tone="primary" className="h-7 px-2.5" onClick={onRepair}>
              <AdminIcon name="link" size={13} />
              重新配对
            </Button>
          )}
          {m.kind === "link" && active && (
            <Button className="h-7 px-2.5" onClick={onPair} title={tls ? "给这台电脑的 Persona Link 生成配对链接" : "电脑来连只走 TLS，先在网络与访问开启"}>
              <AdminIcon name="link" size={13} />
              生成配对链接
            </Button>
          )}
          {active && (
            <IconButton
              icon="retry"
              label="重连"
              disabled={busy !== ""}
              onClick={() => act("reconnect", () => adminApi(`/machines/${enc(m.id)}/reconnect`, "POST"), "正在重连")}
            />
          )}
          {canRemove(m) && (
            <ConfirmButton
              label="删除"
              confirm="删除"
              busy={busy === "delete"}
              onConfirm={() => act("delete", () => adminApi(`/machines/${enc(m.id)}`, "DELETE"), m.state === "pending_add" ? "已撤回" : "已删除，重启 hub 后生效")}
            />
          )}
        </div>
      </div>
      {paths.length > 1 && <Paths rows={paths} />}
    </div>
  );
}

// Paths: every way to a computer — how, through which hub, whether it works, its round trip, the one in use.
function Paths({ rows }: { rows: ReturnType<typeof pathRows> }) {
  return (
    <div className="mt-2 ml-12 max-w-[560px] divide-y divide-line overflow-hidden rounded-control bg-inset shadow-hairline" role="list" aria-label="路">
      {rows.map((r) => (
        <div key={r.key} role="listitem" className="px-2.5 py-1.5 text-[11.5px]">
          <div className="flex items-center gap-2.5">
            <Dot tone={r.ok ? "green" : r.error ? "red" : "gray"} />
            <span className="w-[68px] shrink-0 whitespace-nowrap text-ink-2">{r.label}</span>
            <span className="flex min-w-0 flex-1 items-center gap-1 text-ink-3" title={`经过 ${r.hub}`}>
              <AdminIcon name="mesh" size={11} className="shrink-0" />
              <span className="truncate">{r.hub}</span>
            </span>
            {r.rtt !== undefined && <span className="shrink-0 font-mono text-ink-3 tabular-nums">{r.rtt} ms</span>}
            {r.active ? <Tag tone="accent">在用</Tag> : r.ok ? <Tag>备用</Tag> : <Tag tone="red">不通</Tag>}
          </div>
          {r.error && (
            <div className="mt-0.5 truncate pl-[18px] text-red" title={r.error}>
              {r.error}
            </div>
          )}
        </div>
      ))}
    </div>
  );
}

const ICONS: { value: string; label: string }[] = [
  { value: "laptop", label: "笔记本" },
  { value: "desktop", label: "台式机" },
  { value: "server", label: "服务器" },
];

// MachineFields: id, name and icon, the same for both ways.
function MachineFields({
  id,
  setId,
  name,
  setName,
  icon,
  setIcon,
  existing,
  autoFocus,
  example,
}: {
  example: { id: string; name: string };
  id: string;
  setId: (v: string) => void;
  name: string;
  setName: (v: string) => void;
  icon: string;
  setIcon: (v: string) => void;
  existing: string[];
  autoFocus?: boolean;
}) {
  const idErr = idError(id, existing);
  return (
    <>
      <div className="grid gap-3.5 sm:grid-cols-2">
        <Field label="id" hint="App 里用的标识，之后不能改" value={id} onChange={(v) => setId(v.toLowerCase())} placeholder={example.id} mono error={idErr || undefined} autoFocus={autoFocus} maxLength={63} />
        <Field label="名字" hint="App 里显示的名字，不填就用 id" value={name} onChange={setName} placeholder={example.name} maxLength={40} />
      </div>
      <div>
        <span className="mb-1 block text-[12px] font-medium text-ink-2">图标</span>
        <Segmented label="图标" value={icon} options={ICONS} onChange={setIcon} />
      </div>
    </>
  );
}

function idError(id: string, existing: string[]): string {
  if (!id) return "";
  if (!validMachineId(id)) return "只能用小写字母、数字和 - _ .，以字母或数字开头";
  return existing.includes(id) ? "已经有这个 id 了" : "";
}

// AddLink: the computer dials the hub. Make the entry, then the jarvis-link pairing link right away.
function AddLink({ open, onClose, existing, onAdded }: { open: boolean; onClose: () => void; existing: string[]; onAdded: () => void }) {
  const [id, setId] = useState("");
  const [name, setName] = useState("");
  const [icon, setIcon] = useState("laptop");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [added, setAdded] = useState<AdminMachine | null>(null);
  const ok = id && !idError(id, existing);
  const close = () => {
    onClose();
    setTimeout(() => (setId(""), setName(""), setIcon("laptop"), setError(""), setAdded(null)), 200);
  };
  const submit = async (e?: React.FormEvent) => {
    e?.preventDefault();
    if (!ok || busy) return;
    setBusy(true);
    setError("");
    try {
      const r = await addLinkMachine({ id, name: name.trim() || id, icon });
      setAdded(r.machine?.id ? r.machine : { id, name: name.trim() || id, kind: "link", icon, online: false, state: "active" });
      onAdded();
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={open}
      onClose={close}
      title={added ? `配对「${added.name || added.id}」` : "添加电脑 · 电脑来连"}
      width={540}
      footer={
        added ? (
          <Button tone="primary" onClick={close}>
            完成
          </Button>
        ) : (
          <>
            <Button tone="quiet" onClick={close}>
              取消
            </Button>
            <Button tone="primary" busy={busy} disabled={!ok} onClick={() => submit()}>
              添加并生成配对链接
            </Button>
          </>
        )
      }
    >
      {added ? (
        <PairPanel machine={added} onClose={close} />
      ) : (
        <form onSubmit={submit} className="space-y-3.5">
          <p className="text-[12.5px] leading-relaxed text-ink-2">
            那台电脑上的 Persona Link 主动连这台 hub，适合笔记本、会换网络的电脑。加好之后马上生成配对链接，到那台电脑上粘贴。
          </p>
          <MachineFields example={{ id: "laptop", name: "笔记本" }} id={id} setId={setId} name={name} setName={setName} icon={icon} setIcon={setIcon} existing={existing} autoFocus />
          {error && <FormError text={error} />}
          <button type="submit" hidden />
        </form>
      )}
    </Dialog>
  );
}

type Platform = "mac" | "linux" | "windows";

// PairPanel makes a jarvis-link pairing link for a link computer and says how to use it on each platform.
function PairPanel({ machine, onClose }: { machine: AdminMachine; onClose: () => void }) {
  const [pair, setPair] = useState<LinkPair | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [round, setRound] = useState(0);
  const [platform, setPlatform] = useState<Platform>(machine.icon === "server" ? "linux" : "mac");
  const now = useNow(1000, Boolean(pair));
  useEffect(() => {
    let live = true;
    setPair(null);
    setError(null);
    linkPair(machine.id).then(
      (p) => live && setPair(p),
      (e) => live && setError(e),
    );
    return () => {
      live = false;
    };
  }, [machine.id, round]);
  const left = pair ? countdown(pair.expires_at, now) : "";
  const tls = error instanceof AdminError && error.code === "tls_required";
  if (tls)
    return (
      <Notice tone="orange" icon="lock" title="电脑来连只走 TLS" action={<Button onClick={() => (onClose(), navigate("network"))}>去开启</Button>}>
        这台电脑已经加好了，但 hub 还没开 TLS 端口。在「网络与访问」里填上 TLS 监听、重启 hub 后，到「电脑」列表里给它「配对链接」。
      </Notice>
    );
  if (error)
    return (
      <div className="flex flex-wrap items-center gap-3 py-3">
        <p className="min-w-0 flex-1 text-[12.5px] text-red">{errorText(error)}</p>
        <Button icon="retry" onClick={() => setRound((r) => r + 1)}>
          重试
        </Button>
      </div>
    );
  if (!pair) return <div className="py-8 text-center text-[12.5px] text-ink-3">正在生成配对链接…</div>;
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <p className="min-w-0 flex-1 text-[12.5px] leading-relaxed text-ink-2">
          链接只能用一次，
          {left ? (
            <>
              还有 <span className="font-mono text-ink tabular-nums">{left}</span> 过期。
            </>
          ) : (
            <span className="text-red">已经过期了。</span>
          )}
        </p>
        <Button tone={left ? "quiet" : "primary"} icon="retry" className="h-7 px-2.5" onClick={() => setRound((r) => r + 1)}>
          重新生成
        </Button>
      </div>
      <div className={cx("rounded-control bg-inset p-2.5 shadow-hairline transition-opacity", !left && "opacity-50")}>
        <div className="mb-1 text-[11px] font-medium text-ink-3">配对链接</div>
        <CopyValue value={pair.pair_url} label="拷贝配对链接" />
      </div>
      <div>
        <Segmented
          label="那台电脑的系统"
          value={platform}
          onChange={setPlatform}
          options={[
            { value: "mac", label: "Mac" },
            { value: "linux", label: "Linux" },
            { value: "windows", label: "Windows" },
          ]}
        />
        <div className="mt-2.5 text-[12.5px] leading-relaxed text-ink-2" style={{ animation: "fade-in 160ms ease-out both" }} key={platform}>
          {platform === "mac" && (
            <ol className="list-decimal space-y-1 pl-4 marker:text-ink-3">
              <li>装 Persona Link（菜单栏 App，自带 node 服务），打开它。</li>
              <li>点菜单栏里的 Persona Link ›「配对 / 修改连接…」，粘贴上面的链接。</li>
              <li>菜单里显示已连接，这里的列表里它就在线了。</li>
            </ol>
          )}
          {platform === "linux" && (
            <div className="space-y-2">
              <p>下载 jarvis-link 后在那台电脑上运行，一条命令配对、装成 systemd 用户服务并连上：</p>
              <CodeBlock value={linuxInstallCommand(pair.pair_url)} copyLabel="拷贝命令" />
              <p className="text-[11.5px] text-ink-3">没人登录时也要连着的话，再运行 sudo loginctl enable-linger 加你的用户名。</p>
            </div>
          )}
          {platform === "windows" && <p>Windows 的托盘程序还在做，做好后在托盘菜单里粘贴同一个链接。</p>}
        </div>
      </div>
      <p className="text-[11.5px] leading-relaxed text-ink-3">链接相当于这台电脑的凭据，别发到群里。过期了可以随时在「电脑」列表里重新生成。</p>
    </div>
  );
}

function PairLinkDialog({ machine, onClose }: { machine: AdminMachine | null; onClose: () => void }) {
  return (
    <Dialog open={Boolean(machine)} onClose={onClose} title={machine ? `「${machine.name || machine.id}」的配对链接` : ""} width={540}>
      {machine && <PairPanel machine={machine} onClose={onClose} />}
    </Dialog>
  );
}

// ConnectorLinkField: the jarvis://connector link pasted from `jarvis-link pair --listen`, read back to the user.
function ConnectorLinkField({ value, onChange, autoFocus }: { value: string; onChange: (v: string) => void; autoFocus?: boolean }) {
  const parsed = value.trim() ? parseConnectorLink(value) : null;
  const bad = value.trim() !== "" && !parsed;
  return (
    <div>
      <Field label="配对链接" value={value} onChange={onChange} placeholder="jarvis://connector?addr=…&fp=…&code=…" mono multiline autoFocus={autoFocus} error={bad ? "这不是连接器的配对链接（jarvis://connector?…），整条拷过来" : undefined} />
      {parsed && (
        <div className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-[11.5px] text-ink-3" style={{ animation: "fade-in 160ms ease-out both" }}>
          <span>
            地址 <span className="font-mono text-ink-2">{parsed.addrs.length ? parsed.addrs.join("、") : "链接里没有，下面填上"}</span>
          </span>
          <span className="min-w-0 truncate">
            指纹 <span className="font-mono text-ink-2">{parsed.fp.slice(0, 12)}…</span>
          </span>
        </div>
      )}
    </div>
  );
}

// AddService: the hub dials the computer's connector. The hub pairs with the pasted link first (up to ~40 s), then
// writes hub.json and connects.
function AddService({ open, onClose, existing, onAdded }: { open: boolean; onClose: () => void; existing: string[]; onAdded: () => void }) {
  const [link, setLink] = useState("");
  const [id, setId] = useState("");
  const [name, setName] = useState("");
  const [icon, setIcon] = useState("server");
  const [address, setAddress] = useState("");
  const [proxy, setProxy] = useState("");
  const [more, setMore] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [added, setAdded] = useState<AdminMachine | null>(null);

  const parsed = link.trim() ? parseConnectorLink(link) : null;
  const needAddress = Boolean(parsed && parsed.addrs.length === 0);
  const addrErr = address.trim() && !validHostPort(address) ? "写成 主机:端口，IPv6 带方括号，例如 [2001:db8::30]:18663" : "";
  const proxyErr = proxy.trim() && !validProxy(proxy) ? "写成 socks5://主机:端口" : "";
  const ok = parsed && id && !idError(id, existing) && !addrErr && !proxyErr && (!needAddress || address.trim());
  useEffect(() => {
    if (needAddress) setMore(true);
  }, [needAddress]);

  const close = () => {
    if (busy) return;
    onClose();
    setTimeout(() => (setLink(""), setId(""), setName(""), setIcon("server"), setAddress(""), setProxy(""), setMore(false), setError(""), setAdded(null)), 200);
  };
  const submit = async (e?: React.FormEvent) => {
    e?.preventDefault();
    if (!ok || busy) return;
    setBusy(true);
    setError("");
    try {
      const r = await addServiceMachine({ id, name: name.trim() || id, icon, pair_link: link.trim(), address: address.trim() || undefined, proxy: proxy.trim() || undefined });
      setAdded(r.machine?.id ? r.machine : { id, name: name.trim() || id, kind: "service", icon, online: false, state: "active", address: address.trim() || parsed?.addrs[0] });
      onAdded();
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={open}
      onClose={close}
      title={added ? "已添加" : "添加电脑 · hub 来连"}
      width={560}
      footer={
        added ? (
          <Button tone="primary" onClick={close}>
            完成
          </Button>
        ) : (
          <>
            <Button tone="quiet" onClick={close} disabled={busy}>
              取消
            </Button>
            <Button tone="primary" busy={busy} disabled={!ok} onClick={() => submit()}>
              配对并添加
            </Button>
          </>
        )
      }
    >
      {added ? (
        <div className="flex items-start gap-3 py-1" style={{ animation: "fade-in 160ms ease-out both" }}>
          <span className="flex size-9 shrink-0 items-center justify-center rounded-full bg-green-tint text-green">
            <AdminIcon name="check" size={17} stroke={2.4} />
          </span>
          <div className="min-w-0 flex-1">
            <div className="text-[13.5px] font-medium text-ink">已和「{added.name || added.id}」配对</div>
            <p className="mt-0.5 text-[12.5px] leading-relaxed text-ink-2">hub 正在连它，几秒后在列表里显示在线。</p>
            {added.address && (
              <p className="mt-1.5 text-[11.5px] text-ink-3">
                地址 <span className="font-mono text-ink-2">{added.address}</span>
                {added.proxy && (
                  <>
                    {" "}
                    · 经代理 <span className="font-mono text-ink-2">{added.proxy}</span>
                  </>
                )}
              </p>
            )}
          </div>
        </div>
      ) : (
        <form onSubmit={submit} className="space-y-3.5">
          <div>
            <p className="mb-2 text-[12.5px] leading-relaxed text-ink-2">hub 按地址连那台电脑上的连接器，适合服务器、开发工作站这类地址固定、hub 连得到的电脑。先在那台电脑上运行：</p>
            <CodeBlock value="jarvis-link pair --listen" copyLabel="拷贝命令" />
            <p className="mt-1.5 text-[11.5px] leading-relaxed text-ink-3">
              Mac 的菜单栏、Windows 的托盘里选「让 hub 来连」也一样。它会打印一条 jarvis://connector?… 链接，10 分钟内有效，只能用一次。
            </p>
          </div>
          <ConnectorLinkField value={link} onChange={setLink} autoFocus />
          <MachineFields example={{ id: "example-workstation", name: "开发机" }} id={id} setId={setId} name={name} setName={setName} icon={icon} setIcon={setIcon} existing={existing} />
          <div>
            <button type="button" onClick={() => setMore((v) => !v)} aria-expanded={more} className="flex items-center gap-1 text-[12px] font-medium whitespace-nowrap text-ink-2 hover:text-ink">
              <AdminIcon name="chevronRight" size={13} className="transition-transform duration-200" style={{ transform: more ? "rotate(90deg)" : undefined }} />
              地址和代理{needAddress ? "" : "（可选）"}
            </button>
            <Collapse open={more}>
              <div className="grid gap-3.5 pt-3 sm:grid-cols-2">
                <Field
                  label={needAddress ? "地址" : "地址（可选）"}
                  hint={needAddress ? "链接里没有地址：填 hub 能连到它的 主机:端口" : "不填就用链接里的；hub 要按别的地址连它时填"}
                  value={address}
                  onChange={setAddress}
                  placeholder={parsed?.addrs[0] || "192.0.2.30:18663"}
                  mono
                  error={addrErr || undefined}
                />
                <Field label="代理（可选）" hint="hub 只能经某台机器上的 SOCKS 代理连到它时填" value={proxy} onChange={setProxy} placeholder="socks5://192.0.2.5:1080" mono error={proxyErr || undefined} />
              </div>
            </Collapse>
          </div>
          {busy && <p className="text-[12.5px] text-ink-3">正在配对：hub 正在连那台电脑，最多要几十秒…</p>}
          {error && <FormError text={error} />}
          <button type="submit" hidden />
        </form>
      )}
    </Dialog>
  );
}

// RepairDialog pairs a service computer's connector again: it was paired with another hub, or reinstalled.
function RepairDialog({ machine, onClose, onDone }: { machine: AdminMachine | null; onClose: () => void; onDone: () => void }) {
  const [link, setLink] = useState("");
  const [address, setAddress] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!machine) return;
    setLink("");
    setAddress("");
    setError("");
  }, [machine]);
  const parsed = link.trim() ? parseConnectorLink(link) : null;
  const addrErr = address.trim() && !validHostPort(address) ? "写成 主机:端口，IPv6 带方括号" : "";
  const ok = parsed && !addrErr;
  const close = () => !busy && onClose();
  const submit = async (e?: React.FormEvent) => {
    e?.preventDefault();
    if (!machine || !ok || busy) return;
    setBusy(true);
    setError("");
    try {
      const r = await repairConnector(machine.id, link.trim(), address.trim());
      toast(r.restart_required ? "已重新配对；地址变了，重启 hub 后按新地址连" : "已重新配对，正在连接", "ok");
      onDone();
      onClose();
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={Boolean(machine)}
      onClose={close}
      title={machine ? `重新配对「${machine.name || machine.id}」` : ""}
      width={540}
      footer={
        <>
          <Button tone="quiet" onClick={close} disabled={busy}>
            取消
          </Button>
          <Button tone="primary" busy={busy} disabled={!ok} onClick={() => submit()}>
            重新配对
          </Button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-3.5">
        <p className="text-[12.5px] leading-relaxed text-ink-2">
          这台电脑上的连接器不认这台 hub 了：它配对给了别的 hub，或者重装过。在那台电脑上再运行一次 <code className="font-mono text-[12px] text-ink">jarvis-link pair --listen</code>
          （或在菜单栏 / 托盘里选「让 hub 来连」），把新打印的链接粘贴进来。
        </p>
        <ConnectorLinkField value={link} onChange={setLink} autoFocus />
        <Field label="地址（可选）" hint="不填就用原来的地址；地址变了才填" value={address} onChange={setAddress} placeholder={machine?.address || "192.0.2.30:18663"} mono error={addrErr || undefined} />
        {busy && <p className="text-[12.5px] text-ink-3">正在配对：hub 正在连那台电脑…</p>}
        {error && <FormError text={error} />}
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}

function FormError({ text }: { text: string }) {
  return (
    <div role="alert" className="flex items-start gap-2 rounded-control bg-red-tint px-3 py-2 text-[12.5px] leading-relaxed break-words text-red" style={{ animation: "fade-in 160ms ease-out both" }}>
      <AdminIcon name="alert" size={14} className="mt-0.5 shrink-0" />
      <span className="min-w-0">{text}</span>
    </div>
  );
}
