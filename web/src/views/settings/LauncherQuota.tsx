// "Quota" in a launcher's detail (docs/integrations/launchers.md "Quota", iOS LauncherQuotaSection): what its key may still spend at its
// gateway, titled with the gateway's name the hub sends — a budget on the key, each account / provider with its
// windows, the key's own use. Read once when the dialog opens (the hub keeps an answer a minute); ↻ reads again.
import { useEffect, useState } from "react";
import { t } from "../../i18n";
import { api, enc, errorText, pastHub } from "../../lib/api";
import { ago } from "../../lib/format";
import { gatewayName, poolTrouble, quotaAmount, quotaFooter, quotaPercent, quotaReset, quotaTone, quotaUnlimited, quotaUsage, type LauncherQuota as Quota, type LauncherQuotaAnswer, type QuotaPool, type QuotaWindow } from "../../lib/quota";
import { useStore } from "../../lib/store";
import { cx, IconButton, Spinner } from "../../ui/atoms";
import { Icon } from "../../ui/icons";

export function LauncherQuota({ name }: { name: string }) {
  const machines = useStore((s) => s.machines);
  const [answer, setAnswer] = useState<LauncherQuotaAnswer | null>(null);
  const [failed, setFailed] = useState("");
  const [loading, setLoading] = useState(false);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    let dead = false;
    setLoading(true);
    // the hub asks the gateway for up to 45 s (assistant/models_quota.go)
    api<LauncherQuotaAnswer>(`/launchers/${enc(name)}/quota${tick ? "?refresh=1" : ""}`, { timeout: pastHub(45_000) })
      .then((a) => !dead && (setAnswer(a), setFailed("")))
      .catch((e) => !dead && setFailed(errorText(e)))
      .finally(() => !dead && setLoading(false));
    return () => {
      dead = true;
    };
  }, [name, tick]);

  const q = answer?.quota;
  const footer = answer && q ? quotaFooter(answer, (id) => machines.find((m) => m.id === id)?.name ?? id, (at) => ago(at)) : "";

  return (
    <div data-testid="launcher-quota">
      <div className="mb-1 flex min-h-6 items-center gap-2">
        <span className="min-w-0 flex-1 truncate text-[11.5px] font-medium text-ink-3">{q ? t("settings-launcherquota.title_gateway", { gateway: gatewayName(q) }) : t("settings-launcherquota.title")}</span>
        {answer?.unsupported !== true && (
          <span className="-my-1 flex size-6 items-center justify-center">
            {loading && answer ? <Spinner /> : <IconButton icon="retry" label={t("settings-launcherquota.refresh")} size={24} iconSize={13} disabled={loading} onClick={() => setTick((n) => n + 1)} />}
          </span>
        )}
      </div>
      <div className="divide-y divide-line rounded-control bg-inset px-3 shadow-hairline">
        {failed ? (
          <p className="flex items-center gap-1.5 py-2 text-[12px] text-red">
            <Icon name="x" size={12} stroke={2.4} className="shrink-0" />
            {failed}
          </p>
        ) : !answer ? (
          <p className="flex items-center gap-2 py-2 text-[12px] text-ink-3">
            <Spinner />
            {t("settings-launcherquota.loading")}
          </p>
        ) : !q ? (
          <p className="py-2 text-[12px] text-ink-3">{answer.error || t("settings-launcherquota.unavailable")}</p>
        ) : (
          <Report q={q} />
        )}
      </div>
      {footer && <p className="mt-1 text-[11px] leading-relaxed text-ink-3">{footer}</p>}
    </div>
  );
}

function Report({ q }: { q: Quota }) {
  return (
    <>
      {q.note && <p className="py-2 text-[12px] text-ink-2">{q.note}</p>}
      {(q.budget ?? []).map((w) => (
        <div key={w.label} className="py-2">
          <WindowRow w={w} />
        </div>
      ))}
      {(q.pools ?? []).map((p) => (
        <PoolRow key={p.name} p={p} />
      ))}
      {q.usage && q.usage.length > 0 && (
        <div className="py-2">
          <div className="mb-1 text-[11.5px] font-medium text-ink-3">{t("settings-launcherquota.key_usage")}</div>
          {q.usage.map((u) => (
            <div key={u.label} className="flex items-baseline gap-3 py-0.5 text-[11.5px]">
              <span className="shrink-0 text-ink-2">{u.label}</span>
              <span className="flex-1" />
              <span className="truncate text-ink-3 tabular-nums">{quotaUsage(u)}</span>
            </div>
          ))}
        </div>
      )}
      {quotaUnlimited(q) && <p className="py-2 text-[12px] text-ink-3">{t("settings-launcherquota.unlimited")}</p>}
    </>
  );
}

function PoolRow({ p }: { p: QuotaPool }) {
  const [open, setOpen] = useState(false);
  const models = p.models ?? [];
  return (
    <div className="flex flex-col gap-2 py-2">
      <div className="flex items-baseline gap-2">
        <span className="min-w-0 truncate text-[12.5px] font-medium text-ink">{p.name}</span>
        <span className="flex-1" />
        {p.detail && <span className={cx("min-w-0 text-right text-[11.5px]", poolTrouble(p) ? "text-orange" : "text-ink-3")}>{p.detail}</span>}
      </div>
      {(p.windows ?? []).map((w) => (
        <WindowRow key={w.label} w={w} />
      ))}
      {models.length > 0 && (
        <div>
          <button type="button" onClick={() => setOpen((x) => !x)} aria-expanded={open} className="flex items-center gap-1.5 whitespace-nowrap text-[11.5px] font-medium text-ink-2 hover:text-ink">
            <Icon name="chevron" size={10} stroke={2.4} style={{ transform: open ? "none" : "rotate(-90deg)", transition: "transform 150ms" }} />
            {t("settings-launcherquota.by_model", { count: models.length })}
          </button>
          {open && (
            <div className="mt-1.5 flex flex-col gap-2 pl-4">
              {models.map((m) => (
                <div key={m.model} className="flex flex-col gap-1.5">
                  <span className="truncate font-mono text-[11px] text-ink-3">{m.model}</span>
                  {m.windows.map((w) => (
                    <WindowRow key={w.label} w={w} />
                  ))}
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

/** One window: its name, how much is used (of what), a bar, when it resets. */
function WindowRow({ w }: { w: QuotaWindow }) {
  const r = w.used_ratio;
  const tone = r !== undefined ? quotaTone(r, w.exhausted) : "accent";
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-baseline gap-2 text-[11.5px]">
        <span className="truncate text-ink-2">{w.label}</span>
        <span className="flex-1" />
        <span className={cx("shrink-0 whitespace-nowrap tabular-nums", w.exhausted ? "text-red" : "text-ink-3")}>{quotaAmount(w)}</span>
      </div>
      {r !== undefined && (
        <div className="h-1 overflow-hidden rounded-full bg-field" role="meter" aria-valuemin={0} aria-valuemax={100} aria-valuenow={quotaPercent(r)} aria-label={t("settings-launcherquota.used_percent", { label: w.label, percent: quotaPercent(r) })}>
          <div className={cx("h-full rounded-full", tone === "red" ? "bg-red" : tone === "orange" ? "bg-orange" : "bg-accent")} style={{ width: `${Math.max(2, quotaPercent(r))}%` }} />
        </div>
      )}
      {w.resets_at && <span className="truncate text-[10.5px] text-ink-3">{quotaReset(w.resets_at)}</span>}
    </div>
  );
}
