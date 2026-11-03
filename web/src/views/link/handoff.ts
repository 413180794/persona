// Where text picked in the link panel goes (「作为上下文」/「AI 修改」): the composer the user is writing in — the
// conversation on screen, or the one last typed in. With none on the page (总览, 机器, 设置…), or one that can't send
// (a read-only source), it goes to Persona's main conversation (a new one when there is none): the page moves there and
// the panel stays open, as on the Mac.
import { addContext, editMessage, usableComposer, whenComposer, type ContextRef } from "../../lib/links";
import { navigate } from "../../lib/router";
import { getState } from "../../lib/store";
import { toast } from "../../ui/atoms";

/** Persona's main conversation (its composer's draft key and route), else a new conversation. */
function jarvisTarget(): { key: string; go: () => void } {
  const a = getState().assistant;
  if (a?.main) return { key: `${a.machine_id}/${a.main}`, go: () => navigate({ name: "jarvis", sid: a.main }) };
  return { key: "jarvis-new", go: () => navigate({ name: "jarvis" }) };
}

/** Who gets it: "当前对话" when a composer is on the page, else Persona. */
export function handoffLabel(): string {
  return usableComposer() ? "当前对话" : "Persona";
}

export function giveContext(ref: ContextRef) {
  const c = usableComposer();
  if (c) {
    addContext(c.key, ref);
    toast("已作为上下文，会随下一条消息发送", "ok");
    return;
  }
  const t = jarvisTarget();
  addContext(t.key, ref);
  t.go();
  toast("已作为上下文，会随下一条发给 Persona 的消息发送", "ok");
}

/** 「AI 修改」: sent at once. If it can't go (the conversation can't take messages now), it waits as a context instead. */
export function giveEdit(ref: ContextRef, ask: string) {
  const text = editMessage(ref, ask);
  const keep = (key: string) => {
    addContext(key, { ...ref, kind: "edit" });
    toast("没发出去：这段已放在输入框上方，可以写上要求再发", "error");
  };
  const send = (c: { key: string; send: (t: string) => Promise<boolean> }) =>
    void c.send(text).then((ok) => {
      if (!ok) keep(c.key);
    });
  const c = usableComposer();
  if (c) return send(c);
  const t = jarvisTarget();
  t.go();
  whenComposer(t.key, send, () => keep(t.key));
}
