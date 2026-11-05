import { useRef, useState } from "react";
import { useDeclaredList } from "../lib/extension-declared";
import { Button, cx, Dialog, Menu } from "./atoms";

interface TriageTemplate {
  extension: string;
  extension_name: string;
  id: string;
  title: string;
  text: string;
}

// The rule field preserves newlines and stages templates in a confirmation before changing the caller's draft.
export function TriageRuleField({ id, value, placeholder, maxLength, invalid, onChange }: {
  id: string;
  value: string;
  placeholder?: string;
  maxLength?: number;
  invalid: boolean;
  onChange: (text: string) => void;
}) {
  const templates = useDeclaredList<TriageTemplate>("triage-templates", "templates");
  const trigger = useRef<HTMLSpanElement>(null);
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const [picked, setPicked] = useState<TriageTemplate | null>(null);
  return <>
    <textarea id={id} value={value} onChange={(e) => onChange(e.target.value)} rows={4} maxLength={maxLength} placeholder={placeholder} spellCheck={false}
      className={cx("w-full resize-y rounded-control bg-field px-2.5 py-2 text-[12.5px] text-ink outline-none placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)]", invalid ? "shadow-[inset_0_0_0_1px_var(--red)]" : "shadow-inset-field")} />
    {!!templates.length && <span className="inline-block" ref={trigger}><Button tone="quiet" icon="sparkles" onClick={() => setAnchor((a) => a ? null : trigger.current)}>从扩展选模板</Button></span>}
    {anchor && <Menu anchor={anchor} onClose={() => setAnchor(null)} header="规则模板" items={templates.map((t) => ({ key: `${t.extension}/${t.id}`, label: t.title, hint: t.extension_name, onSelect: () => setPicked(t) }))} />}
    {picked && <Dialog open title={`用「${picked.title}」替换规则草稿？`} onClose={() => setPicked(null)} footer={<><Button tone="quiet" onClick={() => setPicked(null)}>取消</Button><Button tone="accent" onClick={() => { onChange(picked.text); setPicked(null); }}>替换草稿</Button></>}>
      <p className="text-ink-2">来自「{picked.extension_name}」。只替换输入框里的草稿，不自动保存；确认保存后才在 hub 生效。</p>
      <p className="mt-3 whitespace-pre-wrap text-ink-2">{picked.text}</p>
    </Dialog>}
  </>;
}
