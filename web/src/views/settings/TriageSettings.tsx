// 设置 › 会话分诊 (docs/agent-work.md「开关」): when an agent stops, the hub can read its last reply and say whether it is
// done or what it waits for, and act on it. All off until turned on here: the triage calls a model on every stopped
// turn, and 自动继续 / 到点叫醒 run a turn of the agent on its own model. Saved on the hub (PUT /agents/triage), so it is
// the same on every device.
import { useEffect, useState } from "react";
import { api, errorText } from "../../lib/api";
import type { TriageSwitches } from "../../lib/types";
import { toast } from "../../ui/atoms";
import { Group, Row, Switch } from "./SettingsParts";

const OFF: TriageSwitches = { enabled: false, auto_continue: false, wake: false, decide: false };

export function TriageSettings({ onModel }: { onModel?: () => void }) {
  const [sw, setSw] = useState<TriageSwitches | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let dead = false;
    api<TriageSwitches>("/agents/triage")
      .then((r) => !dead && setSw({ ...OFF, ...r }))
      .catch(() => !dead && setSw(OFF));
    return () => {
      dead = true;
    };
  }, []);

  const change = async (next: TriageSwitches) => {
    const before = sw;
    setSw(next);
    setBusy(true);
    try {
      setSw(await api<TriageSwitches>("/agents/triage", { method: "PUT", json: next }));
    } catch (e) {
      setSw(before);
      toast(`没改成：${errorText(e)}`, "error");
    } finally {
      setBusy(false);
    }
  };
  const flip = (k: keyof TriageSwitches) => (on: boolean) => sw && change({ ...sw, [k]: on });
  const off = !sw?.enabled;

  return (
    <Group
      title="会话分诊"
      footer={
        <>
          Agent 每停下一轮，hub 用分诊模型读它最后的回复（每轮一次，用的是你在
          {onModel ? (
            <button type="button" onClick={onModel} className="mx-0.5 text-accent hover:underline">
              模型配置 › 会话分诊
            </button>
          ) : (
            "「模型配置 › 会话分诊」"
          )}
          里选的模型）。自动继续和到点叫醒会让 Agent 用它自己的模型再跑一轮。推代码、部署、给别人发消息这类事 hub 只会问你，不会自己做。
        </>
      }
    >
      <Row label="会话分诊" hint="列表里显示在等什么；做完的不再算完成未看">
        <Switch on={Boolean(sw?.enabled)} onChange={flip("enabled")} disabled={!sw || busy} label="会话分诊" />
      </Row>
      <Row label="要你决定的开卡片" hint="Agent 在等你拍板时，问题和选项放进「需要你」">
        <Switch on={Boolean(sw?.decide)} onChange={flip("decide")} disabled={!sw || busy || off} label="要你决定的开卡片" />
      </Row>
      <Row label="出错自动继续" hint="模型服务临时出错（503 等）后自动发「继续」，最多 3 次">
        <Switch on={Boolean(sw?.auto_continue)} onChange={flip("auto_continue")} disabled={!sw || busy || off} label="出错自动继续" />
      </Row>
      <Row label="到点叫醒" hint="在等部署、CI 或另一个 Agent 的，到点叫醒它自己去查">
        <Switch on={Boolean(sw?.wake)} onChange={flip("wake")} disabled={!sw || busy || off} label="到点叫醒" />
      </Row>
    </Group>
  );
}
