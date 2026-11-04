// 插件: what extends this hub (docs/plugins.md), installed, upgraded, rolled back, switched and uninstalled from here as
// `personad plugin` does on the hub's machine. Installing runs a new program there, so nothing installs before the
// page has shown what it may do (the preview) and the user agreed to exactly that (its permissions digest).
import { useEffect, useRef, useState } from "react";
import { bytes } from "../../lib/format";
import { Button, Collapse, cx, Dialog, toast, useNow } from "../../ui/atoms";
import { Switch } from "../../views/settings/SettingsParts";
import { AdminError, errorText, getPlugins, installPlugin, previewPlugin, rollbackPlugin, setPluginEnabled, uninstallPlugin, upgradePlugin, uploadPlugin } from "../api";
import { AdminIcon, type AdminIconName } from "../icons";
import { canRollBack, canUpgrade, countdown, pluginRunNote, pluginSourceLabel, pluginStatus, pointLabel, sourceProblem, uploadProblem } from "../logic";
import { devDialog } from "../router";
import { PluginMarkets } from "./PluginMarket";
import type { AdminPlugin, PermissionLine, PluginPreview } from "../types";
import { Card, Dot, EmptyState, ErrorBox, Field, Loader, Notice, PageHeader, Segmented, Tag, useLoad } from "../parts";

const PERM_ICON: Record<string, AdminIconName> = { runs_code: "terminal", network: "web", hub_api: "key", launcher_keys: "gauge", secrets: "lock" };

export function Plugins() {
  const { data, error, loading, reload, set } = useLoad(getPlugins);
  const [installing, setInstalling] = useState(() => ["install", "upload", "preview"].includes(devDialog()));
  const [upgrading, setUpgrading] = useState<AdminPlugin | null>(null);
  const [removing, setRemoving] = useState<AdminPlugin | null>(null);
  const [rolling, setRolling] = useState<AdminPlugin | null>(null);

  // Dev only (screenshots): ?dialog=upgrade / uninstall opens it on the first plugin it fits.
  const [devOpened, setDevOpened] = useState(false);
  useEffect(() => {
    const d = devDialog();
    if (!data || devOpened || (d !== "upgrade" && d !== "uninstall")) return;
    setDevOpened(true);
    const p = data.plugins.find(canUpgrade) ?? null;
    if (d === "upgrade") setUpgrading(p);
    else setRemoving(p);
  }, [data, devOpened]);

  // A plugin's state moves on its own (starting, running, idle).
  useEffect(() => {
    if (!data) return;
    const t = setInterval(reload, 15_000);
    return () => clearInterval(t);
  }, [data, reload]);

  const replace = (p: AdminPlugin) => data && set({ ...data, plugins: data.plugins.map((x) => (x.id === p.id ? p : x)) });
  const missing = error instanceof AdminError && (error.status === 404 || error.code === "no_plugins");

  return (
    <>
      <PageHeader
        title="插件"
        subtitle="给这台 hub 加能力：连接器、语音……装插件就是在 hub 机器上运行一个新程序，所以先看清它能做什么，同意了才装。"
        right={
          data?.can_install && (
            <Button tone="primary" onClick={() => setInstalling(true)}>
              <AdminIcon name="plus" size={13} />
              安装插件
            </Button>
          )
        }
      />
      {missing ? (
        <Card>
          <EmptyState icon="puzzle" title="这台 hub 还不能在这里管插件">
            先把 hub 升级到新版本；在那之前，在 hub 机器上用 <span className="font-mono">personad plugin …</span> 安装和管理。
          </EmptyState>
        </Card>
      ) : !data ? (
        loading ? <Loader /> : <ErrorBox error={error} onRetry={reload} />
      ) : (
        <>
          {!data.can_install && (
            <Notice tone="gray" icon="info" title="这台 hub 没有数据库，装不了插件">
              内置的和 hub.json 里的插件照常能用；装、升级、卸载要 hub 连上 Postgres。
            </Notice>
          )}
          <Card
            title={`${data.plugins.length} 个插件`}
            footer="设置和密钥在 App 的「设置 › 插件」里填。内置的不能卸载，hub.json 里的只能去 hub.json 改。这里的每一步都以「管理界面」记进插件的审计。"
          >
            {data.plugins.length === 0 ? (
              <EmptyState icon="puzzle" title="还没有插件" action={data.can_install && <Button onClick={() => setInstalling(true)}>安装插件</Button>}>
                插件可以来自 hub 机器上的目录或包、带 sha256 的 https 链接、跑在别处的插件（socket），也可以从这里上传。
              </EmptyState>
            ) : (
              data.plugins.map((p) => (
                <PluginRow key={p.id} p={p} onChanged={replace} onUpgrade={() => setUpgrading(p)} onRollBack={() => setRolling(p)} onRemove={() => setRemoving(p)} />
              ))
            )}
          </Card>
          {data.can_install && <PluginMarkets onChanged={reload} />}
        </>
      )}
      <InstallDialog open={installing} uploadMost={data?.upload_most ?? 0} onClose={() => setInstalling(false)} onDone={reload} />
      <InstallDialog open={Boolean(upgrading)} upgrade={upgrading ?? undefined} uploadMost={data?.upload_most ?? 0} onClose={() => setUpgrading(null)} onDone={reload} />
      <RollBackDialog plugin={rolling} onClose={() => setRolling(null)} onDone={(p) => (replace(p), setRolling(null))} />
      <UninstallDialog plugin={removing} onClose={() => setRemoving(null)} onDone={() => (setRemoving(null), reload())} />
    </>
  );
}

function PluginRow({ p, onChanged, onUpgrade, onRollBack, onRemove }: { p: AdminPlugin; onChanged: (p: AdminPlugin) => void; onUpgrade: () => void; onRollBack: () => void; onRemove: () => void }) {
  // Dev only (screenshots): ?dialog=details opens the rows that can roll back.
  const [open, setOpen] = useState(() => devDialog() === "details" && canRollBack(p));
  const [busy, setBusy] = useState(false);
  const st = pluginStatus(p);
  const toggle = async (on: boolean) => {
    setBusy(true);
    try {
      onChanged(await setPluginEnabled(p.id, on));
      toast(on ? `已启用「${p.name}」` : `已停用「${p.name}」`, "ok");
    } catch (e) {
      toast(errorText(e), "error");
    } finally {
      setBusy(false);
    }
  };
  const contributes = Object.entries(p.contributes ?? {});
  return (
    <div className="px-4 py-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <button type="button" onClick={() => setOpen((o) => !o)} aria-expanded={open} className="flex min-w-[200px] flex-1 items-center gap-3 text-left">
          <span className="flex size-9 shrink-0 items-center justify-center rounded-[10px] bg-inset text-ink-2 shadow-hairline">
            <AdminIcon name="puzzle" size={16} />
          </span>
          <span className="min-w-0 flex-1">
            <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
              <span className="truncate text-[13.5px] font-medium text-ink">{p.name}</span>
              {p.version && <span className="font-mono text-[11.5px] text-ink-3">{p.version}</span>}
              <Tag tone={p.source === "installed" ? "accent" : "gray"}>{pluginSourceLabel(p.source)}</Tag>
            </span>
            <span className="mt-1 flex min-w-0 items-center gap-1.5 text-[11.5px] text-ink-3">
              <Dot tone={st.tone} pulse={p.status === "starting"} />
              <span className="whitespace-nowrap">{st.label}</span>
              {p.description && (
                <>
                  <span>·</span>
                  <span className="truncate">{p.description}</span>
                </>
              )}
            </span>
          </span>
        </button>
        <div className="ml-auto flex shrink-0 items-center gap-1.5">
          {canUpgrade(p) && (
            <Button tone="quiet" className="h-7 px-2" onClick={onUpgrade}>
              <AdminIcon name="up" size={13} />
              升级
            </Button>
          )}
          {canRollBack(p) && (
            <Button tone="quiet" className="h-7 px-2" onClick={onRollBack}>
              <AdminIcon name="undo" size={13} />
              回滚
            </Button>
          )}
          {p.source === "installed" && (
            <button
              type="button"
              title="卸载"
              aria-label={`卸载「${p.name}」`}
              onClick={onRemove}
              className="flex size-7 shrink-0 items-center justify-center rounded-[8px] text-ink-3 transition-colors hover:bg-red-tint hover:text-red"
            >
              <AdminIcon name="trash" size={14} />
            </button>
          )}
          {!p.read_only && <Switch on={p.enabled} disabled={busy} onChange={(on) => void toggle(on)} label={`启用「${p.name}」`} />}
          <button
            type="button"
            title={open ? "收起" : "详情"}
            aria-label={open ? `收起「${p.name}」` : `「${p.name}」的详情`}
            aria-expanded={open}
            onClick={() => setOpen((o) => !o)}
            className="flex size-7 shrink-0 items-center justify-center rounded-[8px] text-ink-3 transition-colors hover:bg-hover hover:text-ink"
          >
            <AdminIcon name="chevron" size={14} className={cx("transition-transform duration-200", open && "rotate-180")} />
          </button>
        </div>
      </div>
      {p.error && <p className="mt-2 ml-12 text-[12px] leading-relaxed text-red">{p.error}</p>}
      {p.needs_setup && p.needs_setup.length > 0 && (
        <p className="mt-2 ml-12 text-[12px] leading-relaxed text-orange">还缺：{p.needs_setup.join("、")}（在 App 的「设置 › 插件」里填）</p>
      )}
      <Collapse open={open} lazy>
        <div className="mt-3 ml-12 grid gap-3">
          {p.permissions.length > 0 && <Permissions lines={p.permissions} title="它可以" />}
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-[12px]">
            {pluginRunNote(p) && <Meta label="现在">{pluginRunNote(p)}</Meta>}
            {contributes.length > 0 && <Meta label="提供">{contributes.map(([point, ids]) => `${pointLabel(point)}：${ids.join("、")}`).join("；")}</Meta>}
            {p.previous_version && <Meta label="上一个版本" mono>{p.previous_version}</Meta>}
            {p.origin && <Meta label="安装自" mono>{p.origin}</Meta>}
            {p.publisher && <Meta label="发布者">{p.publisher}</Meta>}
            {p.homepage && (
              <Meta label="主页">
                <a href={p.homepage} target="_blank" rel="noreferrer noopener" className="text-accent-ink hover:underline">
                  {p.homepage}
                </a>
              </Meta>
            )}
            {p.read_only && <Meta label="改动">写在 hub.json 里，只能去 hub.json 改</Meta>}
          </dl>
          {p.warnings?.map((w) => (
            <p key={w} className="flex items-start gap-1.5 text-[12px] leading-relaxed text-orange">
              <AdminIcon name="alert" size={13} className="mt-0.5 shrink-0" />
              {w}
            </p>
          ))}
        </div>
      </Collapse>
    </div>
  );
}

function Meta({ label, children, mono }: { label: string; children: React.ReactNode; mono?: boolean }) {
  return (
    <>
      <dt className="whitespace-nowrap text-ink-3">{label}</dt>
      <dd className={cx("min-w-0 break-all text-ink-2", mono && "font-mono text-[11.5px]")}>{children}</dd>
    </>
  );
}

export function Permissions({ lines, title, tone }: { lines: PermissionLine[]; title: string; tone?: "orange" }) {
  return (
    <div>
      <div className={cx("mb-1.5 text-[11.5px] font-medium", tone === "orange" ? "text-orange" : "text-ink-3")}>{title}</div>
      <ul className={cx("divide-y divide-line overflow-hidden rounded-control shadow-hairline", tone === "orange" ? "bg-orange-tint" : "bg-inset")}>
        {lines.map((l) => (
          <li key={l.key} className="flex items-start gap-2.5 px-3 py-2">
            <AdminIcon name={PERM_ICON[l.key] ?? "info"} size={14} className={cx("mt-0.5 shrink-0", tone === "orange" ? "text-orange" : "text-ink-2")} />
            <span className="min-w-0">
              <span className="block text-[12.5px] font-medium text-ink">{l.title}</span>
              {l.detail && <span className="mt-0.5 block text-[11.5px] leading-relaxed break-words text-ink-2">{l.detail}</span>}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

type Way = "path" | "url" | "remote" | "upload";

const WAYS: { value: Way; label: string; icon: AdminIconName }[] = [
  { value: "path", label: "hub 机器上", icon: "folder" },
  { value: "url", label: "链接", icon: "web" },
  { value: "upload", label: "上传", icon: "upload" },
  { value: "remote", label: "跑在别处", icon: "link" },
];

const WAY_FIELD: Record<Exclude<Way, "upload">, { label: string; placeholder: string; hint: string }> = {
  path: {
    label: "目录或包的路径",
    placeholder: "/opt/removed/example-notes",
    hint: "hub 机器上的目录（拷一份进来，原目录以后改了不影响已装的），或 .tar.gz / .zip 包。",
  },
  url: {
    label: "下载链接",
    placeholder: "https://example.com/example-notes-1.2.0.tar.gz#sha256=…",
    hint: "只收 https，链接后面必须带 #sha256=<64 位 hex>：hub 下载后校验，对不上就不装。",
  },
  remote: {
    label: "socket",
    placeholder: "remote:rpc=/run/example-notes.sock,http=/run/example-notes-http.sock",
    hint: "插件跑在别处、经 hub 机器上的 unix socket 接入：不拷文件，清单问插件要。",
  },
};

// sourceOf is what the hub is asked to preview for a typed value.
function sourceOf(way: Way, value: string): string {
  const v = value.trim();
  if (way === "remote" && v && !v.startsWith("remote:")) return `remote:${v}`;
  return v;
}

function InstallDialog({ open, upgrade, uploadMost, onClose, onDone }: { open: boolean; upgrade?: AdminPlugin; uploadMost: number; onClose: () => void; onDone: () => void }) {
  const [way, setWay] = useState<Way>("path");
  const [value, setValue] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [preview, setPreview] = useState<PluginPreview | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!open) return;
    setWay(devDialog() === "upload" ? "upload" : "path");
    setValue("");
    setFile(null);
    setPreview(null);
    setError("");
    // Dev only (screenshots): ?dialog=preview shows a preview straight away.
    if (devDialog() === "preview" || devDialog() === "upgrade") void previewPlugin("dir:/opt/removed/example-notes", upgrade?.id).then(setPreview, () => {});
  }, [open, upgrade?.id]);

  const typed = way === "upload" ? "" : sourceProblem(sourceOf(way, value));
  const fileProblem = way === "upload" && file ? uploadProblem(file, uploadMost) : "";
  const ready = way === "upload" ? Boolean(file) && !fileProblem : Boolean(value.trim()) && !typed;

  const look = async () => {
    setBusy(true);
    setError("");
    try {
      setPreview(way === "upload" && file ? await uploadPlugin(file, upgrade?.id) : await previewPlugin(sourceOf(way, value), upgrade?.id));
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  const agree = async () => {
    if (!preview) return;
    setBusy(true);
    setError("");
    try {
      const p = upgrade ? await upgradePlugin(upgrade.id, preview) : await installPlugin(preview);
      toast(upgrade ? `「${p.name}」已升级到 ${p.version}` : `已安装「${p.name}」`, "ok");
      onDone();
      onClose();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const title = upgrade ? `升级「${upgrade.name}」` : "安装插件";
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={title}
      width={540}
      footer={
        preview ? (
          <>
            <Button tone="quiet" onClick={() => (setPreview(null), setError(""))}>
              换一个
            </Button>
            <Button tone={preview.more_permissions ? "danger" : "primary"} busy={busy} onClick={() => void agree()}>
              {upgrade ? (preview.more_permissions ? "同意新权限并升级" : "升级") : "同意并安装"}
            </Button>
          </>
        ) : (
          <>
            <Button tone="quiet" onClick={onClose}>
              取消
            </Button>
            <Button tone="primary" busy={busy} disabled={!ready} onClick={() => void look()}>
              看看它能做什么
            </Button>
          </>
        )
      }
    >
      {preview ? (
        <PreviewBody pv={preview} />
      ) : (
        <div className="grid gap-3">
          {upgrade && (
            <p className="text-[12.5px] leading-relaxed text-ink-2">
              现在是 <span className="font-mono text-ink">{upgrade.version}</span>。选新版本的包，先看它能做什么，再决定升不升；配置和密钥留着。
            </p>
          )}
          <Segmented value={way} options={WAYS} onChange={(w) => (setWay(w), setError(""))} label="插件从哪来" />
          {way === "upload" ? (
            <div>
              <span className="mb-1 block text-[12px] font-medium text-ink-2">插件包</span>
              <div className="flex flex-wrap items-center gap-2 rounded-control bg-inset px-3 py-2.5 shadow-hairline">
                <Button onClick={() => input.current?.click()}>
                  <AdminIcon name="upload" size={13} />
                  选择文件
                </Button>
                <span className={cx("min-w-0 flex-1 truncate text-[12.5px]", file ? "text-ink" : "text-ink-3")}>{file ? `${file.name} · ${bytes(file.size)}` : "还没选"}</span>
                <input ref={input} type="file" accept=".tar.gz,.tgz,.zip,application/gzip,application/zip" hidden onChange={(e) => (setFile(e.target.files?.[0] ?? null), (e.target.value = ""))} />
              </div>
              <span className={cx("mt-1 block text-[11.5px] leading-relaxed", fileProblem ? "text-red" : "text-ink-3")}>
                {fileProblem || `.tar.gz、.tgz 或 .zip，最大 ${Math.floor(uploadMost / (1 << 20)) || 200} MB。上传后先放在 hub 的暂存目录里，10 分钟内不装就删掉。`}
              </span>
            </div>
          ) : (
            <Field key={way} label={WAY_FIELD[way].label} value={value} onChange={setValue} placeholder={WAY_FIELD[way].placeholder} mono autoFocus error={value.trim() ? typed : ""} hint={WAY_FIELD[way].hint} />
          )}
          {error && <p className="text-[12.5px] leading-relaxed text-red">{error}</p>}
        </div>
      )}
      {preview && error && <p className="mt-3 text-[12.5px] leading-relaxed text-red">{error}</p>}
    </Dialog>
  );
}

export function PreviewBody({ pv }: { pv: PluginPreview }) {
  const now = useNow(1000);
  const left = countdown(pv.expires_at, now);
  const m = pv.manifest;
  const contributes = Object.entries(m.contributes ?? {});
  return (
    <div className="grid gap-3">
      <div className="flex items-start gap-3">
        <span className="flex size-10 shrink-0 items-center justify-center rounded-[11px] bg-accent-tint text-accent-ink">
          <AdminIcon name="puzzle" size={18} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span className="text-[14px] font-semibold text-ink">{m.name}</span>
            {pv.upgrade ? (
              <span className="font-mono text-[12px] text-ink-2">
                {pv.from} → <span className="text-ink">{m.version}</span>
              </span>
            ) : (
              <span className="font-mono text-[12px] text-ink-2">{m.version}</span>
            )}
          </div>
          <div className="mt-0.5 text-[11.5px] text-ink-3">
            <span className="font-mono">{m.id}</span>
            {m.publisher && <> · {m.publisher}</>}
          </div>
          {m.description && <p className="mt-1.5 text-[12.5px] leading-relaxed text-ink-2">{m.description}</p>}
        </div>
      </div>
      {pv.upgrade && pv.more_permissions && (
        <Notice tone="red" icon="alert" title="新版本要的权限变多了">
          它比你当初同意的能做更多的事。看清下面新增的几项再决定；不同意就不升级，旧版本照常用。
        </Notice>
      )}
      {pv.upgrade && (pv.added_permissions?.length ?? 0) > 0 && <Permissions lines={pv.added_permissions!} title="新增" tone="orange" />}
      {pv.permissions.length > 0 ? (
        <Permissions lines={pv.permissions} title={pv.upgrade ? "新版本可以" : "它可以"} />
      ) : (
        <p className="text-[12.5px] text-ink-2">它只有清单，不运行程序、不要任何权限。</p>
      )}
      {contributes.length > 0 && (
        <p className="text-[12px] leading-relaxed text-ink-2">
          <span className="text-ink-3">提供：</span>
          {contributes.map(([point, items]) => `${pointLabel(point)} ${items.length} 项`).join("、")}
        </p>
      )}
      {pv.warnings?.map((w) => (
        <p key={w} className="flex items-start gap-1.5 text-[12px] leading-relaxed text-orange">
          <AdminIcon name="alert" size={13} className="mt-0.5 shrink-0" />
          {w}
        </p>
      ))}
      <p className="text-[11.5px] leading-relaxed text-ink-3">
        {left ? <>这份预览还有 <span className="font-mono text-ink-2">{left}</span> 有效，</> : "这份预览过期了，点「换一个」重新预览；"}
        装的就是你看到的这一份（按权限摘要核对）。
      </p>
    </div>
  );
}

function RollBackDialog({ plugin, onClose, onDone }: { plugin: AdminPlugin | null; onClose: () => void; onDone: (p: AdminPlugin) => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => setError(""), [plugin]);
  const go = async () => {
    if (!plugin) return;
    setBusy(true);
    try {
      const p = await rollbackPlugin(plugin.id);
      toast(`「${p.name}」回到了 ${p.version}`, "ok");
      onDone(p);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={Boolean(plugin)}
      onClose={onClose}
      title={plugin ? `回滚「${plugin.name}」` : ""}
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>
            取消
          </Button>
          <Button tone="primary" busy={busy} onClick={() => void go()}>
            回到 {plugin?.previous_version}
          </Button>
        </>
      }
    >
      <p className="text-[12.5px] leading-relaxed text-ink-2">
        从 <span className="font-mono text-ink">{plugin?.version}</span> 回到上一个版本 <span className="font-mono text-ink">{plugin?.previous_version}</span>。只能回一步；配置和密钥留着，权限回到旧版本的那一份。
      </p>
      {error && <p className="mt-2 text-[12.5px] text-red">{error}</p>}
    </Dialog>
  );
}

function UninstallDialog({ plugin, onClose, onDone }: { plugin: AdminPlugin | null; onClose: () => void; onDone: () => void }) {
  const [keep, setKeep] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => (setKeep(false), setError("")), [plugin]);
  const go = async () => {
    if (!plugin) return;
    setBusy(true);
    try {
      await uninstallPlugin(plugin.id, keep);
      toast(`已卸载「${plugin.name}」`, "ok");
      onDone();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={Boolean(plugin)}
      onClose={onClose}
      title={plugin ? `卸载「${plugin.name}」` : ""}
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>
            取消
          </Button>
          <Button tone="danger" busy={busy} onClick={() => void go()}>
            卸载
          </Button>
        </>
      }
    >
      <p className="text-[12.5px] leading-relaxed text-ink-2">
        停掉它的程序，删掉它的包、配置和密钥；它提供的连接器从收件箱、知识库和助手里拿掉。要再用得重新安装。
      </p>
      <label className="mt-3 flex cursor-pointer items-start gap-2.5 rounded-control bg-inset px-3 py-2.5 shadow-hairline">
        <input type="checkbox" checked={keep} onChange={(e) => setKeep(e.target.checked)} className="mt-0.5 size-3.5 shrink-0 accent-[var(--accent)]" />
        <span className="min-w-0">
          <span className="block text-[12.5px] font-medium text-ink">保留它的数据</span>
          <span className="mt-0.5 block text-[11.5px] leading-relaxed break-all text-ink-3">
            <span className="font-mono">~/.persona/plugins/data/{plugin?.id}/</span> 留着，以后重装能接着用。
          </span>
        </span>
      </label>
      {error && <p className="mt-2 text-[12.5px] text-red">{error}</p>}
    </Dialog>
  );
}
