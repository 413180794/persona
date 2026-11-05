// Where text picked in the link panel goes (「作为上下文」/「AI 修改」): the composer the user is writing in — the
// conversation on screen, or the one last typed in. With none on the page (总览, 机器, 设置…), or one that can't send
// (a read-only source), it goes to Persona's main conversation (a new one when there is none): the page moves there and
// the panel stays open, as on the Mac.
import { addContext, editMessage, usableComposer, whenComposer, type ContextRef } from "../../lib/links";
import { navigate } from "../../lib/router";
import { getState } from "../../lib/store";
import { toast } from "../../ui/atoms";
import { t } from "../../i18n";

/** Persona's main conversation (its composer's draft key and route), else a new conversation. */
function jarvisTarget(): { key: string; go: () => void } {
  const a = getState().assistant;
  if (a?.main) return { key: `${a.machine_id}/${a.main}`, go: () => navigate({ name: "jarvis", sid: a.main }) };
  return { key: "jarvis-new", go: () => navigate({ name: "jarvis" }) };
}

/** Who gets it: "当前对话" when a composer is on the page, else Persona. */
export function handoffLabel(): string {
  return usableComposer() ? t("link-handoff.current_chat") : "Persona";
}

export function giveContext(ref: ContextRef) {
  const c = usableComposer();
  if (c) {
    addContext(c.key, ref);
    toast(t("link-handoff.added"), "ok");
    return;
  }
  const target = jarvisTarget();
  addContext(target.key, ref);
  target.go();
  toast(t("link-handoff.added_persona"), "ok");
}

/** 「AI 修改」: sent at once. If it can't go (the conversation can't take messages now), it waits as a context instead. */
export function giveEdit(ref: ContextRef, ask: string) {
  const text = editMessage(ref, ask);
  const keep = (key: string) => {
    addContext(key, { ...ref, kind: "edit" });
    toast(t("link-handoff.send_failed"), "error");
  };
  const send = (c: { key: string; send: (text: string) => Promise<boolean> }) =>
    void c.send(text).then((ok) => {
      if (!ok) keep(c.key);
    });
  const c = usableComposer();
  if (c) return send(c);
  const target = jarvisTarget();
  target.go();
  whenComposer(target.key, send, () => keep(target.key));
}
