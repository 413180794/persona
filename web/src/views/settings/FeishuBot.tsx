// 设置 › 连接器 › 飞书机器人 (docs/bots.md, the app's FeishuBotView): talk to Persona from Feishu. A row on the
// connectors page with the bot's state; its page (a dialog) has the status and the on / off switch, the app's
// credentials, pairing with a code sent to the bot, what the bot may do, the agents driven there, and how to make one.
// Each change goes to the hub at once and the page shows what the hub answered; a failed one leaves it as it was.
import { useCallback, useEffect, useState } from "react";
import { errorText } from "../../lib/api";
import { ago } from "../../lib/format";
import {
  botStateText, botStatus, botSummary, botTone, credentialsChanged, credentialsOf, credentialsUpdate, DEFAULT_FEATURES, livePairing, pairBot, pairingLeft,
  testBot, unpairBot, updateBot, withFeature, type BotCredentials, type BotFeatures, type FeishuBotStatus,
} from "../../lib/feishu-bot";
import { Button, copyText, cx, Dialog, IconButton, Spinner, toast } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";
import { Group, Pill, Row, Switch } from "./SettingsParts";

const controlClass = "h-8 rounded-control bg-field px-2.5 text-[12.5px] text-ink shadow-inset-field outline-none focus:shadow-[inset_0_0_0_1px_var(--accent)]";
const dotClass = { green: "bg-green", red: "bg-red", orange: "bg-orange" } as const;

const FEATURES: { key: keyof BotFeatures; icon: IconName; title: string; detail: string }[] = [
  { key: "main", icon: "spark", title: "和 Persona 对话", detail: "消息发进主对话（/new、/list 换一段），回答发回飞书" },
  { key: "cards", icon: "hand", title: "提问和批准", detail: "Persona 和飞书里驱动的 Agent 要你回答、批准时，在飞书里发卡片，点一下就行" },
  { key: "all_cards", icon: "bell", title: "所有 Agent 的提问", detail: "各台电脑上所有 Agent 的提问和批准都发到飞书" },
  { key: "pushes", icon: "send", title: "推送也发到飞书", detail: "Persona 的通知、定时任务的结果也发一份到飞书" },
  { key: "sync", icon: "loop", title: "同步主对话", detail: "在 App 里、定时任务在主对话里的回答，也发到飞书" },
  { key: "agents", icon: "agent", title: "驱动 Agent", detail: "/agents 选各台电脑上的 Claude Code / Codex，在飞书里和它聊；/start 开新的" },
];

const STEPS = [
  "在飞书开放平台建一个企业自建应用，开启「机器人」能力",
  "权限里开通：读取与发送单聊消息（im:message、im:message:send_as_bot）、获取单聊中发给机器人的消息、上传和下载消息里的资源；能读通讯录名字更好（contact:user.base:readonly）",
  "「事件与回调」里选「使用长连接接收事件」，订阅「接收消息」和「卡片回传交互」",
  "发布版本（公司里可能要管理员审核），把 App ID、App Secret 填到上面",
];

/** The bot's state, kept by whoever shows it (the row and its page share one). */
function useBot() {
  const [status, setStatus] = useState<FeishuBotStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const load = useCallback(() => {
    setLoading(true);
    return botStatus()
      .then((s) => (setStatus(s), setError("")))
      .catch((e) => setError(`读不到机器人的状态：${errorText(e)}`))
      .finally(() => setLoading(false));
  }, []);
  useEffect(() => void load(), [load]);
  return { status, setStatus, loading, error, setError, load };
}

export function FeishuBot() {
  const bot = useBot();
  const [open, setOpen] = useState(() => new URLSearchParams(location.search).get("connector") === "feishu-bot");
  const tone = botTone(bot.status);
  return (
    <>
      <Group title="飞书机器人" footer="在飞书里私聊这个机器人，就是在和 Persona 说话。机器人只回应配对过的你，群里不回应。">
        <button type="button" onClick={() => setOpen(true)} className="flex w-full items-center gap-3 whitespace-nowrap px-4 py-3 text-left transition-colors hover:bg-hover">
          <span className="flex size-9 shrink-0 items-center justify-center rounded-[9px] bg-accent-tint text-accent-ink">
            <Icon name="send" size={16} />
          </span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-[13.5px] font-medium text-ink">{bot.status?.bot_name || "飞书机器人"}</span>
            <span className="mt-0.5 flex items-center gap-1.5 text-[11.5px] text-ink-3">
              <span className={cx("size-1.5 shrink-0 rounded-full", tone ? dotClass[tone] : "bg-line-strong")} />
              <span className="truncate">{bot.error ? "读不到状态" : botStateText(bot.status, bot.loading)}</span>
            </span>
          </span>
          {bot.status && <Pill tone={tone}>{botSummary(bot.status)}</Pill>}
          <Icon name="chevronRight" size={12} stroke={2} className="shrink-0 text-ink-3" />
        </button>
      </Group>
      {open && <FeishuBotDialog bot={bot} onClose={() => setOpen(false)} />}
    </>
  );
}

function FeishuBotDialog({ bot, onClose }: { bot: ReturnType<typeof useBot>; onClose: () => void }) {
  const { status, setStatus, loading, error, setError, load } = bot;
  const [form, setForm] = useState<BotCredentials>(() => (status ? credentialsOf(status) : { appID: "", appSecret: "", domain: "feishu" }));
  const [saving, setSaving] = useState(false);
  const [confirmUnpair, setConfirmUnpair] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  const changed = credentialsChanged(status, form);
  const pairing = livePairing(status, now);
  const features = status?.features ?? DEFAULT_FEATURES;

  // A newer status: fill the form unless something is being typed.
  const adopt = (s: FeishuBotStatus, replace = false) => {
    if (replace || !changed) setForm(credentialsOf(s));
    setStatus(s);
  };
  useEffect(() => {
    if (status && !changed) setForm(credentialsOf(status));
    // only when the hub's credentials change, not on each keystroke
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status?.app_id, status?.domain]);

  // Waiting for the code: tick the countdown and look again every 3 s until someone uses it.
  useEffect(() => {
    if (!pairing || status?.owner) return;
    const tick = setInterval(() => setNow(Date.now()), 1000);
    const poll = setInterval(() => void botStatus().then((s) => adopt(s), () => {}), 3000);
    return () => (clearInterval(tick), clearInterval(poll));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pairing?.code, status?.owner]);

  const run = async (what: string, call: () => Promise<FeishuBotStatus>, done?: string) => {
    setSaving(true);
    try {
      adopt(await call(), true);
      setError("");
      if (done) toast(done, "ok");
      return true;
    } catch (e) {
      setError(`${what}：${errorText(e)}`);
      return false;
    } finally {
      setSaving(false);
    }
  };

  const test = async () => {
    setSaving(true);
    try {
      await testBot();
      toast("已发一条测试消息到飞书", "ok");
      setError("");
    } catch (e) {
      setError(`测试消息没发出去：${errorText(e)}`);
    } finally {
      setSaving(false);
    }
  };

  const tone = botTone(status);
  const agents = status?.agents ?? [];

  return (
    <Dialog open onClose={onClose} title="飞书机器人" width={560}>
      <Group footer="在飞书里私聊这个机器人，就是在和 Persona 说话。机器人只回应配对过的你，群里不回应。">
        <div className="flex items-center gap-3 px-4 py-3">
          <span className="flex size-9 shrink-0 items-center justify-center rounded-[9px] bg-accent-tint text-accent-ink">
            <Icon name="send" size={16} />
          </span>
          <div className="min-w-0 flex-1">
            <div className="truncate text-[13.5px] font-medium text-ink">{status?.bot_name || "飞书机器人"}</div>
            <div className="mt-0.5 flex items-center gap-1.5 text-[11.5px] text-ink-3">
              <span className={cx("size-1.5 shrink-0 rounded-full", tone ? dotClass[tone] : "bg-line-strong")} />
              <span className="truncate" title={botStateText(status, loading)}>{botStateText(status, loading)}</span>
            </div>
          </div>
          {loading || saving ? <Spinner /> : <IconButton icon="retry" label="重新检查" onClick={() => void load()} />}
        </div>
        <Row label="打开飞书机器人">
          <Switch on={status?.enabled ?? false} disabled={!status || saving} label="打开飞书机器人" onChange={(on) => void run(on ? "打开失败" : "关闭失败", () => updateBot({ enabled: on }))} />
        </Row>
        {!!status?.last_event_at && <Row label="最近收到">{ago(new Date(status.last_event_at * 1000).toISOString())}</Row>}
        {error && <p role="alert" className="px-4 py-2.5 text-[12px] break-words text-red">{error}</p>}
      </Group>

      <Group title="应用凭证" footer={status?.from_config ? "现在用的是 hub.json 里的凭证；在这里保存会改用这里的。" : "App Secret 加密存在 hub 上，不会再显示出来。"}>
        <Row label="App ID">
          <input value={form.appID} onChange={(e) => setForm({ ...form, appID: e.target.value })} placeholder="必填" aria-label="App ID" autoComplete="off" spellCheck={false} className={cx(controlClass, "w-[240px] font-mono")} />
        </Row>
        <Row label="App Secret">
          <input
            type="password"
            value={form.appSecret}
            onChange={(e) => setForm({ ...form, appSecret: e.target.value })}
            placeholder={status?.has_secret ? "已保存，留空不改" : "必填"}
            aria-label="App Secret"
            autoComplete="new-password"
            className={cx(controlClass, "w-[240px]")}
          />
        </Row>
        <Row label="平台">
          <select value={form.domain} onChange={(e) => setForm({ ...form, domain: e.target.value })} aria-label="平台" className={cx(controlClass, "w-[240px]")}>
            <option value="feishu">飞书</option>
            <option value="lark">Lark</option>
            {form.domain !== "feishu" && form.domain !== "lark" && <option value={form.domain}>{form.domain}</option>}
          </select>
        </Row>
        <div className="flex justify-end px-4 py-2.5">
          <Button tone="accent" busy={saving && changed} disabled={!changed || !form.appID.trim() || saving} onClick={() => void run("保存失败", () => updateBot(credentialsUpdate(form)), "应用凭证已验证并保存")}>
            验证并保存
          </Button>
        </div>
      </Group>

      {status?.has_secret && (
        <>
          <Group
            title="配对"
            footer={status.owner || pairing ? undefined : status.enabled ? "点「配对」拿一个 6 位数字，10 分钟内在飞书里发给机器人。" : "先打开飞书机器人再配对。"}
          >
            {status.owner ? (
              <>
                <Row label="已配对">{status.owner.name || "飞书用户"}</Row>
                {status.target && <Row label="消息现在发给">{status.target}</Row>}
                {confirmUnpair ? (
                  <div className="flex items-center gap-2 bg-red-tint/50 px-4 py-2.5">
                    <span className="min-w-0 flex-1 text-[12px] leading-snug text-ink-2">解除配对？机器人会不再回应现在的飞书账号，直到重新配对。</span>
                    <Button tone="quiet" onClick={() => setConfirmUnpair(false)}>取消</Button>
                    <Button tone="danger" busy={saving} onClick={() => void run("解除失败", unpairBot, "已解除配对").then((ok) => ok && setConfirmUnpair(false))}>
                      解除配对
                    </Button>
                  </div>
                ) : (
                  <div className="flex justify-end gap-2 px-4 py-2.5">
                    <Button tone="quiet" icon="x" onClick={() => setConfirmUnpair(true)} className="text-red enabled:hover:text-red">
                      解除配对
                    </Button>
                    <Button icon="send" disabled={saving || !status.connected} onClick={() => void test()}>
                      发一条测试消息
                    </Button>
                  </div>
                )}
              </>
            ) : pairing ? (
              <div className="flex items-end gap-3 px-4 py-3">
                <div className="min-w-0 flex-1">
                  <div className="text-[12px] text-ink-3">在飞书里把这串数字发给机器人</div>
                  <div className="mt-1 font-mono text-[28px] font-semibold tracking-[0.12em] text-ink tabular-nums select-all">{pairing.code}</div>
                  <div className="mt-0.5 text-[11.5px] text-ink-3 tabular-nums">{pairingLeft(pairing.expires_at, now)}</div>
                </div>
                <Button icon="copy" onClick={() => void copyText(pairing.code).then(() => toast("已拷贝配对码", "ok"))}>
                  拷贝配对码
                </Button>
              </div>
            ) : (
              <div className="flex justify-end px-4 py-2.5">
                <Button tone="accent" icon="link" disabled={saving || !status.enabled} onClick={() => void run("拿不到配对码", pairBot)}>
                  配对
                </Button>
              </div>
            )}
          </Group>

          <Group title="能做什么" footer="主对话能用你的全部数据（飞书、邮件、手机里的日历和位置……），机器人发出的消息会留在飞书里。替你出面的操作照常先问你。">
            {FEATURES.map((f) => {
              const off = f.key === "all_cards" && !features.cards;
              return (
                <div key={f.key} className={cx("flex items-start gap-3 px-4 py-2.5", off && "opacity-50")}>
                  <Icon name={f.icon} size={15} className="mt-0.5 shrink-0 text-accent" />
                  <div className="min-w-0 flex-1">
                    <div className="text-[13px] text-ink">{f.title}</div>
                    <div className="mt-0.5 text-[11.5px] leading-snug text-ink-3">{f.detail}</div>
                  </div>
                  <Switch on={features[f.key]} disabled={saving || off} label={f.title} onChange={(on) => void run("保存失败", () => updateBot({ features: withFeature(features, f.key, on) }))} />
                </div>
              );
            })}
          </Group>
        </>
      )}

      {agents.length > 0 && (
        <Group title="飞书里聊过的 Agent">
          {agents.map((a) => (
            <Row key={a.session_id} label={a.title}>
              <span className="max-w-[160px] truncate">{a.machine_id}</span>
            </Row>
          ))}
        </Group>
      )}

      <Group title="怎么建机器人" footer="长连接由 hub 主动连出去，hub 不需要公网地址。">
        {STEPS.map((s, i) => (
          <div key={i} className="flex items-baseline gap-3 px-4 py-2">
            <span className="w-3 shrink-0 text-[11.5px] font-semibold text-accent tabular-nums">{i + 1}</span>
            <span className="text-[11.5px] leading-relaxed text-ink-3">{s}</span>
          </div>
        ))}
      </Group>

    </Dialog>
  );
}
