import { useRef, useState } from "react";
import { useDeclaredList } from "../lib/extension-declared";
import { Button, cx, Dialog, Menu } from "./atoms";
import { t } from "../i18n";

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
    {!!templates.length && <span className="inline-block" ref={trigger}><Button tone="quiet" icon="sparkles" onClick={() => setAnchor((a) => a ? null : trigger.current)}>{t("ui-triagerulefield.pick_template")}</Button></span>}
    {anchor && <Menu anchor={anchor} onClose={() => setAnchor(null)} header={t("ui-triagerulefield.templates")} items={templates.map((tpl) => ({ key: `${tpl.extension}/${tpl.id}`, label: tpl.title, hint: tpl.extension_name, onSelect: () => setPicked(tpl) }))} />}
    {picked && <Dialog open title={t("ui-triagerulefield.replace_title", { title: picked.title })} onClose={() => setPicked(null)} footer={<><Button tone="quiet" onClick={() => setPicked(null)}>{t("common.cancel")}</Button><Button tone="accent" onClick={() => { onChange(picked.text); setPicked(null); }}>{t("ui-triagerulefield.replace")}</Button></>}>
      <p className="text-ink-2">{t("ui-triagerulefield.replace_body", { extension: picked.extension_name })}</p>
      <p className="mt-3 whitespace-pre-wrap text-ink-2">{picked.text}</p>
    </Dialog>}
  </>;
}
