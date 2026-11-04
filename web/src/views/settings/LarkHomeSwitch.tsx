// 设置 › 连接器 › 飞书 › 首页显示消息 (iOS FeishuConnectorView's switch): off by default, kept by this browser.
import { setLarkHome, useLarkHome } from "../../lib/lark-chats";
import { Switch } from "./SettingsParts";

export function LarkHomeSwitch() {
  const on = useLarkHome();
  return (
    <div className="flex items-center gap-3 px-4 py-2.5">
      <div className="min-w-0 flex-1">
        <div className="text-[13px] text-ink">首页显示消息</div>
        <div className="mt-0.5 text-[11.5px] leading-snug text-ink-3">在侧边栏增加「消息」，查看全部私聊和群聊。与收件箱共用账号和聊天记录；只对这个浏览器生效。</div>
      </div>
      <Switch on={on} onChange={setLarkHome} label="首页显示消息" />
    </div>
  );
}
