// "Network & access": how the hub is reached (public URL, TLS, advertised addresses — restart to apply), the security settings
// (read only), who may open this admin UI (narrow only: widening is the hub machine's call), the admin password.
import { useEffect, useState } from "react";
import { t } from "../../i18n";
import { tRich } from "../../i18n/rich";
import { Button, cx, Dialog, toast } from "../../ui/atoms";
import { adminApi, AdminError, errorText } from "../api";
import { AdminIcon } from "../icons";
import { ACCESS_INFO, ACCESS_ORDER, canChoose, lines, validCidr } from "../logic";
import { share } from "../store";
import type { Access, NetworkView } from "../types";
import { Card, ErrorBox, Field, Line, Loader, Notice, PageHeader, Tag, useLoad } from "../parts";
import { Endpoints } from "./Overview";

export function Network() {
  const { data, error, loading, reload, set } = useLoad(async () => {
    const v = await adminApi<NetworkView>("/network");
    share({ restart: v.restart, tls: v.tls });
    return v;
  });
  const update = (v: NetworkView) => {
    set(v);
    share({ restart: v.restart, tls: v.tls });
  };
  if (!data) return loading ? <Loader /> : <ErrorBox error={error} onRetry={reload} />;
  return (
    <>
      <PageHeader title={t("admin-pages-network.title")} subtitle={t("admin-pages-network.subtitle")} />
      <AccessCard v={data} onSaved={update} />
      <PublicCard v={data} onSaved={update} />
      <Card title={t("admin-pages-network.endpoints")} footer={t("admin-pages-network.endpoints_footer")}>
        <Endpoints list={data.endpoints} />
      </Card>
      <Card title={t("admin-pages-network.security")} footer={t("admin-pages-network.security_footer")}>
        <Line label={t("admin-pages-network.trusted_cidrs")} hint={t("admin-pages-network.trusted_cidrs_hint")}>
          {data.security.trusted_cidrs.length ? (
            <span className="flex flex-wrap justify-end gap-1">
              {data.security.trusted_cidrs.map((c) => (
                <Tag key={c}>
                  <span className="font-mono">{c}</span>
                </Tag>
              ))}
            </span>
          ) : (
            <span className="text-ink-3">{t("admin-pages-network.none")}</span>
          )}
        </Line>
        <Line label={t("admin-pages-network.public_plaintext")} hint={t("admin-pages-network.public_plaintext_hint")}>
          {data.security.allow_public_plaintext ? <Tag tone="red">{t("admin-pages-network.on")}</Tag> : <Tag tone="green">{t("admin-pages-network.off")}</Tag>}
        </Line>
      </Card>
      <PasswordCard />
    </>
  );
}

function AccessCard({ v, onSaved }: { v: NetworkView; onSaved: (v: NetworkView) => void }) {
  const cur = v.admin.access;
  const [pick, setPick] = useState<Access>(cur);
  const [cidrs, setCidrs] = useState(v.admin.cidrs.join("\n"));
  const [busy, setBusy] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    setPick(v.admin.access);
    setCidrs(v.admin.cidrs.join("\n"));
  }, [v.admin.access, v.admin.cidrs]);

  const list = lines(cidrs);
  const badCidr = pick === "cidrs" ? list.find((c) => !validCidr(c)) : undefined;
  const sameCidrs = list.join(",") === v.admin.cidrs.join(",");
  const dirty = pick !== cur || (pick === "cidrs" && !sameCidrs);
  const lockout = !v.you.allowed[pick];
  const locked = v.admin.from_env;

  const save = async (force = false) => {
    setBusy(true);
    setError("");
    try {
      const r = await adminApi<NetworkView>("/network/access", "PUT", { access: pick, cidrs: pick === "cidrs" ? list : [], force });
      setConfirm(false);
      toast(t("admin-pages-network.access_saved"), "ok");
      onSaved(r);
    } catch (e) {
      if (e instanceof AdminError && e.code === "would_lock_you_out" && !force) setConfirm(true);
      else setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card
      title={t("admin-pages-network.access_title")}
      right={<Tag tone={cur === "any" ? "orange" : "gray"}>{ACCESS_INFO[cur].label}</Tag>}
      footer={
        <>
          {tRich("admin-pages-network.access_footer", { command: <span className="font-mono">{t("admin-pages-network.access_command")}</span> })}
        </>
      }
    >
      {locked && (
        <div className="px-4 pt-3">
          <Notice tone="gray" icon="lock" title={t("admin-pages-network.access_env_title")}>
            {t("admin-pages-network.access_env_body")}
          </Notice>
        </div>
      )}
      <div role="radiogroup" aria-label={t("admin-pages-network.access_label")} className="grid gap-2 p-3 sm:grid-cols-2">
        {ACCESS_ORDER.map((a) => {
          const allowed = !locked && canChoose(cur, a);
          const on = pick === a;
          return (
            <button
              key={a}
              type="button"
              role="radio"
              aria-checked={on}
              disabled={!allowed}
              title={!allowed && !locked ? t("admin-pages-network.access_wider") : undefined}
              onClick={() => setPick(a)}
              className={cx(
                "rounded-card px-3 py-2.5 text-left transition-[background-color,box-shadow,opacity] duration-150 disabled:cursor-not-allowed",
                on ? "bg-accent-tint shadow-[0_0_0_1.5px_var(--accent)]" : "bg-inset shadow-hairline enabled:hover:bg-hover",
                !allowed && !on && "opacity-50",
              )}
            >
              <div className="flex items-center gap-2">
                <span className={cx("text-[13px] font-medium", on ? "text-accent-ink" : "text-ink")}>{ACCESS_INFO[a].label}</span>
                <span className="font-mono text-[11px] text-ink-3">{a}</span>
                {a === cur && <span className="ml-auto text-[11px] text-ink-3">{t("admin-pages-network.current")}</span>}
                {a !== cur && !allowed && !locked && <AdminIcon name="lock" size={12} className="ml-auto text-ink-3" />}
              </div>
              <div className="mt-1 text-[11.5px] leading-snug text-ink-2">{ACCESS_INFO[a].detail}</div>
              <div className="mt-1 text-[11px] leading-snug text-ink-3">{ACCESS_INFO[a].risk}</div>
            </button>
          );
        })}
      </div>
      {pick === "cidrs" && !locked && (
        <div className="px-4 pb-3">
          <Field
            label={t("admin-pages-network.cidrs")}
            hint={cur === "cidrs" ? t("admin-pages-network.cidrs_hint_narrow") : t("admin-pages-network.cidrs_hint")}
            value={cidrs}
            onChange={setCidrs}
            placeholder={"192.0.2.0/24\n2001:db8::/48"}
            mono
            multiline
            error={badCidr ? t("admin-pages-network.cidr_invalid", { value: badCidr }) : undefined}
          />
        </div>
      )}
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2 bg-inset px-4 py-2.5">
        <span className="min-w-0 flex-1 text-[12px] text-ink-2">
          {tRich(v.you.tls ? "admin-pages-network.you_from_tls" : "admin-pages-network.you_from", { address: <span className="font-mono text-ink">{v.you.address}</span> })}
          {dirty && lockout && <span className="text-red">{t("admin-pages-network.you_locked_out", { scope: ACCESS_INFO[pick].label })}</span>}
        </span>
        {error && <span className="w-full text-[12px] text-red sm:order-last">{error}</span>}
        {!locked && (
          <Button tone={dirty && lockout ? "danger" : "primary"} disabled={!dirty || Boolean(badCidr) || (pick === "cidrs" && list.length === 0)} busy={busy} onClick={() => (dirty && lockout ? setConfirm(true) : save())}>
            {t("admin-pages-network.apply")}
          </Button>
        )}
      </div>
      <Dialog
        open={confirm}
        onClose={() => setConfirm(false)}
        title={t("admin-pages-network.lockout_title")}
        footer={
          <>
            <Button tone="quiet" onClick={() => setConfirm(false)}>
              {t("common.cancel")}
            </Button>
            <Button tone="danger" busy={busy} onClick={() => save(true)}>
              {t("admin-pages-network.lockout_confirm")}
            </Button>
          </>
        }
      >
        <p className="text-[13px] leading-relaxed text-ink-2">
          {tRich("admin-pages-network.lockout_body", {
            address: <span className="font-mono text-ink">{v.you.address}</span>,
            scope: ACCESS_INFO[pick].label,
            command: <span className="font-mono">personad hub admin access</span>,
          })}
        </p>
      </Dialog>
    </Card>
  );
}

function PublicCard({ v, onSaved }: { v: NetworkView; onSaved: (v: NetworkView) => void }) {
  const [url, setUrl] = useState(v.public_url);
  const [tls, setTls] = useState(v.tls?.listen ?? "");
  const [adv, setAdv] = useState(v.advertise.join("\n"));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const dirty = url !== v.public_url || tls !== (v.tls?.listen ?? "") || lines(adv).join(",") !== v.advertise.join(",");
  const reset = () => {
    setUrl(v.public_url);
    setTls(v.tls?.listen ?? "");
    setAdv(v.advertise.join("\n"));
    setError("");
  };
  const save = async () => {
    setBusy(true);
    setError("");
    try {
      const r = await adminApi<NetworkView>("/network/public", "PUT", { public_url: url.trim(), tls_listen: tls.trim(), advertise: lines(adv) });
      toast(t("admin-pages-network.public_saved"), "ok");
      onSaved(r);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card title={t("admin-pages-network.public_title")} footer={t("admin-pages-network.public_footer")}>
      <div className="space-y-3.5 px-4 py-3.5">
        {v.public_url_from_env ? (
          <Field label={t("admin-pages-network.public_url")} hint={t("admin-pages-network.public_url_env")} value={url} onChange={() => {}} mono disabled />
        ) : (
          <Field label={t("admin-pages-network.public_url")} hint={t("admin-pages-network.public_url_hint")} value={url} onChange={setUrl} placeholder="http://hub.example.com:18660" mono />
        )}
        <Field
          label={t("admin-pages-network.tls_listen")}
          hint={v.tls ? t("admin-pages-network.tls_listen_on", { listen: v.tls.listen, fp: v.tls.fp.slice(0, 12) }) : t("admin-pages-network.tls_listen_hint")}
          value={tls}
          onChange={setTls}
          placeholder=":18662"
          mono
        />
        <Field label={t("admin-pages-network.advertise")} hint={t("admin-pages-network.advertise_hint")} value={adv} onChange={setAdv} placeholder="https://hub.example.com:18662" mono multiline />
        <div className="flex flex-wrap gap-x-4 gap-y-1 text-[11.5px] text-ink-3">
          <span>
            {tRich("admin-pages-network.advertise_ipv6", { value: <span className="text-ink-2">{v.advertise_ipv6 ? t("admin-pages-network.yes") : t("admin-pages-network.no")}</span> })}
          </span>
          <span>
            {tRich("admin-pages-network.advertise_lan", { value: <span className="text-ink-2">{v.advertise_lan ? t("admin-pages-network.yes") : t("admin-pages-network.no")}</span> })}
          </span>
          <span>
            {tRich("admin-pages-network.plain_listen", { value: <span className="font-mono text-ink-2">{v.listen}</span> })}
          </span>
        </div>
        {error && (
          <p className="text-[12px] text-red" role="alert">
            {error}
          </p>
        )}
      </div>
      <div className="flex items-center justify-end gap-2 bg-inset px-4 py-2.5">
        <Button tone="quiet" disabled={!dirty} onClick={reset}>
          {t("admin-pages-network.revert")}
        </Button>
        <Button tone="primary" disabled={!dirty} busy={busy} onClick={save}>
          {t("common.save")}
        </Button>
      </div>
    </Card>
  );
}

function PasswordCard() {
  const [cur, setCur] = useState("");
  const [pw, setPw] = useState("");
  const [again, setAgain] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const short = pw.length > 0 && [...pw].length < 8;
  const mismatch = again.length > 0 && again !== pw;
  const ok = cur && pw && !short && again === pw;
  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!ok) return;
    setBusy(true);
    setError("");
    try {
      await adminApi("/password", "POST", { current: cur, new: pw });
      setCur("");
      setPw("");
      setAgain("");
      toast(t("admin-pages-network.password_saved"), "ok");
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card title={t("admin-pages-network.password_title")} footer={t("admin-pages-network.password_footer")}>
      <form onSubmit={save}>
        <div className="grid gap-3 px-4 py-3.5 sm:grid-cols-3">
          <Field label={t("admin-pages-network.password_current")} type="password" value={cur} onChange={setCur} autoComplete="current-password" />
          <Field label={t("admin-pages-network.password_new")} type="password" value={pw} onChange={setPw} autoComplete="new-password" error={short ? t("admin-pages-network.password_short") : undefined} />
          <Field label={t("admin-pages-network.password_again")} type="password" value={again} onChange={setAgain} autoComplete="new-password" error={mismatch ? t("admin-pages-network.password_mismatch") : undefined} />
        </div>
        <div className="flex items-center justify-end gap-3 bg-inset px-4 py-2.5">
          {error && <span className="min-w-0 flex-1 text-[12px] text-red">{error}</span>}
          <Button type="submit" tone="primary" busy={busy} disabled={!ok}>
            <AdminIcon name="key" size={13} />
            {t("admin-pages-network.password_change")}
          </Button>
        </div>
      </form>
    </Card>
  );
}
