// 设置 › 扩展 › 扩展市场 (docs/extensions.md「扩展市场」): the catalog of the markets this hub reads (search, a market at a
// time), one extension of it (what it may do; preview → install / upgrade; uninstall when this market installed it), and
// the markets themselves (add with an optional write-only token, remove, read again, change the token). Logic in
// lib/extension-market.ts.
import { useEffect, useRef, useState } from "react";
import { errorText } from "../../lib/api";
import { ago } from "../../lib/format";
import {
  addMarket, agreeLabel, canUninstall, getCatalog, installPreview, isGitMarket, itemLine, itemState, listMarkets, MARKET_SPEC_HINT, MARKET_SPEC_PLACEHOLDER, marketKind, marketLine, previewFromMarket,
  previewVersion, refreshMarket, removeMarket, setMarketToken, specLabel, uninstallExtension, upgradePreview,
  type Catalog, type CatalogItem, type Market, type MarketPreview,
} from "../../lib/extension-market";
import { extensionUpgrades, pointText, setExtensionUpgrades, type ExtensionPermission } from "../../lib/extensions";
import { Button, cx, Dialog, EASE, IconButton, Loading, Spinner, toast, useNow } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Group, Pill, Row, Switch } from "./SettingsParts";

const inputClass =
  "h-8 w-full rounded-control bg-field px-2.5 text-[12.5px] text-ink shadow-inset-field outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)] disabled:opacity-60";

function Back({ label, onBack, right }: { label: string; onBack: () => void; right?: React.ReactNode }) {
  return (
    <div className="mb-3 flex items-center gap-2">
      <Button tone="quiet" icon="chevronLeft" onClick={onBack} className="-ml-2">
        {label}
      </Button>
      <span className="flex-1" />
      {right}
    </div>
  );
}

function Problem({ text, onRetry, busy }: { text: string; onRetry: () => void; busy?: boolean }) {
  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <p className="min-w-0 flex-1 text-[12.5px] break-words text-red">{text}</p>
      <Button tone="secondary" icon="retry" onClick={onRetry} busy={busy}>
        重试
      </Button>
    </div>
  );
}

const STATE_TONE = { accent: "accent", green: "green", orange: "orange", red: "red", gray: undefined } as const;

// ── the catalog ─────────────────────────────────────────

export function MarketCatalog({ onBack, onOpen, onMarkets }: { onBack: () => void; onOpen: (it: CatalogItem) => void; onMarkets: () => void }) {
  const [q, setQ] = useState("");
  const [market, setMarket] = useState("");
  const [data, setData] = useState<Catalog | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const seq = useRef(0);
  const load = (refresh = false) => {
    const n = ++seq.current;
    setLoading(true);
    getCatalog(q, market, refresh)
      .then((c) => n === seq.current && (setData(c), setError("")))
      .catch((e) => n === seq.current && setError(errorText(e)))
      .finally(() => n === seq.current && setLoading(false));
  };
  // typing waits a moment; picking a market reads at once
  useEffect(() => {
    const t = setTimeout(() => load(), q ? 250 : 0);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q, market]);

  const markets = data?.markets ?? [];
  const broken = markets.filter((m) => m.error);
  return (
    <>
      <Back
        label="扩展"
        onBack={onBack}
        right={
          <>
            <IconButton icon="retry" label="重新读取所有市场" onClick={() => load(true)} disabled={loading} />
            <Button tone="secondary" icon="gear" onClick={onMarkets}>
              管理市场
            </Button>
          </>
        }
      />
      <h2 className="mb-1 px-1 text-[15px] font-semibold text-ink">扩展市场</h2>
      <p className="mb-4 px-1 text-[12.5px] leading-relaxed text-ink-3">这台 hub 登记的市场里的扩展。装之前先看它能做什么，同意了才装；包按市场写的 sha256 校验。</p>

      <div className="mb-3 flex items-center gap-2 rounded-control bg-surface px-2.5 shadow-card">
        <Icon name="search" size={14} className="shrink-0 text-ink-3" />
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="搜索名字、说明、发布者、标签"
          aria-label="搜索扩展"
          spellCheck={false}
          className="h-9 min-w-0 flex-1 bg-transparent text-[13px] text-ink outline-none placeholder:text-ink-3"
        />
        {loading && data && <Spinner />}
        {q && <IconButton icon="x" label="清除搜索" onClick={() => setQ("")} size={24} iconSize={12} />}
      </div>
      {markets.length > 1 && (
        <div role="radiogroup" aria-label="按市场筛选" className="mb-4 flex flex-wrap gap-1.5">
          {[{ id: "", name: "全部" }, ...markets].map((m) => {
            const on = market === m.id;
            return (
              <button
                key={m.id || "all"}
                type="button"
                role="radio"
                aria-checked={on}
                onClick={() => setMarket(m.id)}
                className={cx(
                  "flex h-7 items-center whitespace-nowrap rounded-full px-3 text-[12px] transition-[background-color,box-shadow,color] duration-150",
                  on ? "bg-accent-tint font-medium text-accent-ink shadow-[inset_0_0_0_1px_var(--accent)]" : "text-ink-2 shadow-hairline hover:bg-hover",
                )}
              >
                {m.name}
              </button>
            );
          })}
        </div>
      )}
      {broken.map((m) => (
        <p key={m.id} className="mb-3 flex items-start gap-2 rounded-[8px] bg-red-tint px-3 py-2 text-[12px] leading-relaxed break-words text-red">
          <Icon name="info" size={13} className="mt-0.5 shrink-0" />
          读不到「{m.name}」：{m.error}
        </p>
      ))}

      <Group title={data ? `${data.extensions.length} 个扩展` : "扩展"}>
        {!data && !error && (
          <div className="px-4 py-3">
            <Loading />
          </div>
        )}
        {error && <Problem text={`读不到扩展市场：${error}`} onRetry={() => load()} busy={loading} />}
        {data && markets.length === 0 && (
          <div className="flex flex-col items-center gap-2 px-6 py-8 text-center">
            <span className="flex size-10 items-center justify-center rounded-[10px] bg-inset text-ink-3 shadow-hairline">
              <Icon name="extension" size={18} />
            </span>
            <p className="text-[13px] font-medium text-ink">还没有市场</p>
            <p className="max-w-[420px] text-[12px] leading-relaxed text-ink-3">市场是一个网站或 GitHub 仓库发布的扩展索引（persona-extensions.json）。添加一个，就能在这里浏览和安装它的扩展。</p>
            <Button tone="primary" icon="plus" onClick={onMarkets}>
              添加市场
            </Button>
          </div>
        )}
        {data && markets.length > 0 && data.extensions.length === 0 && (
          <p className="px-4 py-6 text-center text-[12.5px] text-ink-3">{q ? `没有找到「${q.trim()}」` : "这个市场还没有扩展"}</p>
        )}
        {data?.extensions.map((it, i) => {
          const st = itemState(it);
          return (
            <button
              key={`${it.market}/${it.id}`}
              type="button"
              onClick={() => onOpen(it)}
              className="flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-hover"
              style={{ animation: `fade-up 320ms ${EASE} ${Math.min(i, 8) * 30}ms both` }}
            >
              <span className={cx("flex size-9 shrink-0 items-center justify-center rounded-[9px]", it.installed ? "bg-accent-tint text-accent-ink" : "bg-inset text-ink-3 shadow-hairline")}>
                <Icon name="extension" size={16} />
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="truncate text-[13px] font-medium text-ink">{it.name}</span>
                  <span className="shrink-0 font-mono text-[11px] text-ink-3">{it.version}</span>
                </div>
                {it.description && <div className="mt-0.5 truncate text-[11.5px] text-ink-3">{it.description}</div>}
                <div className="mt-1 flex min-w-0 flex-wrap items-center gap-1">
                  <span className="mr-1 truncate text-[11px] text-ink-3">{it.market_name}</span>
                  {(it.tags ?? []).map((t) => (
                    <Pill key={t}>{t}</Pill>
                  ))}
                </div>
              </div>
              <span className="flex shrink-0 flex-col items-end gap-1">
                <Pill tone={STATE_TONE[st.tone]}>{st.label}</Pill>
                {it.problem && <span className="max-w-[180px] truncate text-[11px] text-orange">{it.problem}</span>}
              </span>
              <Icon name="chevronRight" size={12} stroke={2} className="shrink-0 text-ink-3" />
            </button>
          );
        })}
      </Group>
    </>
  );
}

// ── one extension of a market ──────────────────────────────

export function MarketItem({ market, id, onBack, onInstalled }: { market: string; id: string; onBack: () => void; onInstalled: (id: string) => void }) {
  const [it, setIt] = useState<CatalogItem | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [preview, setPreview] = useState<MarketPreview | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const [removing, setRemoving] = useState(false);
  const load = () => {
    setLoading(true);
    getCatalog("", market)
      .then((c) => {
        const found = c.extensions.find((x) => x.id === id) ?? null;
        setIt(found);
        setError(found ? "" : "这个市场里没有这个扩展了");
      })
      .catch((e) => setError(errorText(e)))
      .finally(() => setLoading(false));
  };
  useEffect(load, [market, id]);

  const look = async () => {
    setPreviewing(true);
    try {
      setPreview(await previewFromMarket(market, id));
    } catch (e) {
      toast(`看不了：${errorText(e)}`, "error");
    } finally {
      setPreviewing(false);
    }
  };
  const st = it ? itemState(it) : null;
  const contributes = Object.entries(it?.contributes ?? {});
  let home = "";
  try {
    home = it?.homepage && new URL(it.homepage).protocol === "https:" ? it.homepage : "";
  } catch {
    home = "";
  }

  return (
    <>
      <Back label="扩展市场" onBack={onBack} right={<IconButton icon="retry" label="重新读取" onClick={load} disabled={loading} />} />
      <section className="mb-6 overflow-hidden rounded-card bg-surface shadow-card">
        <div className="flex items-center gap-3 px-4 py-3.5">
          <span className={cx("flex size-11 shrink-0 items-center justify-center rounded-[11px]", it?.installed ? "bg-accent-tint text-accent-ink" : "bg-inset text-ink-3 shadow-hairline")}>
            <Icon name="extension" size={20} />
          </span>
          <div className="min-w-0 flex-1">
            <h2 className="truncate text-[15px] font-semibold text-ink">{it?.name ?? id}</h2>
            <div className="mt-0.5 truncate text-[12px] text-ink-3">{it ? itemLine(it) : loading ? "正在读取…" : ""}</div>
          </div>
          {loading && !it && <Spinner />}
          {st && <Pill tone={STATE_TONE[st.tone]}>{st.label}</Pill>}
        </div>
        {(it?.description || it?.tags?.length || it?.problem || error) && (
          <div className="flex flex-col gap-2 border-t border-line px-4 py-3">
            {it?.description && <p className="text-[12.5px] leading-relaxed text-ink-2">{it.description}</p>}
            {!!it?.tags?.length && (
              <div className="flex flex-wrap gap-1">
                {it.tags.map((t) => (
                  <Pill key={t}>{t}</Pill>
                ))}
              </div>
            )}
            {it?.problem && <p className="text-[12px] text-orange">装不了：{it.problem}</p>}
            {error && <p className="text-[12px] break-words text-red">{error}</p>}
          </div>
        )}
        {it && st && (st.kind === "install" || st.kind === "upgrade" || canUninstall(it)) && (
          <div className="flex flex-wrap items-center gap-2 border-t border-line bg-inset px-4 py-2.5">
            {canUninstall(it) && (
              <Button tone="quiet" icon="trash" onClick={() => setRemoving(true)} className="text-red enabled:hover:text-red">
                卸载
              </Button>
            )}
            <span className="flex-1" />
            {(st.kind === "install" || st.kind === "upgrade") && (
              <Button tone="primary" icon={st.kind === "upgrade" ? "up" : "download"} busy={previewing} onClick={() => void look()}>
                {st.kind === "upgrade" ? `升级到 ${it.version}` : "安装"}
              </Button>
            )}
          </div>
        )}
      </section>

      {it && (
        <>
          <Group title="它可以" footer="装之前还会把这几项再列一遍，点「同意并安装」才装；以后的版本要得更多时，升级前会再问一次。">
            {(it.permissions ?? []).length ? (
              (it.permissions ?? []).map((p) => <PermissionRow key={p.key + p.title} p={p} />)
            ) : (
              <p className="px-4 py-2.5 text-[12.5px] text-ink-2">它只有清单，不运行程序、不要任何权限。</p>
            )}
          </Group>
          {contributes.length > 0 && (
            <Group title="提供">
              {contributes.map(([point, ids]) => (
                <Row key={point} label={pointText(point)} mono>
                  <span className="truncate">{ids.join("、")}</span>
                </Row>
              ))}
            </Group>
          )}
          <Group title="关于">
            <Row label="版本" mono>
              {it.version}
            </Row>
            {it.installed && (
              <Row label="已装的版本" mono>
                {it.installed}
              </Row>
            )}
            {it.publisher && <Row label="发布者">{it.publisher}</Row>}
            <Row label="市场">{it.market_name}</Row>
            <Row label="id" mono>
              {it.id}
            </Row>
            {home && (
              <a href={home} target="_blank" rel="noreferrer noopener" className="flex items-center gap-3 px-4 py-2.5 whitespace-nowrap transition-colors hover:bg-hover">
                <span className="min-w-0 flex-1 truncate text-[13px] text-ink">主页</span>
                <span className="min-w-0 truncate text-[12.5px] text-ink-3">{new URL(home).host}</span>
                <Icon name="external" size={12} className="shrink-0 text-ink-3" />
              </a>
            )}
          </Group>
        </>
      )}

      {preview && it && (
        <PreviewDialog
          pv={preview}
          onClose={() => setPreview(null)}
          onAgree={async () => {
            const d = preview.upgrade ? await upgradePreview(it.id, preview) : await installPreview(preview);
            toast(preview.upgrade ? `「${d.name}」已升级到 ${d.version}` : `已安装「${d.name}」`, "ok");
            setPreview(null);
            onInstalled(d.id);
          }}
        />
      )}
      {removing && it && (
        <UninstallDialog
          name={it.name}
          id={it.id}
          onClose={() => setRemoving(false)}
          onDone={() => {
            setRemoving(false);
            load();
          }}
        />
      )}
    </>
  );
}

export function PermissionRow({ p, tone }: { p: ExtensionPermission; tone?: "orange" }) {
  return (
    <div className="flex items-start gap-2.5 px-4 py-2.5">
      <Icon name={permissionIcon(p.key)} size={14} className={cx("mt-0.5 shrink-0", tone === "orange" ? "text-orange" : "text-ink-3")} />
      <div className="min-w-0">
        <div className="text-[13px] text-ink">{p.title}</div>
        {p.detail && <div className="mt-0.5 text-[11.5px] leading-snug break-words text-ink-3">{p.detail}</div>}
      </div>
    </div>
  );
}

/** The preview of what installing (or upgrading) does; agreeing installs exactly this (its permissions digest). */
export function PreviewDialog({ pv, onClose, onAgree }: { pv: MarketPreview; onClose: () => void; onAgree: () => Promise<void> }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const now = useNow(1000);
  const left = Math.max(0, Date.parse(pv.expires_at) - now);
  const expired = Number.isFinite(left) && left <= 0;
  const agree = async () => {
    setBusy(true);
    setError("");
    try {
      await onAgree();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  const m = pv.manifest;
  return (
    <Dialog
      open
      onClose={() => !busy && onClose()}
      width={520}
      title={pv.upgrade ? `升级「${m.name}」` : `安装「${m.name}」`}
      footer={
        <>
          <Button tone="quiet" onClick={onClose} disabled={busy}>
            取消
          </Button>
          <Button tone={pv.more_permissions ? "danger" : "primary"} busy={busy} disabled={expired} onClick={() => void agree()}>
            {agreeLabel(pv)}
          </Button>
        </>
      }
    >
      <div className="flex max-h-[60vh] flex-col gap-3 overflow-y-auto">
        <div className="flex items-start gap-3">
          <span className="flex size-10 shrink-0 items-center justify-center rounded-[11px] bg-accent-tint text-accent-ink">
            <Icon name="extension" size={18} />
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-baseline gap-x-2">
              <span className="text-[14px] font-semibold text-ink">{m.name}</span>
              <span className="font-mono text-[12px] text-ink-2">{previewVersion(pv)}</span>
            </div>
            <div className="mt-0.5 text-[11.5px] text-ink-3">
              <span className="font-mono">{m.id}</span>
              {m.publisher && <> · {m.publisher}</>}
            </div>
            {m.description && <p className="mt-1.5 text-[12.5px] leading-relaxed text-ink-2">{m.description}</p>}
          </div>
        </div>
        {pv.upgrade && pv.more_permissions && (
          <p className="rounded-[8px] bg-red-tint px-3 py-2 text-[12px] leading-relaxed text-red">新版本要的权限变多了：它比你当初同意的能做更多的事。看清新增的几项再决定；不同意就不升级，旧版本照常用。</p>
        )}
        {pv.upgrade && !!pv.added_permissions?.length && (
          <div>
            <div className="mb-1 px-1 text-[11.5px] font-medium text-orange">新增</div>
            <div className="divide-y divide-line overflow-hidden rounded-control bg-orange-tint shadow-hairline">
              {pv.added_permissions.map((p) => (
                <PermissionRow key={p.key + p.title} p={p} tone="orange" />
              ))}
            </div>
          </div>
        )}
        <div>
          <div className="mb-1 px-1 text-[11.5px] font-medium text-ink-3">{pv.upgrade ? "新版本可以" : "它可以"}</div>
          <div className="divide-y divide-line overflow-hidden rounded-control bg-inset shadow-hairline">
            {pv.permissions.length ? pv.permissions.map((p) => <PermissionRow key={p.key + p.title} p={p} />) : <p className="px-4 py-2.5 text-[12.5px] text-ink-2">它只有清单，不运行程序、不要任何权限。</p>}
          </div>
        </div>
        {pv.warnings?.map((w) => (
          <p key={w} className="flex items-start gap-1.5 text-[12px] leading-relaxed text-orange">
            <Icon name="info" size={13} className="mt-0.5 shrink-0" />
            {w}
          </p>
        ))}
        <p className="text-[11.5px] leading-relaxed text-ink-3">
          {expired ? "这份预览过期了，关掉再点一次安装。" : `这份预览 ${Math.ceil(left / 60000)} 分钟内有效；装的就是你看到的这一份（按权限摘要核对）。`}
        </p>
        {error && <p className="rounded-[8px] bg-red-tint px-3 py-2 text-[12px] break-words text-red">没有完成：{error}</p>}
      </div>
    </Dialog>
  );
}

/** Uninstalling an extension a market installed; its data can stay for a later install. */
export function UninstallDialog({ id, name, onClose, onDone }: { id: string; name: string; onClose: () => void; onDone: () => void }) {
  const [keep, setKeep] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const go = async () => {
    setBusy(true);
    setError("");
    try {
      await uninstallExtension(id, keep);
      toast(`已卸载「${name}」`, "ok");
      onDone();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open
      onClose={() => !busy && onClose()}
      width={440}
      title={`卸载「${name}」`}
      footer={
        <>
          <Button tone="quiet" onClick={onClose} disabled={busy}>
            取消
          </Button>
          <Button tone="danger" busy={busy} onClick={() => void go()}>
            卸载
          </Button>
        </>
      }
    >
      <p className="text-[12.5px] leading-relaxed text-ink-2">停掉它的程序，删掉它的包、配置和密钥；它提供的连接器等一起拿掉。要再用得从市场重新安装。</p>
      <label className="mt-3 flex cursor-pointer items-start gap-2.5 rounded-control bg-inset px-3 py-2.5 shadow-hairline">
        <input type="checkbox" checked={keep} onChange={(e) => setKeep(e.target.checked)} className="mt-0.5 size-3.5 shrink-0 accent-[var(--accent)]" />
        <span className="min-w-0">
          <span className="block text-[12.5px] font-medium text-ink">保留它的数据</span>
          <span className="mt-0.5 block text-[11.5px] leading-relaxed text-ink-3">以后重装能接着用。</span>
        </span>
      </label>
      {error && <p className="mt-2 rounded-[8px] bg-red-tint px-3 py-2 text-[12px] break-words text-red">没有卸载：{error}</p>}
    </Dialog>
  );
}

// ── the markets ─────────────────────────────────────────

export function Markets({ onBack }: { onBack: () => void }) {
  const [list, setList] = useState<Market[] | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [adding, setAdding] = useState(false);
  const [tokenFor, setTokenFor] = useState<Market | null>(null);
  const [busy, setBusy] = useState<Record<string, "refresh" | "remove">>({});
  const [confirm, setConfirm] = useState("");
  // The switch for all market extensions (docs/extensions.md「自动升级」); null: a hub without it.
  const [auto, setAuto] = useState<boolean | null>(null);
  const [savingAuto, setSavingAuto] = useState(false);
  useEffect(() => void extensionUpgrades().then(setAuto), []);
  const flipAuto = async (on: boolean) => {
    setSavingAuto(true);
    try {
      setAuto(await setExtensionUpgrades(on));
      toast(on ? "已打开扩展自动升级" : "已关掉扩展自动升级", "ok");
    } catch (e) {
      toast(`没有改：${errorText(e)}`, "error");
    } finally {
      setSavingAuto(false);
    }
  };
  const load = () => {
    setLoading(true);
    listMarkets()
      .then((l) => (setList(l), setError("")))
      .catch((e) => setError(errorText(e)))
      .finally(() => setLoading(false));
  };
  useEffect(load, []);
  const put = (m: Market) => setList((l) => (l ?? []).map((x) => (x.id === m.id ? m : x)));
  const work = async (m: Market, kind: "refresh" | "remove") => {
    setBusy((b) => ({ ...b, [m.id]: kind }));
    try {
      if (kind === "refresh") {
        put(await refreshMarket(m.id));
        toast(`已重新读取「${m.name}」`, "ok");
      } else {
        await removeMarket(m.id);
        setList((l) => (l ?? []).filter((x) => x.id !== m.id));
        toast(`已删除「${m.name}」，从它装的扩展留着`, "ok");
      }
    } catch (e) {
      toast(`${kind === "refresh" ? "读不到" : "没有删除"}「${m.name}」：${errorText(e)}`, "error");
      if (kind === "refresh") load();
    } finally {
      setBusy(({ [m.id]: _, ...rest }) => rest);
      setConfirm("");
    }
  };

  return (
    <>
      <Back
        label="扩展市场"
        onBack={onBack}
        right={
          <Button tone="primary" icon="plus" onClick={() => setAdding(true)}>
            添加市场
          </Button>
        }
      />
      <h2 className="mb-1 px-1 text-[15px] font-semibold text-ink">管理市场</h2>
      <p className="mb-4 px-1 text-[12.5px] leading-relaxed text-ink-3">
        hub 读这些市场的索引（persona-extensions.json）。私有的 https / GitHub 市场填一个只读 token：加密存在 hub 上，只发给这个市场自己，填了以后不再显示；git: 的市场用 hub 机器自己的 SSH 凭据。
      </p>
      {auto !== null && !!list?.length && (
        <Group
          footer={
            auto
              ? "从这些市场装的扩展出了新版本，扩展空闲时 hub 自动装上；要的权限变多时先问你。每个扩展也可以在自己的页面里单独关掉。"
              : "关着：有新版本时只在扩展的页面里提示，你自己去升级。"
          }
        >
          <div className="flex items-center gap-3 px-4 py-2.5">
            <span className="min-w-0 flex-1 truncate text-[13px] text-ink">自动升级扩展</span>
            {savingAuto && <Spinner />}
            <Switch on={auto} onChange={(on) => void flipAuto(on)} disabled={savingAuto} label="自动升级扩展" />
          </div>
        </Group>
      )}
      <Group title={list ? `${list.length} 个市场` : "市场"} right={<IconButton icon="retry" label="刷新" onClick={load} disabled={loading} size={24} iconSize={13} className="-mb-1" />}>
        {!list && !error && (
          <div className="px-4 py-3">
            <Loading />
          </div>
        )}
        {error && <Problem text={`读不到市场：${error}`} onRetry={load} busy={loading} />}
        {list?.length === 0 && <p className="px-4 py-6 text-center text-[12.5px] text-ink-3">还没有市场。点右上角「添加市场」，填 https 地址、github:owner/repo 或 git:…。</p>}
        {list?.map((m) => (
          <div key={m.id} className="px-4 py-3">
            <div className="flex items-start gap-3">
              <span className={cx("flex size-9 shrink-0 items-center justify-center rounded-[9px]", m.error ? "bg-red-tint text-red" : "bg-inset text-ink-3 shadow-hairline")}>
                <Icon name={marketKind(m.spec) === "web" ? "web" : "git"} size={16} />
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="truncate text-[13px] font-medium text-ink">{m.name}</span>
                  {m.has_token && (
                    <Pill>
                      <Icon name="lock" size={10} className="mr-0.5" />
                      token
                    </Pill>
                  )}
                </div>
                <div className="mt-0.5 truncate font-mono text-[11px] text-ink-3" title={m.spec}>
                  {specLabel(m.spec)}
                </div>
                <div className="mt-0.5 truncate text-[11.5px] text-ink-3">{marketLine(m, (iso) => ago(iso))}</div>
                {m.description && <p className="mt-1 text-[12px] leading-relaxed text-ink-2">{m.description}</p>}
                {m.error && <p className="mt-1 text-[12px] leading-relaxed break-words text-red">读不到：{m.error}</p>}
                {(m.warnings ?? []).map((w) => (
                  <p key={w} className="mt-1 flex items-start gap-1.5 text-[11.5px] leading-snug break-words text-orange">
                    <Icon name="info" size={12} className="mt-0.5 shrink-0" />
                    {w}
                  </p>
                ))}
              </div>
            </div>
            <div className="mt-2 ml-12 flex flex-wrap items-center gap-1.5">
              <Button tone="secondary" icon="retry" busy={busy[m.id] === "refresh"} disabled={!!busy[m.id]} onClick={() => void work(m, "refresh")}>
                重新读取
              </Button>
              {!isGitMarket(m.spec) && (
                <Button tone="secondary" icon="key" disabled={!!busy[m.id]} onClick={() => setTokenFor(m)}>
                  {m.has_token ? "换 token" : "填 token"}
                </Button>
              )}
              <span className="flex-1" />
              {confirm === m.id ? (
                <span className="flex items-center gap-1" style={{ animation: `pop-in 160ms ${EASE} both` }}>
                  <Button tone="quiet" onClick={() => setConfirm("")}>
                    取消
                  </Button>
                  <Button tone="danger" busy={busy[m.id] === "remove"} onClick={() => void work(m, "remove")}>
                    确认删除
                  </Button>
                </span>
              ) : (
                <Button tone="quiet" icon="trash" disabled={!!busy[m.id]} onClick={() => setConfirm(m.id)} className="text-red enabled:hover:text-red">
                  删除
                </Button>
              )}
            </div>
          </div>
        ))}
      </Group>
      {adding && (
        <AddMarketDialog
          onClose={() => setAdding(false)}
          onAdded={(m) => {
            setList((l) => [...(l ?? []).filter((x) => x.id !== m.id), m]);
            setAdding(false);
            toast(`已添加「${m.name}」`, "ok");
          }}
        />
      )}
      {tokenFor && (
        <TokenDialog
          market={tokenFor}
          onClose={() => setTokenFor(null)}
          onDone={(m) => {
            put(m);
            setTokenFor(null);
          }}
        />
      )}
    </>
  );
}

function AddMarketDialog({ onClose, onAdded }: { onClose: () => void; onAdded: (m: Market) => void }) {
  const [spec, setSpec] = useState("");
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const git = isGitMarket(spec);
  const go = async () => {
    if (!spec.trim()) return;
    setBusy(true);
    setError("");
    try {
      onAdded(await addMarket(spec.trim(), git ? undefined : token.trim() || undefined));
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open
      onClose={() => !busy && onClose()}
      width={480}
      title="添加市场"
      footer={
        <>
          <Button tone="quiet" onClick={onClose} disabled={busy}>
            取消
          </Button>
          <Button tone="primary" busy={busy} disabled={!spec.trim()} onClick={() => void go()}>
            添加
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          void go();
        }}
      >
        <label className="block">
          <span className="mb-1.5 block text-[12px] font-medium text-ink-2">地址</span>
          <input
            value={spec}
            onChange={(e) => setSpec(e.target.value)}
            autoFocus
            placeholder={MARKET_SPEC_PLACEHOLDER}
            spellCheck={false}
            autoComplete="off"
            autoCapitalize="off"
            disabled={busy}
            className={cx(inputClass, "h-9 font-mono")}
          />
          <span className="mt-1 block text-[11.5px] leading-relaxed text-ink-3">{MARKET_SPEC_HINT}</span>
        </label>
        <label className="block">
          <span className="mb-1.5 block text-[12px] font-medium text-ink-2">token（可选）</span>
          <input
            type="password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder={git ? "git: 的市场不用 token" : "私有的市场才要"}
            spellCheck={false}
            autoComplete="new-password"
            disabled={busy || git}
            className={cx(inputClass, "h-9 font-mono")}
          />
          <span className="mt-1 block text-[11.5px] leading-relaxed text-ink-3">
            {git ? "git: 的市场用 hub 机器自己的 SSH 凭据读，不收 token。" : "GitHub 用只读的 fine-grained token（这个仓库的 Contents 读权限）。加密存在 hub 上，填了以后不再显示。"}
          </span>
        </label>
        {busy && <p className="text-[12px] text-ink-3">正在读一次它的索引，读到了才登记…</p>}
        {error && <p className="rounded-[8px] bg-red-tint px-3 py-2 text-[12px] leading-relaxed break-words text-red">没有添加：{error}</p>}
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}

function TokenDialog({ market, onClose, onDone }: { market: Market; onClose: () => void; onDone: (m: Market) => void }) {
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState<"" | "save" | "clear">("");
  const [error, setError] = useState("");
  const run = async (kind: "save" | "clear") => {
    if (kind === "save" && !token.trim()) return;
    setBusy(kind);
    setError("");
    try {
      const m = await setMarketToken(market.id, kind === "save" ? token.trim() : "");
      toast(kind === "save" ? `已换上新的 token，重新读了「${m.name}」` : `已去掉「${m.name}」的 token`, "ok");
      onDone(m);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy("");
    }
  };
  return (
    <Dialog
      open
      onClose={() => !busy && onClose()}
      width={440}
      title={`「${market.name}」的 token`}
      footer={
        <>
          {market.has_token && (
            <span className="mr-auto">
              <Button tone="quiet" icon="trash" busy={busy === "clear"} disabled={!!busy} onClick={() => void run("clear")} className="text-red enabled:hover:text-red">
                去掉 token
              </Button>
            </span>
          )}
          <Button tone="quiet" onClick={onClose} disabled={!!busy}>
            取消
          </Button>
          <Button tone="primary" busy={busy === "save"} disabled={!token.trim() || !!busy} onClick={() => void run("save")}>
            保存
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          void run("save");
        }}
      >
        <div className="flex items-center gap-2 text-[12px] text-ink-3">
          <Icon name="lock" size={13} className="shrink-0" />
          {market.has_token ? "已设置（不显示）" : "未设置"}
        </div>
        <input
          type="password"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          autoFocus
          autoComplete="new-password"
          spellCheck={false}
          placeholder={market.has_token ? "输入新的 token 替换" : "输入 token"}
          aria-label="token"
          disabled={!!busy}
          className={cx(inputClass, "h-9 font-mono")}
        />
        <p className="text-[11.5px] leading-relaxed text-ink-3">保存后 hub 用它重新读一次索引。</p>
        {error && <p className="rounded-[8px] bg-red-tint px-3 py-2 text-[12px] break-words text-red">没有保存：{error}</p>}
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}

/** A permission line's icon: what it is about (its program on computers' lines like the hub's). */
function permissionIcon(key: string) {
  switch (key) {
    case "network":
    case "node_network":
      return "web";
    case "secrets":
    case "launcher_keys":
    case "node_secrets":
      return "key";
    case "hub_api":
      return "hub";
    case "runs_code":
    case "node_runs_code":
      return "terminal";
    case "node_files":
      return "folder";
    default:
      return "info";
  }
}

