// 设置 › 连接器 (iOS ConnectorsSection, docs/mail.md): where Persona's data comes from — the mail accounts (list, add
// one with a password or 授权码, delete), 飞书 (lark-connector through the hub, feature `lark`: read-only status), and the hub's other connectors
// (GitHub, Telegram, an extension's: GET /api/v1/connectors), each with a page of its own (HubConnector.tsx, `&connector=`).
// 这台 iPhone (phone_* tools) is the phone's own and has no place in a browser; Gmail signs in with Google through the
// phone app (its OAuth client is an iOS one whose callback only the app can catch), so the web form leaves it to the phone.
import { useEffect, useState } from "react";
import { EMPTY_MAIL_FORM, MAIL_KINDS, mailAccountLine, mailErrors, mailRequest, mailSummary, type MailAccount, type MailForm } from "../../lib/agent-settings";
import { t } from "../../i18n";
import { api, enc, errorText } from "../../lib/api";
import { ago } from "../../lib/format";
import { accountTitle, type LarkAccount, type LarkAccountList } from "../../lib/lark-accounts";
import { useLarkOn, useStore } from "../../lib/store";
import { Button, cx, Dialog, IconButton, Loading, toast } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";
import { Count, Monogram } from "../inbox/InboxParts";
import { ConnectorDetail, ConnectorList } from "./HubConnector";
import { LarkDisplaySettings } from "./LarkDisplaySettings";
import { LarkHomeSwitch } from "./LarkHomeSwitch";
import { Group, Pill, RowConfirm } from "./SettingsParts";
import { FeishuBot } from "./FeishuBot";

interface LarkStatus {
  configured: boolean;
  connected: boolean;
  name?: string;
  error?: string;
  last_event_at?: number;
}

export function Connectors({ open, onOpen, onChannel }: { open: string | null; onOpen: (id: string | null) => void; onChannel?: (id: string) => void }) {
  // An older hub has no connectors API (feature "connectors"): only mail and Feishu, as before.
  const hasConnectors = useStore((s) => s.features.includes("connectors"));
  // 飞书 (lark-connector through /api/v1/lark/…) only on a hub that has it (feature `lark`); 飞书机器人 is the hub's own.
  const lark = useLarkOn();
  // "feishu-bot" is not a hub connector: FeishuBot reads it and opens its own dialog.
  if (open && open !== "feishu-bot") return <ConnectorDetail key={open} id={open} onBack={() => onOpen(null)} onChannel={onChannel} />;
  return (
    <>
      <p className="mb-5 px-1 text-[12.5px] leading-relaxed text-ink-3">
        {t("settings-connectors.intro")}
      </p>
      <Mail />
      {lark && <Feishu />}
      <FeishuBot onChannel={onChannel} />
      {hasConnectors && <ConnectorList onOpen={onOpen} />}
    </>
  );
}

// ── 邮箱 ────────────────────────────────────────────────

function Mail() {
  const [accounts, setAccounts] = useState<MailAccount[] | null>(null);
  const [error, setError] = useState("");
  const [adding, setAdding] = useState(false);
  const [confirm, setConfirm] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<string | null>(null);

  const load = () =>
    api<{ accounts: MailAccount[] }>("/mail/accounts")
      .then((r) => (setAccounts(r.accounts ?? []), setError("")))
      .catch((e) => setError(errorText(e)));
  useEffect(() => void load(), []);

  const remove = async (a: MailAccount) => {
    setDeleting(a.id);
    try {
      await api(`/mail/accounts/${enc(a.id)}`, { method: "DELETE" });
      toast(t("settings-connectors.mail_deleted", { email: a.email }), "ok");
      setConfirm(null);
      await load();
    } catch (e) {
      toast(t("settings-connectors.delete_failed", { error: errorText(e) }), "error");
    } finally {
      setDeleting(null);
    }
  };

  return (
    <>
      <Group
        title={
          <span className="flex items-center gap-2">
            {t("settings-connectors.mail")}
            {accounts && <span className="font-normal">{mailSummary(accounts)}</span>}
          </span>
        }
        right={
          <Button tone="quiet" icon="plus" onClick={() => setAdding(true)} className="-mb-1 h-7">
            {t("settings-connectors.add_mail")}
          </Button>
        }
        footer={t("settings-connectors.mail_footer")}
      >
        {!accounts && !error && (
          <div className="px-4 py-3">
            <Loading />
          </div>
        )}
        {error && <p className="px-4 py-3 text-[12.5px] text-red">{t("settings-connectors.mail_load_failed", { error })}</p>}
        {accounts?.length === 0 && <p className="px-4 py-3 text-[12.5px] text-ink-3">{t("settings-connectors.mail_empty")}</p>}
        {accounts?.map((a) => (
          <div key={a.id} className="flex items-center gap-3 px-4 py-2.5">
            <span className={cx("flex size-8 shrink-0 items-center justify-center rounded-[8px]", a.status === "error" ? "bg-red-tint text-red" : "bg-accent-tint text-accent-ink")}>
              <Icon name="mail" size={15} />
            </span>
            <div className="min-w-0 flex-1">
              <div className="flex min-w-0 items-center gap-2">
                <span className="truncate text-[13px] text-ink">{a.email}</span>
                {a.provider === "gmail" && <Pill>Gmail</Pill>}
                {a.provider === "feishu" && <Pill>{t("settings-connectors.lark")}</Pill>}
                {a.status === "error" && <Pill tone="red">{t("settings-connectors.has_error")}</Pill>}
              </div>
              <div className={cx("mt-0.5 truncate text-[11.5px]", a.status === "error" ? "text-red" : "text-ink-3")} title={mailAccountLine(a)}>
                {mailAccountLine(a)}
              </div>
            </div>
            <RowConfirm
              open={confirm === a.id}
              confirm={t("settings-connectors.confirm_delete")}
              busy={deleting === a.id}
              onCancel={() => setConfirm(null)}
              onConfirm={() => void remove(a)}
              trigger={<IconButton icon="trash" label={t("settings-connectors.delete_account", { email: a.email })} onClick={() => setConfirm(a.id)} />}
            />
          </div>
        ))}
      </Group>
      <AddMail
        open={adding}
        onClose={() => setAdding(false)}
        onAdded={(email) => {
          setAdding(false);
          toast(t("settings-connectors.mail_added", { email }), "ok");
          void load();
        }}
      />
    </>
  );
}

const fieldClass = "h-9 w-full rounded-control bg-field px-3 text-[13px] text-ink outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)] disabled:opacity-60";

// 添加邮箱 (iOS AddMailAccountView): 飞书 or any IMAP server with a password / 授权码; the hub logs in to IMAP and SMTP
// before it saves, so a wrong password comes back as an error here and nothing is stored.
function AddMail({ open, onClose, onAdded }: { open: boolean; onClose: () => void; onAdded: (email: string) => void }) {
  const [f, setF] = useState<MailForm>(EMPTY_MAIL_FORM);
  const [tried, setTried] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    if (open) {
      setF(EMPTY_MAIL_FORM);
      setTried(false);
      setError("");
    }
  }, [open]);

  const errors = mailErrors(f);
  const req = mailRequest(f);
  const shown = tried ? errors : {};
  const set = (k: keyof MailForm) => (e: React.ChangeEvent<HTMLInputElement>) => setF((x) => ({ ...x, [k]: e.target.value }));

  const save = async () => {
    setTried(true);
    if (!req || Object.keys(errors).length) return;
    setBusy(true);
    setError("");
    try {
      await api("/mail/accounts", { method: "POST", json: req });
      onAdded(req.email);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const field = (k: keyof MailForm, label: string, opts: { placeholder?: string; type?: string; mono?: boolean; auto?: string } = {}) => (
    <label className="block min-w-0">
      <span className="mb-1 block text-[12px] font-medium text-ink-2">{label}</span>
      <input
        value={f[k]}
        onChange={set(k)}
        type={opts.type ?? "text"}
        placeholder={opts.placeholder}
        autoComplete={opts.auto ?? "off"}
        spellCheck={false}
        disabled={busy}
        aria-invalid={!!shown[k]}
        className={cx(fieldClass, opts.mono && "font-mono", shown[k] ? "shadow-[inset_0_0_0_1px_var(--red)]" : "shadow-inset-field")}
      />
      {shown[k] && <span className="mt-1 block text-[11px] text-red">{shown[k]}</span>}
    </label>
  );

  return (
    <Dialog
      open={open}
      onClose={() => !busy && onClose()}
      width={460}
      title={t("settings-connectors.add_mail_title")}
      footer={
        <>
          <Button tone="quiet" onClick={onClose} disabled={busy}>
            {t("common.cancel")}
          </Button>
          {f.kind !== "gmail" && (
            <Button tone="primary" busy={busy} onClick={() => void save()}>
              {busy ? t("settings-connectors.connecting") : t("settings-connectors.connect")}
            </Button>
          )}
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <div className="flex gap-0.5 rounded-control bg-inset p-0.5 shadow-hairline" role="radiogroup" aria-label={t("settings-connectors.mail_kind")}>
          {MAIL_KINDS.map((k) => (
            <button
              key={k.k}
              type="button"
              role="radio"
              aria-checked={f.kind === k.k}
              disabled={busy}
              onClick={() => (setF((x) => ({ ...x, kind: k.k })), setTried(false), setError(""))}
              className={cx(
                "flex h-7 flex-1 items-center justify-center whitespace-nowrap rounded-[7px] px-2.5 text-[12.5px] transition-[background-color,color] duration-150",
                f.kind === k.k ? "bg-surface font-medium text-ink shadow-btn" : "text-ink-3 hover:text-ink-2",
              )}
            >
              {k.label}
            </button>
          ))}
        </div>

        {f.kind === "gmail" ? (
          <div className="flex gap-3 rounded-[10px] bg-inset px-3.5 py-3 shadow-hairline">
            <Icon name="info" size={15} className="mt-0.5 shrink-0 text-ink-3" />
            <div className="text-[12.5px] leading-relaxed text-ink-2">
              <p className="font-medium text-ink">{t("settings-connectors.gmail_title")}</p>
              <p className="mt-1">
                {t("settings-connectors.gmail_body")}
              </p>
            </div>
          </div>
        ) : (
          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              void save();
            }}
          >
            {field("email", t("settings-connectors.email"), { placeholder: "me@example.com", type: "email", auto: "email" })}
            {field("password", f.kind === "feishu" ? t("settings-connectors.password_lark") : t("settings-connectors.password"), { type: "password", auto: "new-password" })}
            <p className="text-[11.5px] leading-relaxed text-ink-3">
              {f.kind === "feishu"
                ? t("settings-connectors.lark_hint")
                : t("settings-connectors.imap_hint")}
            </p>
            {f.kind === "imap" && (
              <fieldset className="flex flex-col gap-3 rounded-[10px] bg-inset p-3 shadow-hairline">
                <legend className="sr-only">{t("settings-connectors.servers")}</legend>
                <p className="text-[12px] font-medium text-ink-2">{t("settings-connectors.servers_optional")}</p>
                <div className="grid grid-cols-[1fr_88px] gap-2">
                  {field("imapHost", t("settings-connectors.imap_server"), { placeholder: "imap.example.com", mono: true })}
                  {field("imapPort", t("settings-connectors.port"), { placeholder: "993", mono: true })}
                  {field("smtpHost", t("settings-connectors.smtp_server"), { placeholder: "smtp.example.com", mono: true })}
                  {field("smtpPort", t("settings-connectors.port"), { placeholder: "465", mono: true })}
                </div>
                <p className="text-[11px] text-ink-3">{t("settings-connectors.smtp_tls")}</p>
              </fieldset>
            )}
            {error && <p className="rounded-[8px] bg-red-tint px-2.5 py-1.5 text-[12px] break-words text-red">{t("settings-connectors.add_failed", { error })}</p>}
            <button type="submit" hidden />
          </form>
        )}
      </div>
    </Dialog>
  );
}

// ── 飞书 ────────────────────────────────────────────────

const USES: { icon: IconName; title: string; detail: string }[] = [
  { icon: "inbox", title: t("settings-connectors.use_inbox"), detail: t("settings-connectors.use_inbox_detail") },
  { icon: "book", title: t("settings-connectors.use_knowledge"), detail: t("settings-connectors.use_knowledge_detail") },
  { icon: "spark", title: t("settings-connectors.use_assistant"), detail: t("settings-connectors.use_assistant_detail") },
  { icon: "agent", title: "/agent", detail: t("settings-connectors.use_agent_detail") },
];

function LarkAccountRow({ a, isDefault }: { a: LarkAccount; isDefault: boolean }) {
  const detail = a.quarantined ? t("settings-connectors.account_quarantined") : !a.signed_in ? t("settings-connectors.signed_out") : a.tenant || t("settings-connectors.signed_in");
  return (
    <div data-account={a.id} className="flex items-center gap-3 px-4 py-2.5">
      <Monogram name={accountTitle(a)} size={30} src={a.avatar} />
      <div className="min-w-0 flex-1">
        <div className="truncate text-[13px] text-ink">{accountTitle(a)}</div>
        <div className={cx("mt-0.5 truncate text-[11.5px]", a.signed_in ? "text-ink-3" : "text-red")}>{detail}</div>
      </div>
      {isDefault && <Pill>{t("settings-connectors.default")}</Pill>}
      {a.signed_in && a.unread > 0 && <Count n={a.unread} />}
    </div>
  );
}

function Feishu() {
  const [st, setSt] = useState<LarkStatus | null>(null);
  const [accounts, setAccounts] = useState<LarkAccountList | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const load = () => {
    setLoading(true);
    // the Feishu accounts on the hub's lark-connector (docs/feishu.md「多账号」); a hub from before them has none to list
    api<LarkAccountList>("/lark/accounts").then(setAccounts).catch(() => setAccounts(null));
    api<LarkStatus>("/lark/status")
      .then((r) => (setSt(r), setError("")))
      .catch((e) => setError(errorText(e)))
      .finally(() => setLoading(false));
  };
  useEffect(load, []);

  const state = !st ? (loading ? t("settings-connectors.checking") : t("settings-connectors.status_unknown")) : !st.configured ? t("settings-connectors.not_configured") : st.connected ? t("settings-connectors.connected") : t("settings-connectors.unreachable");
  const dot = !st ? "bg-line-strong" : st.connected ? "bg-green" : st.configured ? "bg-red" : "bg-line-strong";
  const last = st?.last_event_at ? ago(new Date(st.last_event_at * 1000).toISOString()) : "";

  return (
    <Group
      title={t("settings-connectors.lark")}
      footer={st?.configured === false ? t("settings-connectors.lark_footer_unconfigured") : t("settings-connectors.lark_footer")}
    >
      <div className="flex items-center gap-3 px-4 py-3">
        <span className="flex size-9 shrink-0 items-center justify-center rounded-[9px] bg-accent-tint text-accent-ink">
          <Icon name="chat" size={16} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="truncate text-[13.5px] font-medium text-ink">{st?.name || t("settings-connectors.lark")}</div>
          <div className="mt-0.5 flex items-center gap-1.5 text-[11.5px] text-ink-3">
            <span className={cx("size-1.5 shrink-0 rounded-full", dot)} />
            <span className="truncate">{state}</span>
          </div>
        </div>
        <IconButton icon="retry" label={t("settings-connectors.recheck")} onClick={load} disabled={loading} />
      </div>
      {last && (
        <div className="flex items-center gap-3 px-4 py-2.5">
          <span className="min-w-0 flex-1 truncate text-[13px] text-ink">{t("settings-connectors.last_event")}</span>
          <span className="shrink-0 text-[12.5px] text-ink-2">{last}</span>
        </div>
      )}
      {(st?.error || error) && <p className="px-4 py-2.5 text-[12px] break-words text-red">{st?.error || t("settings-connectors.lark_status_failed", { error })}</p>}
      {accounts && accounts.accounts.length > 1 && (
        <>
          <div className="px-4 pt-2.5 pb-1 text-[11.5px] font-medium text-ink-3">{t("settings-connectors.accounts")}</div>
          {accounts.accounts.map((a) => <LarkAccountRow key={a.id} a={a} isDefault={a.id === accounts.default} />)}
          <p className="px-4 pt-1 pb-2.5 text-[11.5px] leading-snug text-ink-3">{t("settings-connectors.accounts_note")}</p>
        </>
      )}
      <LarkHomeSwitch />
      <LarkDisplaySettings />
      <div className="px-4 pt-2.5 pb-1 text-[11.5px] font-medium text-ink-3">{t("settings-connectors.lark_uses")}</div>
      {USES.map((u) => (
        <div key={u.title} className="flex items-start gap-3 px-4 py-2.5">
          <Icon name={u.icon} size={15} className="mt-0.5 shrink-0 text-accent" />
          <div className="min-w-0">
            <div className="text-[13px] text-ink">{u.title}</div>
            <div className="mt-0.5 text-[11.5px] leading-snug text-ink-3">{u.detail}</div>
          </div>
        </div>
      ))}
    </Group>
  );
}
