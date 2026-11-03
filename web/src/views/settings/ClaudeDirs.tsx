// 设置 › Claude Code › 屏蔽的配置目录 (iOS ClaudeDirsSection; docs/claude-integration.md「屏蔽配置目录」): the
// computer's Claude Code config directories (CLAUDE_CONFIG_DIR), each with 屏蔽 / 取消屏蔽, and rules typed by hand
// (a path or a glob such as ~/.automation/*). A blocked directory is not scanned or remembered and its sessions leave
// Agents; the files are not touched. Each change is one PUT of the whole list; the answer is the new state.
import { useEffect, useState } from "react";
import {
  dirMeta,
  looseRules,
  patternError,
  shortPath,
  toggledPatterns,
  withPattern,
  type ClaudeDir,
  type ClaudeDirsState,
} from "../../lib/agent-settings";
import { api, enc, errorText } from "../../lib/api";
import { Button, cx, EASE, Loading, toast } from "../../ui/atoms";
import { Group } from "./SettingsParts";

const FOOTER =
  "被屏蔽的目录不再扫描、不再记住，里面的会话不出现在 Agents（文件不动）；取消屏蔽后恢复。适合用单独 CLAUDE_CONFIG_DIR 跑 claude 的自动化脚本。默认目录不能屏蔽。";

export function ClaudeDirs({ mid, name }: { mid: string; name: string }) {
  const [state, setState] = useState<ClaudeDirsState | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const [input, setInput] = useState("");
  const [reload, setReload] = useState(0);

  useEffect(() => {
    if (!mid) return;
    let dead = false;
    setState(null);
    setError("");
    api<ClaudeDirsState>(`/machines/${enc(mid)}/claude-dirs`)
      .then((r) => !dead && setState(r))
      .catch((e) => !dead && setError(errorText(e)));
    return () => {
      dead = true;
    };
  }, [mid, reload]);

  const save = async (blocked: string[], key: string, done: string) => {
    setBusy(key);
    try {
      setState(await api<ClaudeDirsState>(`/machines/${enc(mid)}/claude-dirs/blocked`, { method: "PUT", json: { blocked } }));
      toast(done, "ok");
      return true;
    } catch (e) {
      toast(`没有改成：${errorText(e)}`, "error");
      return false;
    } finally {
      setBusy("");
    }
  };

  const toggle = (d: ClaudeDir) => {
    if (!state) return;
    const label = shortPath(d.path, state.home);
    void save(toggledPatterns(state, d), d.path, d.blocked ? `已取消屏蔽 ${d.blocked_by ?? label}` : `已屏蔽 ${label}`);
  };

  const typed = state ? patternError(input, state.blocked) : "";
  const add = async () => {
    if (!state || typed) return;
    if (await save(withPattern(state.blocked, input), "+", `已屏蔽 ${input.trim()}`)) setInput("");
  };

  return (
    <Group title={`屏蔽的配置目录 · ${name}`} footer={FOOTER}>
      {error ? (
        <div className="flex items-center gap-3 px-4 py-3">
          <p className="min-w-0 flex-1 text-[12.5px] text-red">读不到这台电脑的配置目录：{error}</p>
          <Button tone="secondary" icon="retry" onClick={() => setReload((n) => n + 1)}>
            重试
          </Button>
        </div>
      ) : !state ? (
        <div className="px-4 py-3">
          <Loading />
        </div>
      ) : (
        <>
          {state.dirs.map((d, i) => (
            <Row key={d.path} i={i} title={shortPath(d.path, state.home)} meta={dirMeta(d, state.home)} dim={!!d.blocked}>
              {!d.default && (
                <Button tone={d.blocked ? "secondary" : "quiet"} busy={busy === d.path} disabled={!!busy} onClick={() => toggle(d)}>
                  {d.blocked ? "取消屏蔽" : "屏蔽"}
                </Button>
              )}
            </Row>
          ))}
          {looseRules(state).map((p, i) => (
            <Row key={`rule:${p}`} i={state.dirs.length + i} title={p} meta="规则" dim>
              <Button tone="secondary" busy={busy === p} disabled={!!busy} onClick={() => void save(state.blocked.filter((x) => x !== p), p, `已删除规则 ${p}`)}>
                删除
              </Button>
            </Row>
          ))}
          <div className="flex items-start gap-2 px-4 py-2.5">
            <div className="min-w-0 flex-1">
              <input
                value={input}
                onChange={(e) => setInput(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && void add()}
                placeholder="~/.automation/* 或具体路径"
                spellCheck={false}
                autoComplete="off"
                aria-label="屏蔽目录或通配"
                className="h-8 w-full rounded-control bg-field px-2.5 font-mono text-[12px] text-ink shadow-inset-field outline-none placeholder:font-sans placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--accent)]"
              />
              {input && typed && <p className="mt-1 text-[11.5px] text-ink-3">{typed}</p>}
            </div>
            <Button tone="secondary" icon="plus" busy={busy === "+"} disabled={!input || !!typed || !!busy} onClick={() => void add()}>
              屏蔽
            </Button>
          </div>
          {!state.live && <p className="px-4 py-2 text-[11.5px] text-ink-3">这台电脑不在线或 node 较旧：列的是 hub 记下的目录，连上后会列出全部。</p>}
        </>
      )}
    </Group>
  );
}

function Row({ i, title, meta, dim, children }: { i: number; title: string; meta: string; dim: boolean; children?: React.ReactNode }) {
  return (
    <div className="flex items-center gap-3 px-4 py-2.5" style={{ animation: `fade-up 320ms ${EASE} ${Math.min(i, 8) * 30}ms both` }}>
      <div className="min-w-0 flex-1">
        <div className={cx("truncate font-mono text-[12.5px]", dim ? "text-ink-3 line-through decoration-ink-3/50" : "text-ink")}>{title}</div>
        {meta && <div className="mt-0.5 truncate text-[11.5px] text-ink-3">{meta}</div>}
      </div>
      {children}
    </div>
  );
}
