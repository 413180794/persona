// 设置 › 连接器 (iOS ConnectorsSection, docs/mail.md): where Persona's data comes from — the mail accounts (list, add
// one with a password or 授权码, delete), 飞书 (lark-connector through the hub, feature `lark`: read-only status), and the hub's other connectors
// (GitHub, Telegram, a plugin's: GET /api/v1/connectors), each with a page of its own (HubConnector.tsx, `&connector=`).
// 这台 iPhone (phone_* tools) is the phone's own and has no place in a browser; Gmail signs in with Google through the
// phone app (its OAuth client is an iOS one whose callback only the app can catch), so the web form leaves it to the phone.
import { useEffect, useState } from "react";
import { EMPTY_MAIL_FORM, MAIL_KINDS, mailAccountLine, mailErrors, mailRequest, mailSummary, type MailAccount, type MailForm } from "../../lib/agent-settings";
import { api, enc, errorText } from "../../lib/api";
import { ago } from "../../lib/format";
import { accountTitle, type LarkAccount, type LarkAccountList } from "../../lib/lark-accounts";
import { useLarkOn, useStore } from "../../lib/store";
import { Button, cx, Dialog, EASE, IconButton, Loading, toast } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";
import { Count, Monogram } from "../inbox/InboxParts";
import { ConnectorDetail, ConnectorList } from "./HubConnector";
import { LarkHomeSwitch } from "./LarkHomeSwitch";
import { Group, Pill } from "./SettingsParts";
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
        Persona 能读、能替你操作的数据来源：助手、AI 分身和 Agent 通过它们查日程、读消息、发邮件；替你出面的操作都会先问你。
        手机上的「这台 iPhone」（通讯录、日历、提醒、照片、健康、位置）只在手机 App 里打开，网页上没有。
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
      toast(`已删除 ${a.email}`, "ok");
      setConfirm(null);
      await load();
    } catch (e) {
      toast(`删除失败：${errorText(e)}`, "error");
    } finally {
      setDeleting(null);
    }
  };

  return (
    <>
      <Group
        title={
          <span className="flex items-center gap-2">
            邮箱
            {accounts && <span className="font-normal">{mailSummary(accounts)}</span>}
          </span>
        }
        right={
          <Button tone="quiet" icon="plus" onClick={() => setAdding(true)} className="-mb-1 h-7">
            添加邮箱
          </Button>
        }
        footer="账号存在 hub 上，密码加密保存、不会再返回；收件箱、助手和 AI 分身共用。也可以直接在 Persona 里说「帮我添加邮箱」。"
      >
        {!accounts && !error && (
          <div className="px-4 py-3">
            <Loading />
          </div>
        )}
        {error && <p className="px-4 py-3 text-[12.5px] text-red">读不到邮箱：{error}</p>}
        {accounts?.length === 0 && <p className="px-4 py-3 text-[12.5px] text-ink-3">还没有邮箱。</p>}
        {accounts?.map((a) => (
          <div key={a.id} className="flex items-center gap-3 px-4 py-2.5">
            <span className={cx("flex size-8 shrink-0 items-center justify-center rounded-[8px]", a.status === "error" ? "bg-red-tint text-red" : "bg-accent-tint text-accent-ink")}>
              <Icon name="mail" size={15} />
            </span>
            <div className="min-w-0 flex-1">
              <div className="flex min-w-0 items-center gap-2">
                <span className="truncate text-[13px] text-ink">{a.email}</span>
                {a.provider === "gmail" && <Pill>Gmail</Pill>}
                {a.provider === "feishu" && <Pill>飞书</Pill>}
                {a.status === "error" && <Pill tone="red">有错误</Pill>}
              </div>
              <div className={cx("mt-0.5 truncate text-[11.5px]", a.status === "error" ? "text-red" : "text-ink-3")} title={mailAccountLine(a)}>
                {mailAccountLine(a)}
              </div>
            </div>
            <span className="relative">
              <IconButton icon="trash" label={`删除 ${a.email}`} onClick={() => setConfirm(confirm === a.id ? null : a.id)} active={confirm === a.id} />
              {confirm === a.id && (
                <span className="absolute top-full right-0 z-10 mt-1 flex items-center gap-1 rounded-control bg-surface p-1 shadow-overlay" style={{ animation: `pop-in 160ms ${EASE} both` }}>
                  <Button tone="quiet" onClick={() => setConfirm(null)}>
                    取消
                  </Button>
                  <Button tone="danger" busy={deleting === a.id} onClick={() => void remove(a)}>
                    确认删除
                  </Button>
                </span>
              )}
            </span>
          </div>
        ))}
      </Group>
      <AddMail
        open={adding}
        onClose={() => setAdding(false)}
        onAdded={(email) => {
          setAdding(false);
          toast(`已添加 ${email}`, "ok");
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
      title="添加邮箱"
      footer={
        <>
          <Button tone="quiet" onClick={onClose} disabled={busy}>
            取消
          </Button>
          {f.kind !== "gmail" && (
            <Button tone="primary" busy={busy} onClick={() => void save()}>
              {busy ? "正在连接" : "连接"}
            </Button>
          )}
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <div className="flex gap-0.5 rounded-control bg-inset p-0.5 shadow-hairline" role="radiogroup" aria-label="邮箱类型">
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
              <p className="font-medium text-ink">在手机上添加 Gmail</p>
              <p className="mt-1">
                Gmail 用「使用 Google 账号登录」，不填密码。Google 授权完会跳回 iPhone 上的 Persona App，网页接不到这一步。
                在手机上打开 设置 › 连接器 › 邮箱 › 添加邮箱 › Gmail；加好后这里和收件箱都能看到。
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
            {field("email", "邮箱地址", { placeholder: "me@example.com", type: "email", auto: "email" })}
            {field("password", f.kind === "feishu" ? "密码（第三方客户端登录密码）" : "密码 / 授权码", { type: "password", auto: "new-password" })}
            <p className="text-[11.5px] leading-relaxed text-ink-3">
              {f.kind === "feishu"
                ? "飞书邮箱：服务器固定为 imap.larkoffice.com:993、smtp.larkoffice.com:465（SSL），只填邮箱和密码。密码是飞书邮箱设置里的「第三方客户端登录密码」。"
                : "QQ / 163 / Outlook / iCloud 等会按域名自动填服务器；国内邮箱一般要用「授权码」而不是登录密码。"}
            </p>
            {f.kind === "imap" && (
              <fieldset className="flex flex-col gap-3 rounded-[10px] bg-inset p-3 shadow-hairline">
                <legend className="sr-only">服务器</legend>
                <p className="text-[12px] font-medium text-ink-2">服务器（可留空自动识别）</p>
                <div className="grid grid-cols-[1fr_88px] gap-2">
                  {field("imapHost", "IMAP 服务器", { placeholder: "imap.example.com", mono: true })}
                  {field("imapPort", "端口", { placeholder: "993", mono: true })}
                  {field("smtpHost", "SMTP 服务器", { placeholder: "smtp.example.com", mono: true })}
                  {field("smtpPort", "端口", { placeholder: "465", mono: true })}
                </div>
                <p className="text-[11px] text-ink-3">SMTP 用 587 时走 STARTTLS，其他端口走 SSL。</p>
              </fieldset>
            )}
            {error && <p className="rounded-[8px] bg-red-tint px-2.5 py-1.5 text-[12px] break-words text-red">没有加上：{error}</p>}
            <button type="submit" hidden />
          </form>
        )}
      </div>
    </Dialog>
  );
}

// ── 飞书 ────────────────────────────────────────────────

const USES: { icon: IconName; title: string; detail: string }[] = [
  { icon: "inbox", title: "收件箱", detail: "私聊、@我、群消息和云文档里提到你的地方，AI 先分拣、起草回复" },
  { icon: "book", title: "知识库", detail: "会话、人、群、项目归档下来，给助手和 AI 分身做上下文" },
  { icon: "spark", title: "助手", detail: "Persona 能搜消息、读文档；发消息等写操作先在手机上问你" },
  { icon: "agent", title: "/agent", detail: "在飞书任何地方以 /agent 开头说一句，就起一个 Agent 处理并替你回复" },
];

function LarkAccountRow({ a, isDefault }: { a: LarkAccount; isDefault: boolean }) {
  const detail = a.quarantined ? "无法打开，请在 lark-connector 里重试或移除" : !a.signed_in ? "未登录" : a.tenant || "已登录";
  return (
    <div data-account={a.id} className="flex items-center gap-3 px-4 py-2.5">
      <Monogram name={accountTitle(a)} size={30} src={a.avatar} />
      <div className="min-w-0 flex-1">
        <div className="truncate text-[13px] text-ink">{accountTitle(a)}</div>
        <div className={cx("mt-0.5 truncate text-[11.5px]", a.signed_in ? "text-ink-3" : "text-red")}>{detail}</div>
      </div>
      {isDefault && <Pill>默认</Pill>}
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

  const state = !st ? (loading ? "正在检查…" : "读不到状态") : !st.configured ? "未配置" : st.connected ? "已连接" : "连不上 lark-connector";
  const dot = !st ? "bg-line-strong" : st.connected ? "bg-green" : st.configured ? "bg-red" : "bg-line-strong";
  const last = st?.last_event_at ? ago(new Date(st.last_event_at * 1000).toISOString()) : "";

  return (
    <Group
      title="飞书"
      footer={st?.configured === false ? "hub 还没接飞书：在 hub 所在的机器上运行 lark-connector，并在 hub.json 的 lark 里填上它（docs/feishu.md）。" : "飞书账号在 hub 所在机器的 lark-connector 里登录，这里只看连接状态。"}
    >
      <div className="flex items-center gap-3 px-4 py-3">
        <span className="flex size-9 shrink-0 items-center justify-center rounded-[9px] bg-accent-tint text-accent-ink">
          <Icon name="chat" size={16} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="truncate text-[13.5px] font-medium text-ink">{st?.name || "飞书"}</div>
          <div className="mt-0.5 flex items-center gap-1.5 text-[11.5px] text-ink-3">
            <span className={cx("size-1.5 shrink-0 rounded-full", dot)} />
            <span className="truncate">{state}</span>
          </div>
        </div>
        <IconButton icon="retry" label="重新检查" onClick={load} disabled={loading} />
      </div>
      {last && (
        <div className="flex items-center gap-3 px-4 py-2.5">
          <span className="min-w-0 flex-1 truncate text-[13px] text-ink">最近一条实时消息</span>
          <span className="shrink-0 text-[12.5px] text-ink-2">{last}</span>
        </div>
      )}
      {(st?.error || error) && <p className="px-4 py-2.5 text-[12px] break-words text-red">{st?.error || `读不到飞书的状态：${error}`}</p>}
      {accounts && accounts.accounts.length > 1 && (
        <>
          <div className="px-4 pt-2.5 pb-1 text-[11.5px] font-medium text-ink-3">账号</div>
          {accounts.accounts.map((a) => <LarkAccountRow key={a.id} a={a} isDefault={a.id === accounts.default} />)}
          <p className="px-4 pt-1 pb-2.5 text-[11.5px] leading-snug text-ink-3">已登录的账号都会收进收件箱，「消息」页可以切换账号。/agent、助手和知识库使用默认账号；在 lark-connector 里添加或退出账号。</p>
        </>
      )}
      <LarkHomeSwitch />
      <div className="px-4 pt-2.5 pb-1 text-[11.5px] font-medium text-ink-3">Persona 用飞书做什么</div>
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
