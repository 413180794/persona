// 浏览器登录 on a Mac's page (#1304, docs/plan-ego-browser.md「导入 cookies」; iOS BrowserLoginsSection): the sites
// whose sign-ins ego lite keeps (how many cookies, until when, never what), clearing a site's or all, and importing a
// cookie file (cookies.txt, or an extension's JSON). The file is read here, its sites ticked by the user (none at first),
// and only the ticked ones go to the hub, which passes them to the Mac and keeps nothing. The Mac must be online.
import { useEffect, useRef, useState } from "react";
import { api, enc, errorText } from "../../lib/api";
import { parseCookies, picked, sitesOf, type CookieSite, type CookieSiteNow } from "../../lib/cookies";
import { Button, cx, Dialog, Loading, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";

interface Answer {
  sites: CookieSiteNow[];
  imported?: number;
  skipped?: number;
  removed?: number;
}

const day = (t: string | number) => new Date(typeof t === "number" ? t * 1000 : t).toLocaleDateString("zh-CN", { year: "numeric", month: "numeric", day: "numeric" });

export function BrowserLogins({ mid, online }: { mid: string; online: boolean }) {
  const [sites, setSites] = useState<CookieSiteNow[] | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [file, setFile] = useState<{ name: string; sites: CookieSite[]; bad: number } | null>(null);
  const [ticked, setTicked] = useState<Set<string>>(new Set());
  const [clearing, setClearing] = useState<string | "all" | null>(null);
  const input = useRef<HTMLInputElement>(null);
  const path = `/machines/${enc(mid)}/browser/cookies`;

  useEffect(() => {
    setSites(null);
    setError("");
    if (!online) return;
    let live = true;
    api<Answer>(path)
      .then((a) => live && setSites(a.sites))
      .catch((e) => live && setError(errorText(e)));
    return () => {
      live = false;
    };
  }, [path, online]);

  const pick = async (f: File | undefined) => {
    if (!f) return;
    if (input.current) input.current.value = ""; // the same file can be picked again
    try {
      const parsed = parseCookies(await f.text());
      setFile({ name: f.name, sites: sitesOf(parsed.cookies), bad: parsed.bad });
      setTicked(new Set());
    } catch (e) {
      toast(errorText(e), "error");
    }
  };

  const importTicked = async () => {
    if (!file) return;
    const cookies = picked(file.sites, ticked);
    setBusy(true);
    try {
      const a = await api<Answer>(path, { method: "POST", json: { cookies } });
      setSites(a.sites);
      setFile(null); // the file's contents are gone from the page once sent
      toast(`已导入 ${a.imported ?? 0} 条${a.skipped ? `，跳过 ${a.skipped} 条过期或格式不对的` : ""}`, "ok");
    } catch (e) {
      toast(`导入失败：${errorText(e)}`, "error");
    } finally {
      setBusy(false);
    }
  };

  const clear = async (what: string | "all") => {
    setBusy(true);
    try {
      const a = await api<Answer>(`${path}?${what === "all" ? "all=1" : `site=${enc(what)}`}`, { method: "DELETE" });
      setSites(a.sites);
      toast(`已清除 ${a.removed ?? 0} 条`, "ok");
    } catch (e) {
      toast(`清除失败：${errorText(e)}`, "error");
    } finally {
      setBusy(false);
      setClearing(null);
    }
  };

  const count = file ? picked(file.sites, ticked).length : 0;
  return (
    <section data-browser-logins="">
      <div className="mb-2.5 flex min-h-7 items-center gap-2 px-1">
        <h2 className="text-[13px] font-medium text-ink">浏览器登录</h2>
        {sites && <span className="text-[12px] text-ink-3 tabular-nums">{sites.length}</span>}
        <span className="flex-1" />
        {sites && sites.length > 0 && (
          <Button tone="quiet" onClick={() => setClearing("all")} disabled={busy || !online}>
            全部清除
          </Button>
        )}
        <Button icon="arrowDown" onClick={() => input.current?.click()} disabled={busy || !online}>
          导入 cookie 文件
        </Button>
        <input ref={input} type="file" accept=".txt,.json,text/plain,application/json" className="hidden" onChange={(e) => void pick(e.target.files?.[0])} />
      </div>
      <div className="overflow-hidden rounded-card bg-surface shadow-card">
        {!online ? (
          <p className="px-4 py-5 text-[12.5px] text-ink-3">这台 Mac 在线时才能查看和导入。</p>
        ) : error ? (
          <p className="px-4 py-5 text-[12.5px] text-red">{error}</p>
        ) : !sites ? (
          <div className="flex h-20 items-center justify-center">
            <Loading label="正在读取" />
          </div>
        ) : sites.length === 0 ? (
          <p className="px-4 py-5 text-[12.5px] text-ink-3">ego lite 里还没有登录过的网站。导入另一台电脑或浏览器导出的 cookie 文件，助手就能用你的登录打开那些网站。</p>
        ) : (
          <ul>
            {sites.map((s) => (
              <li key={s.domain} className="flex min-h-12 items-center gap-3 border-b border-line px-4 py-2 last:border-b-0">
                <Icon name="lock" size={14} className="shrink-0 text-ink-3" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13px] font-medium text-ink">{s.domain}</span>
                  <span className="block text-[11.5px] text-ink-3 tabular-nums">
                    {s.count} 条{s.expires ? ` · 最晚 ${day(s.expires)} 过期` : " · 关掉浏览器就失效"}
                  </span>
                </span>
                <Button tone="quiet" onClick={() => setClearing(s.domain)} disabled={busy}>
                  清除
                </Button>
              </li>
            ))}
          </ul>
        )}
      </div>
      <p className="mt-2 px-1 text-[11.5px] leading-relaxed text-ink-3">只列网站和条数，不显示 cookie 的内容。导入的文件在这个页面里解析，只把你勾选的网站经 hub 直接发给这台 Mac，hub 不保存；助手看不到也导不了。</p>

      <Dialog
        open={!!file}
        onClose={() => !busy && setFile(null)}
        title="选择要导入的网站"
        width={480}
        footer={
          <div className="flex items-center justify-end gap-2">
            <Button tone="quiet" onClick={() => setFile(null)} disabled={busy}>
              取消
            </Button>
            <Button tone="primary" onClick={() => void importTicked()} disabled={busy || count === 0} busy={busy}>
              {count ? `导入 ${count} 条` : "导入"}
            </Button>
          </div>
        }
      >
        {file && (
          <div className="flex flex-col gap-3">
            <p className="text-[12.5px] text-ink-3">
              {file.name} · {file.sites.length} 个网站
              {file.bad ? ` · ${file.bad} 行读不懂，已略过` : ""}。默认一个都不勾，只导你勾选的。
            </p>
            <div className="flex items-center gap-3 text-[12px]">
              <button type="button" className="font-medium text-accent-ink" onClick={() => setTicked(new Set(file.sites.filter((s) => s.cookies.length).map((s) => s.site)))}>
                全选
              </button>
              <button type="button" className="text-ink-3 hover:text-ink" onClick={() => setTicked(new Set())}>
                全不选
              </button>
            </div>
            <ul className="max-h-[46vh] overflow-y-auto rounded-[10px] shadow-hairline">
              {file.sites.map((s) => {
                const none = s.cookies.length === 0;
                const on = ticked.has(s.site);
                return (
                  <li key={s.site} className="border-b border-line last:border-b-0">
                    <label className={cx("flex min-h-11 items-center gap-3 px-3 py-2", none ? "opacity-50" : "cursor-pointer hover:bg-hover")}>
                      <input
                        type="checkbox"
                        checked={on}
                        disabled={none}
                        onChange={() => setTicked((t) => {
                          const n = new Set(t);
                          if (n.has(s.site)) n.delete(s.site);
                          else n.add(s.site);
                          return n;
                        })}
                        className="size-4 accent-[var(--accent)]"
                      />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-[13px] text-ink">{s.site}</span>
                        <span className="block text-[11.5px] text-ink-3 tabular-nums">
                          {s.cookies.length} 条{s.expires ? ` · 最晚 ${day(s.expires)} 过期` : ""}
                          {s.expired ? ` · ${s.expired} 条已过期，不导入` : ""}
                        </span>
                      </span>
                    </label>
                  </li>
                );
              })}
            </ul>
            <p className="text-[11.5px] leading-relaxed text-ink-3">导进这台 Mac 的 ego lite 默认配置：你自己在 ego 里浏览时也会用这些登录。不需要时在这里清除。</p>
          </div>
        )}
      </Dialog>

      <Dialog
        open={clearing !== null}
        onClose={() => !busy && setClearing(null)}
        title={clearing === "all" ? "清除 ego lite 里所有网站的登录？" : `清除 ${clearing} 的登录？`}
        footer={
          <div className="flex items-center justify-end gap-2">
            <Button tone="quiet" onClick={() => setClearing(null)} disabled={busy}>
              取消
            </Button>
            <Button tone="danger" onClick={() => clearing && void clear(clearing)} disabled={busy} busy={busy}>
              清除
            </Button>
          </div>
        }
      >
        <p className="text-[13px] leading-relaxed text-ink-2">
          {clearing === "all" ? "这台 Mac 上 ego lite 的所有 cookie 都会删掉，所有网站都要重新登录。" : "这个网站在这台 Mac 的 ego lite 里会退出登录。"}你自己在 ego 里的登录也一样。
        </p>
      </Dialog>
    </section>
  );
}
