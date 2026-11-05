// 扩展 › 扩展市场 (docs/admin.md「扩展」, docs/extensions.md「扩展市场」): the markets this hub reads (add with an optional
// write-only token, read again, change the token, remove) and their catalog (search, a market at a time). Installing
// or upgrading from it previews first (POST extensions/-/catalog/preview) and goes through the page's own install /
// upgrade with that preview's id and permissions digest: nothing installs before the user agreed to exactly that.
import { useEffect, useRef, useState } from "react";
import { previewBlocked } from "../../lib/extension-model";
import { ago } from "../../lib/format";
import { canUninstall, isGitMarket, itemState, MARKET_SPEC_HINT, MARKET_SPEC_PLACEHOLDER, marketKind, marketLine, specLabel, type CatalogItem, type Catalog, type Market } from "../../lib/extension-market";
import { Button, cx, Dialog, IconButton, toast, useNow } from "../../ui/atoms";
import { addMarket, AdminError, errorText, getCatalog, getMarkets, installExtension, previewFromMarket, refreshMarket, removeMarket, setMarketToken, upgradeExtension } from "../api";
import { AdminIcon } from "../icons";
import { devDialog } from "../router";
import type { ExtensionPreview } from "../types";
import { Card, ConfirmButton, EmptyState, ErrorBox, Field, Loader, Segmented, Tag } from "../parts";
import { PreviewBody } from "./Extensions";

/** The markets and their catalog; `onChanged` when an extension was installed or upgraded (the list above reloads). */
export function ExtensionMarkets({ onChanged }: { onChanged: () => void }) {
  const [markets, setMarkets] = useState<Market[] | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [gone, setGone] = useState(false);
  const [adding, setAdding] = useState(() => devDialog() === "market-add");
  const [tokenFor, setTokenFor] = useState<Market | null>(null);
  const [refreshing, setRefreshing] = useState("");
  const [catalogKey, setCatalogKey] = useState(0);

  const load = () =>
    getMarkets().then(
      (l) => (setMarkets(l), setError(null)),
      (e) => {
        // a hub without markets (older: 404; no extension base or database: 503): no section at all
        if (e instanceof AdminError && (e.status === 404 || e.status === 503)) setGone(true);
        else setError(e);
      },
    );
  useEffect(() => void load(), []);
  const put = (m: Market) => setMarkets((l) => (l ?? []).map((x) => (x.id === m.id ? m : x)));
  const changed = () => (void load(), setCatalogKey((k) => k + 1));

  const refresh = async (m: Market) => {
    setRefreshing(m.id);
    try {
      put(await refreshMarket(m.id));
      setCatalogKey((k) => k + 1);
      toast(`已重新读取「${m.name}」`, "ok");
    } catch (e) {
      toast(`读不到「${m.name}」：${errorText(e)}`, "error");
      void load();
    } finally {
      setRefreshing("");
    }
  };
  const remove = async (m: Market) => {
    try {
      await removeMarket(m.id);
      toast(`已删除「${m.name}」，从它装的扩展留着`, "ok");
      changed();
    } catch (e) {
      toast(errorText(e), "error");
    }
  };

  if (gone) return null;
  return (
    <>
      <Card
        title="扩展市场"
        right={
          <Button tone="quiet" className="h-7 px-2" onClick={() => setAdding(true)}>
            <AdminIcon name="plus" size={13} />
            添加市场
          </Button>
        }
        footer="市场是网站或 git / GitHub 仓库发布的扩展索引（persona-extensions.json）。私有的 https / GitHub 市场填一个只读 token：加密存在 hub 上，只发给这个市场自己，填了以后不再显示；git: 的市场用 hub 机器自己的 SSH 凭据。删除市场不影响从它装的扩展。"
      >
        {!markets ? (
          error ? <ErrorBox error={error} onRetry={() => void load()} /> : <Loader />
        ) : markets.length === 0 ? (
          <EmptyState icon="puzzle" title="还没有市场" action={<Button onClick={() => setAdding(true)}>添加市场</Button>}>
            填一个 https 地址、<span className="font-mono">github:owner/repo</span> 或 <span className="font-mono">git:…</span>，hub 读到它的索引后，这里就能浏览和安装里面的扩展。
          </EmptyState>
        ) : (
          markets.map((m) => (
            <div key={m.id} className="flex flex-wrap items-start gap-x-3 gap-y-2 px-4 py-3">
              <span className={cx("flex size-9 shrink-0 items-center justify-center rounded-[10px]", m.error ? "bg-red-tint text-red" : "bg-inset text-ink-2 shadow-hairline")}>
                <AdminIcon name={marketKind(m.spec) === "web" ? "web" : "git"} size={16} />
              </span>
              <div className="min-w-[200px] flex-1">
                <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                  <span className="text-[13.5px] font-medium text-ink">{m.name}</span>
                  {m.official && <Tag tone="accent">官方</Tag>}
                  {m.has_token && (
                    <Tag>
                      <AdminIcon name="lock" size={10} />
                      token
                    </Tag>
                  )}
                </div>
                <div className="mt-0.5 font-mono text-[11.5px] break-all text-ink-3">{specLabel(m.spec)}</div>
                <div className="mt-0.5 text-[11.5px] text-ink-3">{marketLine(m, (iso) => ago(iso))}</div>
                {m.error && <p className="mt-1 text-[12px] leading-relaxed break-words text-red">读不到：{m.error}</p>}
                {(m.warnings ?? []).map((w) => (
                  <p key={w} className="mt-1 flex items-start gap-1.5 text-[11.5px] leading-relaxed break-words text-orange">
                    <AdminIcon name="alert" size={12} className="mt-0.5 shrink-0" />
                    {w}
                  </p>
                ))}
              </div>
              <div className="ml-auto flex shrink-0 items-center gap-1">
                <IconButton icon="retry" label={`重新读取「${m.name}」`} onClick={() => void refresh(m)} disabled={refreshing === m.id} size={28} iconSize={14} />
                {!isGitMarket(m.spec) && (
                  <Button tone="quiet" className="h-7 px-2" onClick={() => setTokenFor(m)}>
                    <AdminIcon name="key" size={13} />
                    {m.has_token ? "换 token" : "填 token"}
                  </Button>
                )}
                <ConfirmButton label={`删除市场「${m.name}」`} confirm="删除" onConfirm={() => remove(m)} />
              </div>
            </div>
          ))
        )}
      </Card>
      {markets && markets.length > 0 && <MarketCatalog key={catalogKey} markets={markets} onChanged={() => (onChanged(), setCatalogKey((k) => k + 1))} />}
      <AddMarketDialog open={adding} onClose={() => setAdding(false)} onAdded={(m) => (setAdding(false), toast(`已添加「${m.name}」`, "ok"), changed())} />
      <TokenDialog market={tokenFor} onClose={() => setTokenFor(null)} onDone={(m) => (put(m), setTokenFor(null), setCatalogKey((k) => k + 1))} />
    </>
  );
}

function MarketCatalog({ markets, onChanged }: { markets: Market[]; onChanged: () => void }) {
  const [q, setQ] = useState("");
  const [market, setMarket] = useState("");
  const [data, setData] = useState<Catalog | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(false);
  const [picked, setPicked] = useState<CatalogItem | null>(null);
  const seq = useRef(0);
  const load = (refresh = false) => {
    const n = ++seq.current;
    setLoading(true);
    getCatalog(q, market, refresh).then(
      (c) => {
        if (n !== seq.current) return;
        setData(c);
        setError(null);
        setLoading(false);
        // Dev only (screenshots): ?dialog=market previews the first extension it can upgrade or install.
        if (devDialog() === "market" && !picked && !q && !market) setPicked(c.extensions.find((x) => itemState(x).kind === "upgrade") ?? c.extensions.find((x) => itemState(x).kind === "install") ?? null);
      },
      (e) => n === seq.current && (setError(e), setLoading(false)),
    );
  };
  useEffect(() => {
    const t = setTimeout(() => load(), q ? 250 : 0);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q, market]);

  return (
    <Card
      title={data ? `市场里的 ${data.extensions.length} 个扩展` : "市场里的扩展"}
      right={<IconButton icon="retry" label="重新读取所有市场" onClick={() => load(true)} disabled={loading} size={24} iconSize={13} className="-mb-1" />}
      footer="装扩展就是在 hub 机器上运行一个新程序：点「安装」先看它能做什么，同意了才装；包按市场写的 sha256 校验。从市场装的扩展在上面的列表里升级、回滚、卸载。"
    >
      <div className="flex flex-wrap items-center gap-2 px-4 py-3">
        <label className="flex h-9 min-w-[220px] flex-1 items-center gap-2 rounded-control bg-inset px-3 shadow-hairline">
          <AdminIcon name="search" size={14} className="shrink-0 text-ink-3" />
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="搜索名字、说明、发布者、标签"
            aria-label="搜索扩展"
            spellCheck={false}
            className="h-full min-w-0 flex-1 bg-transparent text-[13px] text-ink outline-none placeholder:text-ink-3"
          />
        </label>
        {markets.length > 1 && (
          <Segmented value={market || "-"} options={[{ value: "-", label: "全部" }, ...markets.map((m) => ({ value: m.id, label: m.name }))]} onChange={(v) => setMarket(v === "-" ? "" : v)} label="按市场筛选" />
        )}
      </div>
      {!data ? (
        error ? <ErrorBox error={error} onRetry={() => load()} /> : <Loader />
      ) : data.extensions.length === 0 ? (
        <p className="px-4 py-6 text-center text-[12.5px] text-ink-3">{q.trim() ? `没有找到「${q.trim()}」` : "这里还没有扩展"}</p>
      ) : (
        data.extensions.map((it) => {
          const st = itemState(it);
          return (
            <div key={`${it.market}/${it.id}`} className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3">
              <span className={cx("flex size-9 shrink-0 items-center justify-center rounded-[10px]", it.installed ? "bg-accent-tint text-accent-ink" : "bg-inset text-ink-2 shadow-hairline")}>
                <AdminIcon name="puzzle" size={16} />
              </span>
              <div className="min-w-[200px] flex-1">
                <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                  <span className="text-[13.5px] font-medium text-ink">{it.name}</span>
                  <span className="font-mono text-[11.5px] text-ink-3">{it.version}</span>
                  <Tag tone={st.tone}>{st.label}</Tag>
                </div>
                <div className="mt-0.5 text-[11.5px] text-ink-3">
                  {[it.publisher, it.market_name].filter(Boolean).join(" · ")}
                  {it.description && <span className="text-ink-2"> — {it.description}</span>}
                </div>
                {!!it.tags?.length && (
                  <div className="mt-1 flex flex-wrap gap-1">
                    {it.tags.map((t) => (
                      <Tag key={t}>{t}</Tag>
                    ))}
                  </div>
                )}
                {it.problem && <p className="mt-1 text-[12px] leading-relaxed text-orange">装不了：{it.problem}</p>}
                {canUninstall(it) && st.kind !== "upgrade" && <p className="mt-1 text-[11.5px] text-ink-3">从这个市场装的，在上面的列表里管理。</p>}
              </div>
              {(st.kind === "install" || st.kind === "upgrade") && (
                <Button tone={st.kind === "upgrade" ? "primary" : "secondary"} className="ml-auto h-7 px-2.5" onClick={() => setPicked(it)}>
                  <AdminIcon name={st.kind === "upgrade" ? "up" : "plus"} size={13} />
                  {st.kind === "upgrade" ? "升级" : "安装"}
                </Button>
              )}
            </div>
          );
        })
      )}
      <MarketInstallDialog item={picked} onClose={() => setPicked(null)} onDone={() => (setPicked(null), onChanged())} />
    </Card>
  );
}

/** Previews a catalog extension, then installs or upgrades exactly that preview. */
function MarketInstallDialog({ item, onClose, onDone }: { item: CatalogItem | null; onClose: () => void; onDone: () => void }) {
  const [pv, setPv] = useState<ExtensionPreview | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const look = () => {
    if (!item) return;
    setPv(null);
    setError("");
    setBusy(true);
    previewFromMarket(item.market, item.id)
      .then(setPv, (e) => setError(errorText(e)))
      .finally(() => setBusy(false));
  };
  useEffect(look, [item]);
  const now = useNow(1000);
  const agree = async () => {
    if (!item || !pv) return;
    setBusy(true);
    setError("");
    try {
      const p = pv.upgrade ? await upgradeExtension(item.id, pv) : await installExtension(pv);
      toast(pv.upgrade ? `「${p.name}」已升级到 ${p.version}` : `已安装「${p.name}」`, "ok");
      onDone();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={Boolean(item)}
      onClose={onClose}
      width={540}
      title={item ? (itemState(item).kind === "upgrade" ? `升级「${item.name}」` : `安装「${item.name}」`) : ""}
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>
            取消
          </Button>
          {pv ? (
            <Button tone={pv.more_permissions ? "danger" : "primary"} busy={busy} disabled={previewBlocked(pv, now)} onClick={() => void agree()}>
              {pv.upgrade ? (pv.more_permissions ? "同意新权限并升级" : "升级") : "同意并安装"}
            </Button>
          ) : (
            <Button tone="primary" busy={busy} disabled={busy} onClick={look}>
              再看一次
            </Button>
          )}
        </>
      }
    >
      {pv ? (
        <PreviewBody pv={pv} />
      ) : busy ? (
        <Loader label="正在下载、校验，看看它能做什么" />
      ) : null}
      {error && <p className={cx("text-[12.5px] leading-relaxed break-words text-red", pv && "mt-3")}>{error}</p>}
    </Dialog>
  );
}

function AddMarketDialog({ open, onClose, onAdded }: { open: boolean; onClose: () => void; onAdded: (m: Market) => void }) {
  const [spec, setSpec] = useState("");
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    if (open) (setSpec(""), setToken(""), setError(""));
  }, [open]);
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
      open={open}
      onClose={onClose}
      title="添加扩展市场"
      width={500}
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>
            取消
          </Button>
          <Button tone="primary" busy={busy} disabled={!spec.trim()} onClick={() => void go()}>
            添加
          </Button>
        </>
      }
    >
      <div className="grid gap-3">
        <Field
          label="地址"
          value={spec}
          onChange={setSpec}
          placeholder={MARKET_SPEC_PLACEHOLDER}
          mono
          autoFocus
          autoCapitalize="off"
          hint={MARKET_SPEC_HINT}
        />
        <Field
          label="token（可选）"
          type="password"
          value={token}
          onChange={setToken}
          placeholder={git ? "git: 的市场不用 token" : "私有的市场才要"}
          mono
          autoComplete="new-password"
          disabled={git}
          hint={git ? "git: 的市场用 hub 机器自己的 SSH 凭据读，不收 token。" : "GitHub 用只读的 fine-grained token（这个仓库的 Contents 读权限）。加密存在 hub 上，填了以后不再显示。"}
        />
        {busy && <p className="text-[12px] text-ink-3">正在读一次它的索引，读到了才登记…</p>}
        {error && <p className="text-[12.5px] leading-relaxed break-words text-red">{error}</p>}
      </div>
    </Dialog>
  );
}

function TokenDialog({ market, onClose, onDone }: { market: Market | null; onClose: () => void; onDone: (m: Market) => void }) {
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState<"" | "save" | "clear">("");
  const [error, setError] = useState("");
  useEffect(() => (setToken(""), setError("")), [market]);
  const run = async (kind: "save" | "clear") => {
    if (!market || (kind === "save" && !token.trim())) return;
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
      open={Boolean(market)}
      onClose={onClose}
      title={market ? `「${market.name}」的 token` : ""}
      width={460}
      footer={
        <>
          {market?.has_token && (
            <Button tone="quiet" className="mr-auto text-red enabled:hover:text-red" busy={busy === "clear"} disabled={!!busy} onClick={() => void run("clear")}>
              去掉 token
            </Button>
          )}
          <Button tone="quiet" onClick={onClose}>
            取消
          </Button>
          <Button tone="primary" busy={busy === "save"} disabled={!token.trim() || !!busy} onClick={() => void run("save")}>
            保存
          </Button>
        </>
      }
    >
      <div className="grid gap-3">
        <Field
          label={market?.has_token ? "新的 token（现在的不显示）" : "token"}
          type="password"
          value={token}
          onChange={setToken}
          mono
          autoFocus
          autoComplete="new-password"
          hint="保存后 hub 用它重新读一次索引。"
        />
        {error && <p className="text-[12.5px] leading-relaxed break-words text-red">{error}</p>}
      </div>
    </Dialog>
  );
}
