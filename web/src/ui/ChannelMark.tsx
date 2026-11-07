// A channel's mark (docs/extensions/channels.md "UI"): Feishu's own for the Feishu bot and /agent, else an icon of its kind —
// WeChat's mark and its terms of use are undecided (lib/connectors.ts), so it draws the generic chat icon until they are.
import type { Channel } from "../lib/channels";
import { cx } from "./atoms";
import { BrandMark } from "./BrandMark";
import { Icon, type IconName } from "./icons";

/** `size` is the tile's; the apps' tile is filled, the others tinted. */
export function ChannelMark({ c, size }: { c: Pick<Channel, "id" | "kind">; size: number }) {
  if (c.id === "feishu" || c.id === "lark_agent") {
    return (
      <span className="flex shrink-0 items-center justify-center bg-surface shadow-hairline" style={{ width: size, height: size, borderRadius: radius(size) }}>
        <BrandMark brand="lark" size={size} fit="tile" />
      </span>
    );
  }
  const icon: IconName = c.kind === "app" ? "spark" : c.kind === "voice" ? "mic" : c.kind === "mention" ? "at" : "chat";
  return (
    <span className={cx("flex shrink-0 items-center justify-center", c.kind === "app" ? "bg-accent text-white" : "bg-accent-tint text-accent-ink")} style={{ width: size, height: size, borderRadius: radius(size) }}>
      <Icon name={icon} size={Math.round(size * 0.48)} />
    </span>
  );
}

/** The tile's corner: 9 at a row's 32, in proportion below (a 14 px mark beside a message). */
function radius(size: number): number {
  return Math.max(3, Math.round(size * 0.28));
}
