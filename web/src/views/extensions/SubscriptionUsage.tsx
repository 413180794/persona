import { type ReactNode, useEffect, useState } from "react";
import { formatDate, formatNumber, t } from "../../i18n";
import {
  isWarn, measuredUsage, planNote, sampleAge, useUsageWindows, useUsageWindowsStatus,
  windowKey, windowMinutes, type UsageAccount, type UsageWindow,
} from "../../lib/usage-windows";
import { cx, Loading } from "../../ui/atoms";
import { Meter } from "../UsageBars";

const TIME_ZONE = "Asia/Shanghai";
const MINUTE = 60_000;
const DAY = 24 * 60 * MINUTE;
const PROVIDERS = [
  { provider: "codex", name: "Codex" },
  { provider: "claude", name: "Claude Code" },
  { provider: "agy", name: "AGY" },
];
const PLAN_WINDOWS: UsageWindow[] = [
  { label: "5h", window_minutes: 300, used_percent: NaN },
  { label: "7d", window_minutes: 10080, used_percent: NaN },
];

function timestamp(value?: string): number | undefined {
  const time = value ? Date.parse(value) : NaN;
  return time > 0 ? time : undefined;
}

function localClock(time: number, now: number): string {
  const date = (at: number) => formatDate(at, { dateStyle: "medium", timeZone: TIME_ZONE });
  const clock = formatDate(time, { hour: "2-digit", minute: "2-digit", hourCycle: "h23", timeZone: TIME_ZONE });
  if (date(time) === date(now)) return t("subscription-usage.today", { clock });
  if (date(time) === date(now + DAY)) return t("subscription-usage.tomorrow", { clock });
  return formatDate(time, { dateStyle: "medium", timeStyle: "short", hourCycle: "h23", timeZone: TIME_ZONE });
}

function resetText(time: number, now: number): string {
  const clock = localClock(time, now);
  if (time <= now) return t("subscription-usage.reset_passed", { clock });
  const minutes = Math.ceil((time - now) / MINUTE);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);
  const countdown = days
    ? hours % 24 ? t("usage.days_hours", { d: days, h: hours % 24 }) : t("usage.days", { d: days })
    : [
        hours && t("usage.hours", { h: hours }),
        (minutes % 60) && t("usage.minutes", { m: minutes % 60 }),
      ].filter(Boolean).join(" ");
  return t("subscription-usage.reset", { countdown, clock });
}

function hasPlanWindow(account: UsageAccount, minutes: number): boolean {
  return account.windows?.some((w) => !w.model && windowMinutes(w) === minutes && measuredUsage(account, w) !== undefined) ?? false;
}

function accountWindows(account: UsageAccount, omitFiveHour: boolean): UsageWindow[] {
  const windows = (account.windows ?? []).filter((w) => !omitFiveHour || w.model || windowMinutes(w) !== 300);
  if (account.provider === "codex" || account.provider === "claude") {
    for (const expected of PLAN_WINDOWS) {
      if (omitFiveHour && expected.window_minutes === 300) continue;
      if (!windows.some((w) => !w.model && windowMinutes(w) === expected.window_minutes)) windows.push(expected);
    }
  }
  return windows.sort((a, b) => windowMinutes(a) - windowMinutes(b));
}

function sampleDetails(a: UsageAccount, w: UsageWindow, now: number) {
  // The legacy local AGY collector sends one observation timestamp for all model groups.
  const sameObservation = a.provider === "agy" && !a.local && !a.machines?.length &&
    a.sources?.length === 1 && a.sources[0] === "AGY local";
  const sampled = timestamp(w.sampled_at ?? (sameObservation && !a.sample_time_estimated ? a.sampled_at : undefined));
  const staleAt = timestamp(w.stale_at ?? (sameObservation ? a.stale_at : undefined));
  return {
    sampled: sampled !== undefined && sampled <= now ? sampled : undefined,
    stale: w.stale || (sameObservation && a.stale) || (staleAt !== undefined && staleAt <= now),
    problem: w.problem || (sameObservation && a.problem),
  };
}

function UsageWindowRow({ account, window: w, now }: { account: UsageAccount; window: UsageWindow; now: number }) {
  const used = measuredUsage(account, w);
  const { sampled, stale, problem } = sampleDetails(account, w, now);
  const reset = used === undefined ? undefined : timestamp(w.resets_at);
  const label = [w.label, w.model].filter(Boolean).join(" · ");
  const rounded = used === undefined ? undefined : Math.round(used * 10) / 10;
  const percent = rounded === undefined ? undefined : formatNumber(rounded / 100, { style: "percent", maximumFractionDigits: 1 });
  const status =
    used === undefined ? "pending" :
    reset !== undefined && reset <= now ? "expired" :
    problem ? "read_failed" :
    stale ? "stale" :
    sampled === undefined ? "sample_unknown" :
    reset === undefined ? "reset_unknown" : undefined;

  return (
    <div className="flex min-w-0 flex-col gap-2 rounded-control bg-inset p-3">
      <div className="flex items-baseline justify-between gap-2">
        <h3 className="min-w-0 wrap-anywhere text-meta font-medium text-ink">{label}</h3>
        {percent && <span className="shrink-0 text-body font-semibold tabular-nums text-ink">{percent}</span>}
      </div>
      {rounded !== undefined && percent !== undefined ? (
        <Meter used={rounded} tall tone={isWarn(rounded) ? "high" : rounded >= 50 ? "medium" : "low"}
          label={t("usage.window_used_label", { name: account.name, window: label, percent })} />
      ) : <div aria-hidden className="h-1.5 rounded-full bg-hover-2" />}
      <div className="text-label leading-relaxed text-ink-2">
        {reset !== undefined && <time className="block" dateTime={w.resets_at}>{resetText(reset, now)}</time>}
        {used !== undefined && sampled !== undefined && (
          <time className="block text-ink-3" dateTime={new Date(sampled).toISOString()}>
            {t("subscription-usage.sample", { age: sampleAge(new Date(sampled).toISOString(), now) })}
          </time>
        )}
        {status && <p className="mt-0.5 text-ink-3">{t(`subscription-usage.${status}`)}</p>}
      </div>
    </div>
  );
}

function AccountCard({ account, now }: { account: UsageAccount; now: number }) {
  const codexPro = account.provider === "codex" && (account.plan?.trim().toLowerCase() === "pro" || account.name.trim().toLowerCase() === "codex pro");
  const omitFiveHour = codexPro && !hasPlanWindow(account, 300);
  const windows = accountWindows(account, omitFiveHour);
  const provider = PROVIDERS.find((p) => p.provider === account.provider)?.name;
  const name = account.name || provider || account.provider;
  const title = provider && !name.toLowerCase().includes(provider.toLowerCase()) ? provider : name;
  const subtitle = [title !== name ? name : "", planNote({ ...account, name: `${title} ${name}` })].filter(Boolean).join(" · ");
  const machines = account.machines?.length ? account.machines : account.local ? [t("usage.source_local")] : [];
  const sources = account.sources ?? [];
  // Legacy Claude sources merge windows before reporting the account's record time.
  const recorded = !account.sample_time_estimated && account.provider === "claude" &&
    windows.some((w) => measuredUsage(account, w) !== undefined && sampleDetails(account, w, now).sampled === undefined)
    ? timestamp(account.sampled_at) : undefined;
  const waitingForWeek = account.provider === "agy" && windows.length > 0 &&
    !windows.some((w) => windowMinutes(w) === 10080 && measuredUsage(account, w) !== undefined);
  return (
    <article className="min-w-0 rounded-card bg-surface p-4 shadow-card sm:p-5" data-subscription-account>
      <header className="mb-3 flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
        <h2 className="min-w-0 wrap-anywhere text-body font-semibold text-ink">{title}</h2>
        {subtitle && <span className="min-w-0 wrap-anywhere text-meta text-ink-3">{subtitle}</span>}
      </header>
      {windows.length ? (
        <div className="@container">
          <div className={cx("grid gap-2", windows.length > 1 && "@min-[32rem]:grid-cols-2")}>
            {windows.map((w) => <UsageWindowRow key={windowKey(w)} account={account} window={w} now={now} />)}
          </div>
        </div>
      ) : <p className="rounded-control bg-inset p-3 text-label text-ink-3">{t("subscription-usage.pending")}</p>}
      {omitFiveHour && <p className="mt-3 text-label text-ink-3">{t("subscription-usage.codex_pro_weekly")}</p>}
      {waitingForWeek && <p className="mt-3 text-label text-ink-3">{t("subscription-usage.agy_weekly_pending")}</p>}
      {(machines.length > 0 || sources.length > 0 || (recorded !== undefined && recorded <= now)) && (
        <footer className="mt-3 flex flex-wrap gap-x-4 gap-y-1 wrap-anywhere text-label text-ink-3">
          {recorded !== undefined && recorded <= now && <time dateTime={account.sampled_at}>{t("subscription-usage.recorded", { age: sampleAge(account.sampled_at, now) })}</time>}
          {machines.length > 0 && <span>{t("subscription-usage.devices", { names: machines.join(" · ") })}</span>}
          {sources.length > 0 && <span>{t("subscription-usage.sources", { names: sources.join(" · ") })}</span>}
        </footer>
      )}
    </article>
  );
}

export function SubscriptionUsageCards({ accounts, now }: { accounts: UsageAccount[]; now: number }) {
  const shown = PROVIDERS.flatMap(({ provider, name }) => {
    const matching = accounts.filter((a) => a.provider === provider);
    return matching.length ? matching : [{ id: provider, provider, name, windows: [], sampled_at: "" }];
  });
  shown.push(...accounts.filter((a) => !PROVIDERS.some((p) => p.provider === a.provider)));
  return (
    <div className="flex flex-col gap-3" data-subscription-usage>
      <p className="text-label text-ink-3">{t("subscription-usage.time_zone")}</p>
      {shown.map((a) => <AccountCard key={a.id} account={a} now={now} />)}
    </div>
  );
}

export function SubscriptionUsage({ fallback }: { fallback: ReactNode }) {
  const accounts = useUsageWindows();
  const status = useUsageWindowsStatus();
  const [, setTick] = useState(0);
  useEffect(() => {
    const tick = () => setTick((n) => n + 1);
    const timer = setInterval(tick, MINUTE);
    document.addEventListener("visibilitychange", tick);
    return () => {
      clearInterval(timer);
      document.removeEventListener("visibilitychange", tick);
    };
  }, []);
  if (status === "loading") return <Loading />;
  if (status === "unavailable") return fallback;
  return <SubscriptionUsageCards accounts={accounts} now={Date.now()} />;
}
