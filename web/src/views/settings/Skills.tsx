// 设置 › 助手 › Skills (the app's SkillsPage, docs/hub-update.md「skill 从二进制里拿出来」): the assistant's skills on
// the computer that runs it (GET /api/v1/skills), read only.
import { useEffect, useState } from "react";
import { errorText } from "../../lib/api";
import { skills as loadSkills, type Skill } from "../../lib/hub-update";
import { Loading } from "../../ui/atoms";
import { Group, Pill } from "./SettingsParts";

export function SkillsGroup() {
  const [list, setList] = useState<Skill[] | null>(null);
  const [machine, setMachine] = useState<string>();
  const [error, setError] = useState("");

  useEffect(() => {
    loadSkills()
      .then((r) => (setList(r.skills ?? []), setMachine(r.machine || r.machine_id)))
      .catch((e) => (setError(errorText(e)), setList([])));
  }, []);

  const where = machine ? ` ${machine} ` : "跑助手的那台电脑";
  return (
    <Group
      title={
        <span className="flex items-center gap-2">
          Skills
          {list && list.length > 0 && <span className="font-normal tabular-nums">{list.length} 个</span>}
        </span>
      }
      footer={list?.length ? `放在${where}的 ~/.persona/skills/，每个一个目录（里面有 SKILL.md）。更新 personad 不会动它们；改了以后，下一段新对话起生效。` : undefined}
    >
      {list === null ? (
        <div className="px-4 py-4">
          <Loading />
        </div>
      ) : list.length === 0 ? (
        <p className="px-4 py-4 text-[12.5px] leading-relaxed text-ink-3">
          {error ? `读不到 skill：${error}` : "还没有 skill。把 skill 放进跑助手的那台电脑的 ~/.persona/skills/，每个一个目录（里面有 SKILL.md）。"}
        </p>
      ) : (
        list.map((s) => (
          <div key={s.name} className="px-4 py-2.5">
            <div className="flex items-center gap-2">
              <span className="min-w-0 flex-1 truncate text-[13px] font-medium text-ink">{s.name}</span>
              <Pill tone={s.source === "builtin" ? undefined : "accent"}>{s.source === "builtin" ? "内置" : "我的"}</Pill>
            </div>
            {s.description && <p className="mt-0.5 line-clamp-3 text-[11.5px] leading-snug text-ink-3">{s.description}</p>}
          </div>
        ))
      )}
    </Group>
  );
}
