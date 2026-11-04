// A subagent's whole conversation in the side panel (its card's 查看完整过程), read-only: the chat it was launched
// from stays as it was.
import { useStore } from "../../lib/store";
import { subagentParent, subagentTitle } from "../../lib/tasks";
import { StatusDot } from "../../ui/atoms";
import { PanelHeader } from "../SidePanel";
import { ChatView } from "./ChatView";

export function SubagentPanel({ mid, sid, onClose }: { mid: string; sid: string; onClose: () => void }) {
  const agent = useStore((s) => (s.sessions[mid] ?? []).find((x) => x.session_id === subagentParent(sid))?.subagents?.find((a) => a.session_id === sid));
  return (
    <>
      <PanelHeader
        icon="agent"
        title={
          <span className="flex min-w-0 items-center gap-1.5">
            <StatusDot state={agent?.running ? "working" : "unread"} size={7} />
            <span className="truncate">{agent ? subagentTitle(agent) : "子 Agent"}</span>
          </span>
        }
        subtitle={["子 Agent", agent?.fork ? "分叉" : agent?.type, agent?.running ? "运行中，只读" : "只读"].filter(Boolean).join(" · ")}
        onClose={onClose}
      />
      <div className="flex min-h-0 flex-1 flex-col">
        <ChatView key={`${mid}/${sid}`} mid={mid} sid={sid} readOnly />
      </div>
    </>
  );
}
