// A personal agent session's avatar: the hub serves the Lark gateway's image at /agents/sessions/{id}/avatar (SVGs already
// rasterized to PNG); an <img> can't send a header, so the token rides in the query like other hub images. The
// version keys the browser cache; without an avatar (or if it fails to load) the title's first letter is shown.
import { BASE } from "../../lib/base";
import { useState } from "react";
import { enc, getToken } from "../../lib/api";
import { cx } from "../../ui/atoms";

export interface AvatarSession {
  id: string;
  title: string;
  hasAvatar?: boolean;
  avatarVersion?: string;
}

export function agentAvatarURL(s: AvatarSession) {
  return `${BASE}/api/v1/agents/sessions/${enc(s.id)}/avatar?v=${enc(s.avatarVersion ?? "")}&token=${enc(getToken())}`;
}

export function AgentAvatar({ s, size, className }: { s: AvatarSession; size: number; className?: string }) {
  const [failed, setFailed] = useState<string | null>(null);
  const url = s.hasAvatar ? agentAvatarURL(s) : null;
  const letter = Array.from((s.title || "?").trim())[0]?.toUpperCase() ?? "?";
  return (
    <span className={cx("flex shrink-0 items-center justify-center overflow-hidden rounded-full bg-inset text-ink-3", className)} style={{ width: size, height: size, fontSize: Math.round(size * 0.5) }} aria-hidden>
      {url && failed !== url ? <img src={url} alt="" width={size} height={size} loading="lazy" decoding="async" className="size-full object-cover" onError={() => setFailed(url)} /> : <span className="leading-none font-medium">{letter}</span>}
    </span>
  );
}
