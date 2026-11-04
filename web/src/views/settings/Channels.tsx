// 设置 › 插件 › 渠道 (docs/channels.md「界面」, iOS ChannelsView): every way of talking to Persona, connected or not, and
// each one's page — its switch, binding a step at a time (a QR code to scan in the chat app, a pairing code to send the
// bot, credentials), where its messages go, what it does, a test message to the owner — and, for a market's, the way
// to install it. Read-only ones (the apps, /agent, voice) say where they are set up.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { errorText } from "../../lib/api";
import {
  bindChannel, bindTitle, useChannels, canBind, capabilityText, channelDetail, channelSections, channelStateText, channelSubtitle, channelsSummary,
  channelTone, forgetChannels, isBound, isPolled, listChannels, targetText, testChannel, unbindChannel, updateChannel,
  type Channel, type ChannelDetail, type ChannelPrompt, type ChannelTarget, type ChannelTone,
} from "../../lib/channels";
import { canSendStep, fieldInput } from "../../lib/connectors";
import { ago } from "../../lib/format";
import { useStore } from "../../lib/store";
import { Button, cx, Dialog, EASE, IconButton, Loading, Spinner, toast } from "../../ui/atoms";
import { ChannelMark } from "../../ui/ChannelMark";
import { Icon } from "../../ui/icons";
import { Group, Pill, Row, Switch } from "./SettingsParts";

const DOT: Record<ChannelTone, string> = { green: "bg-green", red: "bg-red", idle: "bg-line-strong" };
const fieldClass = "h-8 w-full rounded-control bg-field px-2.5 text-[12.5px] text-ink shadow-inset-field outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)] disabled:opacity-60";

function StateLabel({ c }: { c: Channel }) {
  return (
    <span className={cx("flex shrink-0 items-center gap-1.5 whitespace-nowrap text-[12px]", c.state === "error" ? "text-red" : "text-ink-2")}>
      <span aria-hidden className={cx("size-1.5 shrink-0 rounded-full", DOT[channelTone(c)])} />
      {channelStateText(c)}
    </span>
  );
}

// ── the links from 设置 › 连接器 ─────────────────────────

/** On a connector's page (WeChat's, the Feishu bot's): it is a way of talking to Persona, set up under 渠道. Only for a
 * channel there is, on a hub that has them. */
export function ChannelLink({ id, onOpen }: { id: string; onOpen: (id: string) => void }) {
  const has = useStore((s) => s.features.includes("channels"));
  const list = useChannels();
  const c = list.find((x) => x.id === id);
  if (!has || !c) return null;
  return (
    <Group footer={`${c.name}也是一个和 Persona 对话的渠道：消息发到哪段对话、提问和推送在渠道里设置。`}>
      <Row
        label={
          <span className="flex items-center gap-2">
            <ChannelMark c={c} size={18} />
            在 设置 › 插件 › 渠道 里设置{c.name}
          </span>
        }
        onClick={() => onOpen(id)}
      />
    </Group>
  );
}

// ── the row on 设置 › 插件 ───────────────────────────────

/** The 渠道 row at the top of 设置 › 插件: how many are connected, and the way in. */
export function ChannelsEntry({ onOpen }: { onOpen: () => void }) {
  const [list, setList] = useState<Channel[] | null>(null);
  useEffect(() => {
    listChannels().then(setList, () => setList([]));
  }, []);
  const bad = list?.some((c) => c.state === "error");
  return (
    <Group title="渠道" footer="和 Persona 对话的入口：Persona 自己的各端、微信、飞书机器人……在这里绑定、开关、选消息发到哪段对话。">
      <Row
        label={
          <span className="flex items-center gap-2">
            <Icon name="chat" size={13} className="shrink-0 text-ink-3" />
            渠道
          </span>
        }
        onClick={onOpen}
      >
        {list && <span className={cx(bad && "text-red")}>{channelsSummary(list)}</span>}
      </Row>
    </Group>
  );
}

// ── the list ─────────────────────────────────────────────

export function ChannelList({ onOpen, onBack, onMarketItem }: { onOpen: (id: string) => void; onBack: () => void; onMarketItem: (market: string, plugin: string) => void }) {
  const [list, setList] = useState<Channel[] | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const load = () => {
    setLoading(true);
    listChannels()
      .then((l) => (setList(l), setError("")))
      .catch((e) => setError(errorText(e)))
      .finally(() => setLoading(false));
  };
  useEffect(load, []);
  const sections = useMemo(() => channelSections(list ?? []), [list]);

  return (
    <>
      <div className="mb-3 flex items-center gap-2">
        <Button tone="quiet" icon="chevronLeft" onClick={onBack} className="-ml-2">插件</Button>
        <span className="flex-1" />
        <IconButton icon="retry" label="刷新渠道" onClick={load} disabled={loading} />
      </div>
      <h1 className="mb-1 px-1 text-[17px] font-semibold text-ink">渠道</h1>
      <p className="mb-5 px-1 text-[12.5px] leading-relaxed text-ink-3">
        在哪里都能和 Persona 说话：消息进主对话（或你选的对话），这一轮的回答发回原处，Agent 要你回答或批准的也发过去，在别处答过就不再等。
      </p>
      {!list && !error && (
        <Group>
          <div className="px-4 py-3">
            <Loading />
          </div>
        </Group>
      )}
      {error && (
        <Group>
          <div className="flex items-center gap-3 px-4 py-3">
            <p className="min-w-0 flex-1 text-[12.5px] break-words text-red">读不到渠道：{error}</p>
            <Button tone="secondary" icon="retry" onClick={load} busy={loading}>重试</Button>
          </div>
        </Group>
      )}
      {sections.map((s) => (
        <Group key={s.title} title={s.title} footer={s.footer}>
          {s.channels.map((c, i) => (
            <button
              key={c.id}
              type="button"
              onClick={() => (c.source === "market" && c.market && c.plugin ? onMarketItem(c.market, c.plugin) : onOpen(c.id))}
              className="flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-hover"
              style={{ animation: `fade-up 320ms ${EASE} ${Math.min(i, 8) * 30}ms both` }}
            >
              <ChannelMark c={c} size={32} />
              <div className="min-w-0 flex-1">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="truncate text-[13px] text-ink">{c.name}</span>
                  {c.source === "plugin" && c.kind === "im" && <Pill>插件</Pill>}
                </div>
                {channelSubtitle(c) && (
                  <div className={cx("mt-0.5 truncate text-[11.5px]", c.state === "error" ? "text-red" : "text-ink-3")}>{channelSubtitle(c)}</div>
                )}
              </div>
              {c.source === "market" ? (
                <span className="shrink-0 whitespace-nowrap text-[12px] text-accent-ink">去安装</span>
              ) : (
                c.kind !== "app" && <StateLabel c={c} />
              )}
              <Icon name="chevronRight" size={12} stroke={2} className="shrink-0 text-ink-3" />
            </button>
          ))}
        </Group>
      ))}
    </>
  );
}

// ── one channel ──────────────────────────────────────────

export function ChannelPage({ id, onBack, onSettings }: { id: string; onBack: () => void; onSettings: (url: string) => void }) {
  const [d, setD] = useState<ChannelDetail | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const [picking, setPicking] = useState(false);
  const [unbinding, setUnbinding] = useState(false);
  const load = useCallback(async () => {
    try {
      setD(await channelDetail(id));
      setError("");
    } catch (e) {
      setError(errorText(e));
    }
  }, [id]);
  useEffect(() => void load(), [load]);

  const change = async (what: string, body: Parameters<typeof updateChannel>[1]) => {
    setBusy(what);
    try {
      setD(await updateChannel(id, body));
      forgetChannels();
    } catch (e) {
      toast(errorText(e), "error");
    } finally {
      setBusy("");
    }
  };

  const head = (
    <div className="mb-3 flex items-center gap-2">
      <Button tone="quiet" icon="chevronLeft" onClick={onBack} className="-ml-2">渠道</Button>
      <span className="flex-1" />
      <IconButton icon="retry" label="刷新" onClick={() => void load()} />
    </div>
  );
  if (!d) {
    return (
      <>
        {head}
        <Group>
          <div className="px-4 py-3">{error ? <p className="text-[12.5px] text-red">读不到这个渠道：{error}</p> : <Loading />}</div>
        </Group>
      </>
    );
  }
  const editable = !d.read_only && d.source !== "market";
  return (
    <>
      {head}
      <div className="mb-5 flex items-center gap-3 px-1">
        <ChannelMark c={d} size={44} />
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-[17px] font-semibold text-ink">{d.name}</h1>
          <div className="mt-0.5 flex min-w-0 items-center gap-2 text-[12px] text-ink-3">
            {d.kind !== "app" && <StateLabel c={d} />}
            {d.account?.name && <span className="truncate">{d.account.name}</span>}
            {d.last_event_at && <span className="shrink-0">· 最近一条 {ago(d.last_event_at)}</span>}
          </div>
        </div>
      </div>
      {d.description && <p className="-mt-2 mb-5 px-1 text-[12.5px] leading-relaxed text-ink-3">{d.description}</p>}
      {d.state === "error" && d.error && (
        <div role="alert" className="mb-5 flex items-start gap-2 rounded-card bg-red-tint px-3.5 py-2.5 text-[12.5px] text-red">
          <Icon name="info" size={14} className="mt-px shrink-0" />
          <span className="min-w-0 break-words">{d.error}</span>
        </div>
      )}

      {editable && (
        <Group footer={d.source === "plugin" ? "关掉后它不再收消息；绑定和设置留着。" : undefined}>
          <Row label={`启用 ${d.name}`}>
            <Switch on={d.enabled} onChange={(on) => void change("enabled", { enabled: on })} disabled={busy === "enabled"} label={`启用 ${d.name}`} />
          </Row>
        </Group>
      )}

      {editable && canBind(d) && <BindSteps c={d} onDone={load} />}
      {editable && isBound(d) && (
        <Group title="绑定" footer="只有绑定的这个人发的消息会交给 Persona，别人发来只回一句「只为绑定它的人服务」。">
          <Row label="绑定的账号">{d.account?.name ?? "已绑定"}</Row>
          <div className="px-4 py-2.5">
            <Button tone="quiet" icon="logout" onClick={() => setUnbinding(true)} className="-ml-2 text-red">解除绑定</Button>
          </div>
        </Group>
      )}

      {editable && d.target && (
        <Group title="消息发到" footer="在聊天里发 /new 开新对话、/main 回到主对话，也会改这里。">
          <Row label="发到" onClick={() => setPicking(true)}>
            <span className="max-w-[220px] truncate">{targetText(d.target)}</span>
          </Row>
        </Group>
      )}

      {editable && (d.features?.length ?? 0) > 0 && (
        <Group title="功能">
          {d.features!.map((f) => (
            <Row key={f.id} label={f.title} hint={f.description}>
              <Switch on={f.on} onChange={(on) => void change("f:" + f.id, { features: { [f.id]: on } })} disabled={busy === "f:" + f.id} label={f.title} />
            </Row>
          ))}
        </Group>
      )}

      {(d.waiting.answers > 0 || d.waiting.asks > 0 || (d.conversations?.length ?? 0) > 0) && (
        <Group title="最近">
          {(d.waiting.answers > 0 || d.waiting.asks > 0) && (
            <Row label="在等">
              {[d.waiting.answers > 0 && `${d.waiting.answers} 个回答`, d.waiting.asks > 0 && `${d.waiting.asks} 个提问`].filter(Boolean).join(" · ")}
            </Row>
          )}
          {d.conversations?.map((cv) => (
            <Row key={cv.session_id} label={cv.title || "一段对话"} hint={`消息进过这段对话 · ${ago(cv.at)}`} />
          ))}
        </Group>
      )}

      <Group title="它能做什么">
        {capabilityText(d.capabilities).map((t) => (
          <div key={t} className="flex items-center gap-2 px-4 py-2 text-[12.5px] text-ink-2">
            <Icon name="check" size={12} stroke={2.2} className="shrink-0 text-ink-3" />
            {t}
          </div>
        ))}
        {d.surfaces && d.surfaces.length > 0 && <Row label="可以在这些地方用">{d.surfaces.join("、")}</Row>}
      </Group>

      {editable && d.state === "connected" && (
        <Group footer="只发给绑定的你自己。">
          <div className="px-4 py-2.5">
            <Button
              tone="secondary"
              icon="send"
              busy={busy === "test"}
              onClick={async () => {
                setBusy("test");
                try {
                  await testChannel(id);
                  toast(`测试消息发到 ${d.name} 了`, "ok");
                } catch (e) {
                  toast(errorText(e), "error");
                } finally {
                  setBusy("");
                }
              }}
            >
              发一条测试消息
            </Button>
          </div>
        </Group>
      )}

      {d.settings_url && (
        <Group footer={d.read_only ? "这个渠道的设置在别处。" : undefined}>
          <Row label="更多设置" onClick={() => onSettings(d.settings_url!)} />
        </Group>
      )}

      <TargetPicker open={picking} current={d.target} onClose={() => setPicking(false)} onPick={(t) => (setPicking(false), void change("target", { target: t }))} />
      <Dialog
        open={unbinding}
        onClose={() => setUnbinding(false)}
        title={`解除和${d.name}的绑定？`}
        footer={
          <>
            <Button tone="quiet" onClick={() => setUnbinding(false)}>取消</Button>
            <Button
              tone="danger"
              busy={busy === "unbind"}
              onClick={async () => {
                setBusy("unbind");
                try {
                  setD(await unbindChannel(id));
                  forgetChannels();
                  setUnbinding(false);
                  toast("已解除绑定", "ok");
                } catch (e) {
                  toast(errorText(e), "error");
                } finally {
                  setBusy("");
                }
              }}
            >
              解除绑定
            </Button>
          </>
        }
      >
        <p className="px-5 py-4 text-[13px] leading-relaxed text-ink-2">之后{d.name}里发给机器人的消息不再交给 Persona，在等的回答和提问也不再发过去。想再用要重新绑定。</p>
      </Dialog>
    </>
  );
}

/** 消息发到: the main conversation, a new one, or one of the assistant's conversations. */
function TargetPicker({ open, current, onClose, onPick }: { open: boolean; current?: ChannelTarget; onClose: () => void; onPick: (t: ChannelTarget) => void }) {
  const sessions = useStore((s) => s.sessions);
  const conversations = useMemo(
    () =>
      Object.values(sessions)
        .flat()
        .filter((s) => s.assistant && !s.archived)
        .sort((a, b) => (b.updated_at ?? "").localeCompare(a.updated_at ?? ""))
        .slice(0, 12),
    [sessions],
  );
  const option = (key: string, label: string, hint: string | undefined, on: boolean, t: ChannelTarget) => (
    <button key={key} type="button" onClick={() => onPick(t)} className="flex w-full items-center gap-3 px-5 py-2.5 text-left transition-colors hover:bg-hover">
      <div className="min-w-0 flex-1">
        <div className="truncate text-[13px] text-ink">{label}</div>
        {hint && <div className="mt-0.5 truncate text-[11.5px] text-ink-3">{hint}</div>}
      </div>
      {on && <Icon name="check" size={14} stroke={2.2} className="shrink-0 text-accent" />}
    </button>
  );
  return (
    <Dialog open={open} onClose={onClose} title="消息发到">
      <div className="max-h-[60vh] divide-y divide-line overflow-y-auto">
        {option("main", "主对话", "接着之前在主对话里聊的", current?.kind === "main", { kind: "main" })}
        {option("new", "新对话", "下一条消息开一段新的，之后都发给它", current?.kind === "new", { kind: "new" })}
        {conversations.map((s) =>
          option(s.session_id, s.title || "一段对话", s.last_text, current?.kind === "conversation" && current.session_id === s.session_id, {
            kind: "conversation",
            machine_id: s.machine_id,
            session_id: s.session_id,
          }),
        )}
      </div>
    </Dialog>
  );
}

// ── binding, a step at a time ────────────────────────────

/** A QR code from the hub's modules (rows of "0" / "1"), in the page's own colors, with its quiet zone. */
function QRCode({ rows, size = 184 }: { rows: string[]; size?: number }) {
  const n = rows.length;
  const quiet = 2;
  const cells: string[] = [];
  rows.forEach((row, y) => {
    for (let x = 0; x < row.length; x++) if (row[x] === "1") cells.push(`M${x + quiet} ${y + quiet}h1v1h-1z`);
  });
  return (
    <svg role="img" aria-label="绑定用的二维码" width={size} height={size} viewBox={`0 0 ${n + quiet * 2} ${n + quiet * 2}`} shapeRendering="crispEdges" className="rounded-[10px] bg-white">
      <path d={cells.join("")} fill="#000" />
    </svg>
  );
}

function saveQR(rows: string[], name: string) {
  const n = rows.length + 4;
  const scale = 12;
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = n * scale;
  const g = canvas.getContext("2d");
  if (!g) return;
  g.fillStyle = "#fff";
  g.fillRect(0, 0, canvas.width, canvas.height);
  g.fillStyle = "#000";
  rows.forEach((row, y) => {
    for (let x = 0; x < row.length; x++) if (row[x] === "1") g.fillRect((x + 2) * scale, (y + 2) * scale, scale, scale);
  });
  const a = document.createElement("a");
  a.href = canvas.toDataURL("image/png");
  a.download = `${name}-二维码.png`;
  a.click();
}

function BindSteps({ c, onDone }: { c: Channel; onDone: () => Promise<void> }) {
  const [prompt, setPrompt] = useState<ChannelPrompt | null>(null);
  const [values, setValues] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const live = useRef(true);
  useEffect(() => {
    live.current = true;
    return () => void (live.current = false);
  }, []);

  /** True when binding is done. */
  const send = useCallback(async (step: string, vals: Record<string, string>, quiet = false): Promise<boolean> => {
    if (!quiet) setBusy(true);
    try {
      const p = await bindChannel(c.id, step, vals);
      if (!live.current) return false;
      if (p.done) {
        setPrompt(null);
        setValues({});
        forgetChannels();
        toast(p.account ? `已绑定 ${p.account}` : `已绑定${c.name}`, "ok");
        await onDone();
        return true;
      }
      setPrompt((cur) => {
        if (p.step !== cur?.step) setValues({});
        return p;
      });
    } catch (e) {
      if (live.current && !quiet) toast(errorText(e), "error");
    } finally {
      if (!quiet && live.current) setBusy(false);
    }
    return false;
  }, [c.id, c.name, onDone]);

  // A QR code or a code on screen: ask again every interval until it is scanned / sent.
  const polled = isPolled(prompt) ? prompt : null;
  useEffect(() => {
    if (!polled) return;
    let stop = false;
    let t: ReturnType<typeof setTimeout>;
    const tick = () => {
      t = setTimeout(async () => {
        if (stop) return;
        if (!(await send(polled.step ?? "", {}, true)) && !stop) tick();
      }, Math.max(polled.interval ?? 3, 1) * 1000);
    };
    tick();
    return () => {
      stop = true;
      clearTimeout(t);
    };
  }, [polled?.step, polled?.user_code, polled?.url]); // eslint-disable-line react-hooks/exhaustive-deps

  const footer = c.bind === "qr" ? "用手机上的" + c.name + "扫码，确认后这里自动继续。" : undefined;
  if (!prompt) {
    return (
      <Group title="绑定" footer={footer ?? "绑定以后，只有你发给机器人的消息会交给 Persona。"}>
        <div className="px-4 py-3">
          <Button tone="accent" icon={c.bind === "qr" ? "image" : "link"} busy={busy} onClick={() => void send("", {})}>
            {bindTitle(c)}
          </Button>
        </div>
      </Group>
    );
  }
  const canSend = canSendStep(prompt, values) && !busy;
  return (
    <Group title={bindTitle(c)}>
      {(prompt.hint || prompt.error) && (
        <div className="flex flex-col gap-1 px-4 py-2.5">
          {prompt.hint && <p className="text-[12.5px] leading-relaxed text-ink-2">{prompt.hint}</p>}
          {prompt.error && <p role="alert" className="text-[12px] text-red">{prompt.error}</p>}
        </div>
      )}
      {prompt.kind === "qr" && prompt.qr ? (
        <div className="flex flex-col items-center gap-3 px-4 py-4">
          <QRCode rows={prompt.qr.rows} />
          <p className="flex items-center gap-2 text-[12.5px] text-ink-3"><Spinner size={11} />等你在{c.name}里扫码确认…</p>
          <div className="flex items-center gap-2">
            <Button tone="secondary" icon="download" onClick={() => saveQR(prompt.qr!.rows, c.name)}>保存二维码</Button>
            <Button tone="quiet" onClick={() => setPrompt(null)}>取消</Button>
          </div>
        </div>
      ) : prompt.kind === "pair_code" || prompt.kind === "device_code" ? (
        <div className="flex flex-col gap-3 px-4 py-3">
          <div className="flex items-center gap-3">
            <span className="min-w-0 flex-1 truncate font-mono text-[28px] font-semibold tracking-[0.18em] text-ink select-all">{prompt.user_code}</span>
            <IconButton
              icon="copy"
              label="复制配对码"
              onClick={() => void navigator.clipboard?.writeText(prompt.user_code ?? "").then(() => toast("已复制", "ok"), () => toast("没复制上", "error"))}
            />
          </div>
          <p className="flex items-center gap-2 text-[12.5px] text-ink-3">
            <Spinner size={11} />
            {prompt.kind === "pair_code" ? `等你在${c.name}里发给机器人…` : "等你授权…"}
          </p>
          <div>
            <Button tone="quiet" onClick={() => setPrompt(null)} className="-ml-2">取消</Button>
          </div>
        </div>
      ) : (
        <form
          className="flex flex-col"
          onSubmit={(e) => {
            e.preventDefault();
            if (canSend) void send(prompt.step ?? "", values);
          }}
        >
          {(prompt.fields ?? []).map((f) => {
            const input = fieldInput(f);
            return (
              <label key={f.name} className="flex items-center gap-3 border-b border-line px-4 py-2">
                <span className="w-[96px] shrink-0 truncate text-[13px] text-ink">{f.label}</span>
                <input
                  value={values[f.name] ?? ""}
                  onChange={(e) => setValues((v) => ({ ...v, [f.name]: e.target.value }))}
                  type={input.type}
                  inputMode={input.inputMode}
                  autoComplete={input.autoComplete}
                  placeholder={f.placeholder ?? f.label}
                  spellCheck={false}
                  autoCapitalize="off"
                  disabled={busy}
                  className={cx(fieldClass, "font-mono")}
                />
              </label>
            );
          })}
          <div className="flex items-center gap-2 px-4 py-2.5">
            <Button type="submit" tone="accent" disabled={!canSend} busy={busy}>{busy ? "正在验证…" : "下一步"}</Button>
            <Button tone="quiet" onClick={() => (setPrompt(null), setValues({}))} disabled={busy}>取消</Button>
          </div>
        </form>
      )}
    </Group>
  );
}
