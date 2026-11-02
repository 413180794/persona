// 设置 › 翻译 (docs/ios.md「翻译」, the app's TranslationSettingsView): on / off, the target language, and what gets
// translated: mail (on), and the Feishu chats picked here (none: Feishu has too many messages to translate them all).
// Kept in this browser. The hub translates new mail ahead of time for the phones that ask; here a mail is translated
// when it is opened.
import { useEffect, useMemo, useState } from "react";
import { errorText, getToken } from "../../lib/api";
import { modelPurpose, modelSummary, type ModelPurposeBody } from "../../lib/model-config";
import { LANGUAGES, larkChatChoices, setTranslatePrefs, useTranslatePrefs, type TranslateChat } from "../../lib/translate";
import { Button, cx, Dialog, IconButton, Loading } from "../../ui/atoms";
import { Icon } from "../../ui/icons";
import { Monogram } from "../inbox/InboxParts";
import { Group, Row, Switch } from "./SettingsParts";

const controlClass = "h-8 rounded-control bg-field px-2.5 text-[12.5px] text-ink shadow-inset-field outline-none focus:shadow-[inset_0_0_0_1px_var(--accent)]";

export function TranslationSettings({ onConfigure }: { onConfigure: () => void }) {
  const prefs = useTranslatePrefs();
  const [model, setModel] = useState<ModelPurposeBody | null>(null);
  const [picking, setPicking] = useState(false);

  useEffect(() => {
    modelPurpose("translate").then(setModel, () => {});
  }, []);

  return (
    <>
      <Group footer="内容不是目标语言时自动翻译，原文随时可以切回来。翻译由「模型配置」中的翻译模型处理。">
        <Row label={<span className="flex items-center gap-2"><Icon name="translate" size={14} className="text-ink-3" />自动翻译</span>}>
          <Switch on={prefs.enabled} onChange={(on) => setTranslatePrefs({ enabled: on })} label="自动翻译" />
        </Row>
        <Row label="翻译模型" hint={model ? modelSummary(model.purpose, model.providers) : undefined} onClick={onConfigure} />
      </Group>
      {prefs.enabled && (
        <>
          <Group>
            <Row label="目标语言">
              <select value={prefs.target} onChange={(e) => setTranslatePrefs({ target: e.target.value })} className={cx(controlClass, "min-w-[140px]")} aria-label="目标语言">
                {LANGUAGES.map((l) => <option key={l.code} value={l.code}>{l.name}</option>)}
              </select>
            </Row>
          </Group>
          <Group title="翻译范围" footer="飞书消息很多，默认不翻译；只翻译这里选中的会话里别人发的消息。">
            <Row label={<span className="flex items-center gap-2"><Icon name="mail" size={14} className="text-ink-3" />邮件</span>}>
              <Switch on={prefs.mail} onChange={(on) => setTranslatePrefs({ mail: on })} label="翻译邮件" />
            </Row>
            <Row label={<span className="flex items-center gap-2"><Icon name="chat" size={14} className="text-ink-3" />飞书会话</span>} onClick={() => setPicking(true)}>
              <span className="text-ink-3">{prefs.chats.length ? `${prefs.chats.length} 个会话` : "不翻译"}</span>
            </Row>
          </Group>
          {prefs.chats.length > 0 && (
            <Group title="要翻译的飞书会话">
              {prefs.chats.map((c) => (
                <Row key={c.id} label={c.name}>
                  <IconButton icon="x" label={`不再翻译「${c.name}」`} size={24} iconSize={12} onClick={() => setTranslatePrefs({ chats: prefs.chats.filter((x) => x.id !== c.id) })} />
                </Row>
              ))}
            </Group>
          )}
        </>
      )}
      <p className="px-1 text-[11.5px] leading-relaxed text-ink-3">
        这些设置只存在这个浏览器里。手机上的翻译设置是手机自己的：hub 会按手机的设置提前翻好新邮件，网页打开邮件时如果已经有译文就直接用。
      </p>
      {picking && <ChatPicker selected={prefs.chats} onClose={() => setPicking(false)} onSave={(chats) => setTranslatePrefs({ chats })} />}
    </>
  );
}

/** Picks the Feishu chats to translate: the picked ones first (so one no longer recent can still be unpicked), then
 * the pinned and recent ones, searchable by name. */
function ChatPicker({ selected, onClose, onSave }: { selected: TranslateChat[]; onClose: () => void; onSave: (chats: TranslateChat[]) => void }) {
  const [picked, setPicked] = useState(selected);
  const [choices, setChoices] = useState<{ id: string; name: string; avatar?: string; is_group?: boolean }[] | null>(null);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");

  useEffect(() => {
    let live = true;
    larkChatChoices(getToken())
      .then((list) => live && setChoices(list))
      .catch((e) => {
        if (!live) return;
        setError(errorText(e));
        setChoices([]);
      });
    return () => {
      live = false;
    };
  }, []);

  const rows = useMemo(() => {
    const out: TranslateChat[] = [...selected];
    for (const c of choices ?? []) if (!out.some((x) => x.id === c.id)) out.push({ id: c.id, name: c.name });
    const q = query.trim().toLocaleLowerCase();
    return q ? out.filter((c) => c.name.toLocaleLowerCase().includes(q)) : out;
  }, [selected, choices, query]);

  const toggle = (c: TranslateChat) => setPicked((p) => (p.some((x) => x.id === c.id) ? p.filter((x) => x.id !== c.id) : [...p, c]));

  return (
    <Dialog
      open
      onClose={onClose}
      title="要翻译的会话"
      width={460}
      footer={
        <>
          <Button tone="quiet" onClick={onClose}>取消</Button>
          <Button
            tone="accent"
            onClick={() => {
              onSave(picked);
              onClose();
            }}
          >
            完成
          </Button>
        </>
      }
    >
      <label className="flex h-8 items-center gap-2 rounded-control bg-field px-2.5 shadow-inset-field focus-within:shadow-[inset_0_0_0_1px_var(--accent)]">
        <Icon name="search" size={13} stroke={2} className="shrink-0 text-ink-3" />
        <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="搜索会话" aria-label="搜索会话" className="min-w-0 flex-1 bg-transparent text-[12.5px] text-ink outline-none placeholder:text-ink-3" />
      </label>
      {error && <p className="mt-2 text-[12px] text-red">{error}</p>}
      <div className="mt-2 max-h-[46vh] overflow-y-auto">
        {choices === null && !selected.length ? (
          <div className="py-4"><Loading label="正在读飞书会话" /></div>
        ) : !rows.length ? (
          <p className="py-6 text-center text-[12.5px] text-ink-3">{query ? "没有匹配的会话" : "没有飞书会话"}</p>
        ) : (
          rows.map((c) => {
            const on = picked.some((x) => x.id === c.id);
            const conv = choices?.find((x) => x.id === c.id);
            return (
              <button
                key={c.id}
                type="button"
                role="checkbox"
                aria-checked={on}
                onClick={() => toggle(c)}
                className="flex w-full items-center gap-3 rounded-[8px] px-2 py-1.5 text-left transition-colors hover:bg-hover"
              >
                <Monogram name={c.name} size={28} square={conv?.is_group ?? true} src={conv?.avatar} />
                <span className="min-w-0 flex-1 truncate text-[13px] text-ink">{c.name}</span>
                <span className={cx("flex size-[18px] shrink-0 items-center justify-center rounded-full", on ? "bg-accent text-white" : "shadow-[inset_0_0_0_1.5px_var(--line-strong)]")}>
                  {on && <Icon name="check" size={11} stroke={3} />}
                </span>
              </button>
            );
          })
        )}
        {choices === null && selected.length > 0 && <div className="py-2"><Loading label="正在读飞书会话" /></div>}
      </div>
    </Dialog>
  );
}
