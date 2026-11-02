// 网络与访问: how the hub is reached (public URL, TLS, advertised addresses — restart to apply), the security settings
// (read only), who may open this admin UI (narrow only: widening is the hub machine's call), the admin password.
import { useEffect, useState } from "react";
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
      <PageHeader title="网络与访问" subtitle="hub 怎么被访问，以及谁能打开这个管理界面。" />
      <AccessCard v={data} onSaved={update} />
      <PublicCard v={data} onSaved={update} />
      <Card title="候选地址" footer="自动发现的 IPv6 / 局域网地址加上面配置的地址；重启后按新配置重新发现。">
        <Endpoints list={data.endpoints} />
      </Card>
      <Card title="安全" footer="这些会影响 App 的信任判断，只能在 hub 机器上改 hub.json 的 security。">
        <Line label="受信网段" hint="security.trusted_cidrs：命中的来源按局域网处理">
          {data.security.trusted_cidrs.length ? (
            <span className="flex flex-wrap justify-end gap-1">
              {data.security.trusted_cidrs.map((c) => (
                <Tag key={c}>
                  <span className="font-mono">{c}</span>
                </Tag>
              ))}
            </span>
          ) : (
            <span className="text-ink-3">没有</span>
          )}
        </Line>
        <Line label="公网明文" hint="security.allow_public_plaintext：允许公网经明文 HTTP 访问 App 接口">
          {data.security.allow_public_plaintext ? <Tag tone="red">已开启</Tag> : <Tag tone="green">已关闭</Tag>}
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
      toast("访问范围已改，立刻生效", "ok");
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
      title="管理界面的访问范围"
      right={<Tag tone={cur === "any" ? "orange" : "gray"}>{ACCESS_INFO[cur].label}</Tag>}
      footer={
        <>
          范围外的人打开 /admin 和打不存在的地址一样，看不出有管理界面。这里只能收窄；放宽到更大的范围只能在 hub 机器上运行{" "}
          <span className="font-mono">jarvisd hub admin access &lt;范围&gt;</span>，或者用部署脚本。
        </>
      }
    >
      {locked && (
        <div className="px-4 pt-3">
          <Notice tone="gray" icon="lock" title="由部署时的 JARVIS_ADMIN_ACCESS 决定">
            这台 hub 的环境变量指定了访问范围，这里不能改。改 .env（或 systemd 的 Environment）里的值后重启 hub。
          </Notice>
        </div>
      )}
      <div role="radiogroup" aria-label="访问范围" className="grid gap-2 p-3 sm:grid-cols-2">
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
              title={!allowed && !locked ? "比现在的范围大，只能在 hub 机器上放宽" : undefined}
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
                {a === cur && <span className="ml-auto text-[11px] text-ink-3">当前</span>}
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
            label="网段"
            hint={cur === "cidrs" ? "一行一个。只能删掉网段或换成更小的。" : "一行一个，例如 192.0.2.0/24 或 VPN 的网段。"}
            value={cidrs}
            onChange={setCidrs}
            placeholder={"192.0.2.0/24\n2001:db8::/48"}
            mono
            multiline
            error={badCidr ? `「${badCidr}」不是网段` : undefined}
          />
        </div>
      )}
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2 bg-inset px-4 py-2.5">
        <span className="min-w-0 flex-1 text-[12px] text-ink-2">
          你现在从 <span className="font-mono text-ink">{v.you.address}</span>
          {v.you.tls ? "（TLS）" : ""} 访问
          {dirty && lockout && <span className="text-red">；改成「{ACCESS_INFO[pick].label}」后你就进不来了</span>}
        </span>
        {error && <span className="w-full text-[12px] text-red sm:order-last">{error}</span>}
        {!locked && (
          <Button tone={dirty && lockout ? "danger" : "primary"} disabled={!dirty || Boolean(badCidr) || (pick === "cidrs" && list.length === 0)} busy={busy} onClick={() => (dirty && lockout ? setConfirm(true) : save())}>
            应用
          </Button>
        )}
      </div>
      <Dialog
        open={confirm}
        onClose={() => setConfirm(false)}
        title="改完你就进不来了"
        footer={
          <>
            <Button tone="quiet" onClick={() => setConfirm(false)}>
              取消
            </Button>
            <Button tone="danger" busy={busy} onClick={() => save(true)}>
              仍然修改
            </Button>
          </>
        }
      >
        <p className="text-[13px] leading-relaxed text-ink-2">
          你现在的地址 <span className="font-mono text-ink">{v.you.address}</span> 不在「{ACCESS_INFO[pick].label}」里。改完这个页面会变成打不开；要再放宽，只能到 hub 机器上运行{" "}
          <span className="font-mono">jarvisd hub admin access</span>。
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
      toast("已保存，重启 hub 后生效", "ok");
      onSaved(r);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card title="公网和 TLS" footer="保存后写进 hub.json，重启 hub 才生效。只填地址不会开端口、放行防火墙或更新 DNS。">
      <div className="space-y-3.5 px-4 py-3.5">
        {v.public_url_from_env ? (
          <Field label="对外地址（public_url）" hint="由部署时的 JARVIS_PUBLIC_URL 决定（Docker 的 .env），在那里改" value={url} onChange={() => {}} mono disabled />
        ) : (
          <Field label="对外地址（public_url）" hint="配对二维码里的地址，旧客户端也用它" value={url} onChange={setUrl} placeholder="http://hub.example.com:18660" mono />
        )}
        <Field
          label="TLS 监听（public.tls_listen）"
          hint={v.tls ? `正在监听 ${v.tls.listen}，指纹 ${v.tls.fp.slice(0, 12)}…；留空关掉 TLS（jarvis-link、互联都要它）` : "例如 :18662。开启后公网和互联只走这个端口；第一次开会生成自签证书"}
          value={tls}
          onChange={setTls}
          placeholder=":18662"
          mono
        />
        <Field label="额外候选地址（public.advertise）" hint="一行一个，推荐完整的 HTTPS URL，例如用 DDNS 维护的域名" value={adv} onChange={setAdv} placeholder="https://hub.example.com:18662" mono multiline />
        <div className="flex flex-wrap gap-x-4 gap-y-1 text-[11.5px] text-ink-3">
          <span>
            自动发布 IPv6：<span className="text-ink-2">{v.advertise_ipv6 ? "开" : "关"}</span>
          </span>
          <span>
            自动发布局域网地址：<span className="text-ink-2">{v.advertise_lan ? "开" : "关"}</span>
          </span>
          <span>
            明文监听：<span className="font-mono text-ink-2">{v.listen}</span>
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
          还原
        </Button>
        <Button tone="primary" disabled={!dirty} busy={busy} onClick={save}>
          保存
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
      toast("密码已改，别处登录的都已退出", "ok");
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card title="管理员密码" footer="改密码后，别的浏览器上的登录全部失效。忘了密码在 hub 机器上运行 jarvisd hub admin reset-password。">
      <form onSubmit={save}>
        <div className="grid gap-3 px-4 py-3.5 sm:grid-cols-3">
          <Field label="现在的密码" type="password" value={cur} onChange={setCur} autoComplete="current-password" />
          <Field label="新密码" type="password" value={pw} onChange={setPw} autoComplete="new-password" error={short ? "至少 8 个字符" : undefined} />
          <Field label="再输一遍" type="password" value={again} onChange={setAgain} autoComplete="new-password" error={mismatch ? "两次不一样" : undefined} />
        </div>
        <div className="flex items-center justify-end gap-3 bg-inset px-4 py-2.5">
          {error && <span className="min-w-0 flex-1 text-[12px] text-red">{error}</span>}
          <Button type="submit" tone="primary" busy={busy} disabled={!ok}>
            <AdminIcon name="key" size={13} />
            改密码
          </Button>
        </div>
      </form>
    </Card>
  );
}
