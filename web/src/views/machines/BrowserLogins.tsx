// "Browser sign-ins" on a Mac's page (#1304, docs/plans/plan-ego-browser.md "Importing cookies"; iOS BrowserLoginsSection): the sites
// whose sign-ins ego lite keeps (how many cookies, until when, never what), clearing a site's or all, and importing a
// cookie file (cookies.txt, or an extension's JSON). The file is read here, its sites ticked by the user (none at first),
// and only the ticked ones go to the hub, which passes them to the Mac and keeps nothing. The Mac must be online.
import { useEffect, useRef, useState } from "react";
import { api, enc, errorText, pastHub } from "../../lib/api";
import { parseCookies, picked, sitesOf, type CookieSite, type CookieSiteNow } from "../../lib/cookies";
import { Button, cx, Dialog, Loading, toast } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { formatDate, t } from "../../i18n";

interface Answer {
  sites: CookieSiteNow[];
  imported?: number;
  skipped?: number;
  removed?: number;
}

const day = (at: string | number) => formatDate(typeof at === "number" ? at * 1000 : at, { year: "numeric", month: "numeric", day: "numeric" });

export function BrowserLogins({ mid, online }: { mid: string; online: boolean }) {
  const [sites, setSites] = useState<CookieSiteNow[] | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [file, setFile] = useState<{ name: string; sites: CookieSite[]; bad: number } | null>(null);
  const [ticked, setTicked] = useState<Set<string>>(new Set());
  const [clearing, setClearing] = useState<string | "all" | null>(null);
  const [tries, setTries] = useState(0); // "Try again" reads the sites again
  const input = useRef<HTMLInputElement>(null);
  const path = `/machines/${enc(mid)}/browser/cookies`;

  useEffect(() => {
    setSites(null);
    setError("");
    if (!online) return;
    let live = true;
    // the hub gives the Mac's browser 2 min 15 s (extmgr/browser_cookies.go)
    api<Answer>(path, { timeout: pastHub(135_000) })
      .then((a) => live && setSites(a.sites))
      .catch((e) => live && setError(errorText(e)));
    return () => {
      live = false;
    };
  }, [path, online, tries]);

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
      toast(a.skipped ? t("machines-browserlogins.imported_skipped", { count: a.imported ?? 0, skipped: a.skipped }) : t("machines-browserlogins.imported", { count: a.imported ?? 0 }), "ok");
    } catch (e) {
      toast(t("machines-browserlogins.import_failed", { error: errorText(e) }), "error");
    } finally {
      setBusy(false);
    }
  };

  const clear = async (what: string | "all") => {
    setBusy(true);
    try {
      const a = await api<Answer>(`${path}?${what === "all" ? "all=1" : `site=${enc(what)}`}`, { method: "DELETE" });
      setSites(a.sites);
      toast(t("machines-browserlogins.cleared", { count: a.removed ?? 0 }), "ok");
    } catch (e) {
      toast(t("machines-browserlogins.clear_failed", { error: errorText(e) }), "error");
    } finally {
      setBusy(false);
      setClearing(null);
    }
  };

  const count = file ? picked(file.sites, ticked).length : 0;
  return (
    <section data-browser-logins="">
      <div className="mb-2.5 flex min-h-7 items-center gap-2 px-1">
        <h2 className="text-[13px] font-medium text-ink">{t("machines-browserlogins.title")}</h2>
        {sites && <span className="text-[12px] text-ink-3 tabular-nums">{sites.length}</span>}
        <span className="flex-1" />
        {sites && sites.length > 0 && (
          <Button tone="quiet" onClick={() => setClearing("all")} disabled={busy || !online}>
            {t("machines-browserlogins.clear_all")}
          </Button>
        )}
        <Button icon="arrowDown" onClick={() => input.current?.click()} disabled={busy || !online}>
          {t("machines-browserlogins.import_file")}
        </Button>
        <input ref={input} type="file" accept=".txt,.json,text/plain,application/json" className="hidden" onChange={(e) => void pick(e.target.files?.[0])} />
      </div>
      <div className="overflow-hidden rounded-card bg-surface shadow-card">
        {!online ? (
          <p className="px-4 py-5 text-[12.5px] text-ink-3">{t("machines-browserlogins.offline")}</p>
        ) : error ? (
          <div className="flex items-center gap-3 px-4 py-4">
            <p className="min-w-0 flex-1 text-[12.5px] text-red">{error}</p>
            <Button onClick={() => setTries((n) => n + 1)}>{t("machines-browserlogins.try_again")}</Button>
          </div>
        ) : !sites ? (
          <div className="flex h-20 items-center justify-center">
            <Loading label={t("machines-browserlogins.loading")} />
          </div>
        ) : sites.length === 0 ? (
          <p className="px-4 py-5 text-[12.5px] text-ink-3">{t("machines-browserlogins.empty")}</p>
        ) : (
          <ul>
            {sites.map((s) => (
              <li key={s.domain} className="flex min-h-12 items-center gap-3 border-b border-line px-4 py-2 last:border-b-0">
                <Icon name="lock" size={14} className="shrink-0 text-ink-3" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13px] font-medium text-ink">{s.domain}</span>
                  <span className="block text-[11.5px] text-ink-3 tabular-nums">
                    {[t("machines-browserlogins.count", { count: s.count }), s.expires ? t("machines-browserlogins.expires", { date: day(s.expires) }) : t("machines-browserlogins.session_only")].join(" · ")}
                  </span>
                </span>
                <Button tone="quiet" onClick={() => setClearing(s.domain)} disabled={busy}>
                  {t("machines-browserlogins.clear")}
                </Button>
              </li>
            ))}
          </ul>
        )}
      </div>
      <p className="mt-2 px-1 text-[11.5px] leading-relaxed text-ink-3">{t("machines-browserlogins.privacy")}</p>

      <Dialog
        open={!!file}
        onClose={() => !busy && setFile(null)}
        title={t("machines-browserlogins.pick_title")}
        width={480}
        footer={
          <div className="flex items-center justify-end gap-2">
            <Button tone="quiet" onClick={() => setFile(null)} disabled={busy}>
              {t("common.cancel")}
            </Button>
            <Button tone="primary" onClick={() => void importTicked()} disabled={busy || count === 0} busy={busy}>
              {count ? t("machines-browserlogins.import_count", { count }) : t("machines-browserlogins.import")}
            </Button>
          </div>
        }
      >
        {file && (
          <div className="flex flex-col gap-3">
            <p className="text-[12.5px] text-ink-3">
              {file.bad ? t("machines-browserlogins.file_summary_bad", { name: file.name, count: file.sites.length, bad: file.bad }) : t("machines-browserlogins.file_summary", { name: file.name, count: file.sites.length })}
            </p>
            <div className="flex items-center gap-3 text-[12px]">
              <button type="button" className="font-medium text-accent-ink" onClick={() => setTicked(new Set(file.sites.filter((s) => s.cookies.length).map((s) => s.site)))}>
                {t("machines-browserlogins.select_all")}
              </button>
              <button type="button" className="text-ink-3 hover:text-ink" onClick={() => setTicked(new Set())}>
                {t("machines-browserlogins.select_none")}
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
                          {[t("machines-browserlogins.count", { count: s.cookies.length }), s.expires && t("machines-browserlogins.expires", { date: day(s.expires) }), s.expired && t("machines-browserlogins.expired_skip", { count: s.expired })].filter(Boolean).join(" · ")}
                        </span>
                      </span>
                    </label>
                  </li>
                );
              })}
            </ul>
            <p className="text-[11.5px] leading-relaxed text-ink-3">{t("machines-browserlogins.import_note")}</p>
          </div>
        )}
      </Dialog>

      <Dialog
        open={clearing !== null}
        onClose={() => !busy && setClearing(null)}
        title={clearing === "all" ? t("machines-browserlogins.clear_all_title") : t("machines-browserlogins.clear_site_title", { site: clearing ?? "" })}
        footer={
          <div className="flex items-center justify-end gap-2">
            <Button tone="quiet" onClick={() => setClearing(null)} disabled={busy}>
              {t("common.cancel")}
            </Button>
            <Button tone="danger" onClick={() => clearing && void clear(clearing)} disabled={busy} busy={busy}>
              {t("machines-browserlogins.clear")}
            </Button>
          </div>
        }
      >
        <p className="text-[13px] leading-relaxed text-ink-2">
          {clearing === "all" ? t("machines-browserlogins.clear_all_body") : t("machines-browserlogins.clear_site_body")}
        </p>
      </Dialog>
    </section>
  );
}
